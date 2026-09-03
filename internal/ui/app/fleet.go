package app

import (
	"context"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	"github.com/aronk11/correlux/internal/kube/workloads"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// fleetConcurrency bounds how many clusters are read at once.
//
// A fleet of thirty contexts must not open two hundred and seventy concurrent
// requests, and it must not wait for the slowest cluster before showing the
// first answer either. Four at a time does both (ADR 19).
const fleetConcurrency = 4

// fleetMemberMsg carries one cluster's answer.
type fleetMemberMsg struct {
	gen    uint64
	member fleet.Member
}

// fleetStartedMsg hands the model the channel the answers arrive on.
type fleetStartedMsg struct {
	gen     uint64
	results <-chan fleet.Member
}

// openFleet shows the fleet overview.
func (m *Model) openFleet() tea.Cmd {
	contexts := m.fleetContexts()
	if len(contexts) == 0 {
		m.view = viewFleet
		m.fleetMembers = nil
		m.rebuildCommands()
		return nil
	}

	m.view = viewFleet
	m.fleetPort.Offset, m.fleetPort.Cursor = 0, 0
	m.rebuildCommands()
	return m.startFleet(contexts)
}

// fleetContexts are the clusters the overview covers: the ones named in the
// configuration, plus whatever the user added for this session. Never every
// context in the kubeconfig by default.
func (m *Model) fleetContexts() []string {
	named := append([]string(nil), m.cfg.Fleet...)
	named = append(named, m.fleetExtra...)

	seen := map[string]bool{}
	out := make([]string, 0, len(named))
	for _, name := range named {
		if seen[name] {
			continue
		}
		if _, ok := m.kubeconfig.Context(name); !ok {
			// A context that has since left the kubeconfig; saying nothing
			// about it beats reporting it as unreachable.
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// includeEveryContext adds every context in the kubeconfig to the fleet, for
// this session only. It is an action the user takes, never a default: it
// authenticates against every cluster they hold credentials for.
func (m *Model) includeEveryContext() tea.Cmd {
	for _, c := range m.kubeconfig.Contexts {
		m.fleetExtra = append(m.fleetExtra, c.Name)
	}
	m.notice("Fleet: every context in this kubeconfig, for this session", theme.StatusWarning)
	return tea.Batch(m.openFleet(), m.expireNotice())
}

// startFleet reads every member, four at a time, and reports each answer as it
// arrives rather than waiting for the slowest cluster.
func (m *Model) startFleet(contexts []string) tea.Cmd {
	if m.cancelFleet != nil {
		m.cancelFleet()
	}
	gen := m.fleetGeneration + 1
	m.fleetGeneration = gen

	m.fleetMembers = make([]fleet.Member, 0, len(contexts))
	for _, name := range contexts {
		kctx, _ := m.kubeconfig.Context(name)
		m.fleetMembers = append(m.fleetMembers, fleet.Member{
			Context:    name,
			Production: kctx.Production,
			Scope:      "all namespaces",
			State:      fleet.Loading,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFleet = cancel

	factory := m.factory
	classifier := m.kubeconfig
	results := make(chan fleet.Member, len(contexts))

	return func() tea.Msg {
		go func() {
			defer close(results)

			work := make(chan string)
			var wg sync.WaitGroup
			for i := 0; i < min(fleetConcurrency, len(contexts)); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for name := range work {
						kctx, _ := classifier.Context(name)
						member := fleet.Member{
							Context:    name,
							Production: kctx.Production,
							Scope:      "all namespaces",
						}

						// Each cluster gets its own timeout: one that hangs
						// must not hold up the rest of the fleet.
						readCtx, readCancel := context.WithTimeout(ctx, factory.Timeout())
						apps, snapshot, err := factory.Applications(readCtx, name, workloads.Options{})
						// The machines, storage and services are read in the
						// same breath, one more bounded call rather than a
						// round trip per kind: none of the three belongs to
						// any one application, and a fleet that only asked
						// about applications would never mention them.
						extras := factory.FleetExtras(readCtx, name)
						readCancel()

						switch {
						case err != nil:
							member.State, member.Err = fleet.Failed, err
						default:
							member.State = fleet.Ready
							member.Applications = apps
							member.Gaps = snapshot.Gaps
							member.ReadAt = snapshot.FetchedAt
							member.Nodes, member.NodesErr = extras.Nodes, extras.NodesErr
							member.Claims, member.ClaimsErr = extras.Claims, extras.ClaimsErr
							member.Endpoints, member.EndpointsErr = extras.Endpoints, extras.EndpointsErr
						}
						select {
						case results <- member:
						case <-ctx.Done():
							return
						}
					}
				}()
			}

			for _, name := range contexts {
				select {
				case work <- name:
				case <-ctx.Done():
					close(work)
					wg.Wait()
					return
				}
			}
			close(work)
			wg.Wait()
		}()

		return fleetStartedMsg{gen: gen, results: results}
	}
}

// waitForFleet delivers the next cluster's answer.
func waitForFleet(gen uint64, results <-chan fleet.Member) tea.Cmd {
	return func() tea.Msg {
		member, open := <-results
		if !open {
			return nil
		}
		return fleetMemberMsg{gen: gen, member: member}
	}
}

// applyFleetMember stores one cluster's answer and waits for the next.
func (m *Model) applyFleetMember(msg *fleetMemberMsg) tea.Cmd {
	if msg.gen != m.fleetGeneration {
		return nil
	}
	for i := range m.fleetMembers {
		if m.fleetMembers[i].Context == msg.member.Context {
			m.fleetMembers[i] = msg.member
			break
		}
	}
	m.rebuildCommands()
	return waitForFleet(msg.gen, m.fleetResults)
}

// stopFleet ends the reads. Leaving the view must not leave nine requests per
// cluster in flight.
func (m *Model) stopFleet() {
	if m.cancelFleet != nil {
		m.cancelFleet()
		m.cancelFleet = nil
	}
	m.fleetGeneration++
	m.fleetResults = nil
	m.fleetPartsChan = nil
	m.fleetPending = 0
}

// enterFleetRow acts on the row under the cursor, and there is only one thing
// it can do: leave the overview for the cluster the row is about.
//
// Nothing is ever changed from here (ADR 19). Either you are in the fleet view
// and looking, or you are in a cluster and acting — there is no keystroke whose
// target cluster is ambiguous.
func (m *Model) enterFleetRow() tea.Cmd {
	targets := m.fleetTargets()
	if m.fleetPort.Cursor < 0 || m.fleetPort.Cursor >= len(targets) {
		return nil
	}
	target := targets[m.fleetPort.Cursor]

	m.stopFleet()

	if target.context == m.contextName {
		// Already here: leaving the overview is still the point, and the
		// dashboard for this cluster is already loaded.
		m.view = viewApplications
		m.rebuildCommands()
		switch {
		case target.node != "":
			return m.openObject(objectRef{Kind: "Node", Name: target.node, Resource: "nodes"})
		case target.claim != "":
			return m.openObject(objectRef{
				Kind: "PersistentVolumeClaim", Name: target.claim, Namespace: target.namespace,
				Resource: "persistentvolumeclaims",
			})
		case target.service != "":
			return m.openObject(objectRef{
				Kind: "Service", Name: target.service, Namespace: target.namespace, Resource: "services",
			})
		case target.application != "":
			return m.openApplication(target.application)
		}
		return nil
	}

	cmd := m.switchContextScoped(target.context, target.namespace)
	switch {
	case target.node != "":
		m.pendingObject = objectRef{Kind: "Node", Name: target.node, Resource: "nodes"}
		return cmd
	case target.claim != "":
		m.pendingObject = objectRef{
			Kind: "PersistentVolumeClaim", Name: target.claim, Namespace: target.namespace,
			Resource: "persistentvolumeclaims",
		}
		return cmd
	case target.service != "":
		m.pendingObject = objectRef{
			Kind: "Service", Name: target.service, Namespace: target.namespace, Resource: "services",
		}
		return cmd
	case target.application == "":
		return cmd
	}

	// The application is opened once its cluster's dashboard has loaded; until
	// then the name is remembered.
	m.pendingApplication = target.application
	return cmd
}

// fleetTarget is what one selectable row leads to.
type fleetTarget struct {
	context     string
	application string
	namespace   string
	// node, claim and service each name a machine, a volume claim or a
	// service rather than an application; Enter opens whichever is set in its
	// cluster, like any other object. At most one is ever set.
	node    string
	claim   string
	service string
}

// fleetTargets lists the rows in the order the screen renders them.
//
// It walks the same sorted members and the same per-member listings fleetData
// draws from, in the same order, so the line the cursor lands on and the
// target it acts on never drift apart.
func (m *Model) fleetTargets() []fleetTarget {
	sorted := m.sortedFleetMembers()
	targets := make([]fleetTarget, 0, len(m.fleetMembers))
	for i := range sorted {
		targets = append(targets, fleetTarget{context: sorted[i].Context})
	}
	for i := range sorted {
		member := &sorted[i]
		for _, node := range member.UnhealthyNodes() {
			targets = append(targets, fleetTarget{context: member.Context, node: node.Name})
		}
	}
	for i := range sorted {
		member := &sorted[i]
		for _, claim := range member.UnboundClaims() {
			targets = append(targets, fleetTarget{
				context: member.Context, claim: claim.Name, namespace: claim.Namespace,
			})
		}
	}
	for i := range sorted {
		member := &sorted[i]
		for _, set := range member.UnreadyEndpoints() {
			targets = append(targets, fleetTarget{
				context: member.Context, service: set.Service, namespace: set.Namespace,
			})
		}
	}
	for _, row := range m.fleetRows() {
		for _, instance := range row.Instances {
			if instance.Health == application.Healthy {
				continue
			}
			targets = append(targets, fleetTarget{
				context: instance.Context, application: row.Name, namespace: instance.Namespace,
			})
		}
	}
	return targets
}

// sortedFleetMembers orders the members the way the overview reads: worst
// first, production ahead of the rest at equal severity. Every section that
// lists members draws from this one order, so a reader who has learned where
// to look for the worst cluster finds every other section agreeing with it.
func (m *Model) sortedFleetMembers() []fleet.Member { return fleet.SortMembers(m.fleetMembers) }

// fleetRows merges what the clusters answered.
func (m *Model) fleetRows() []fleet.Row { return fleet.Rows(m.fleetMembers) }

// fleetData assembles the overview.
func (m *Model) fleetData() screens.FleetData {
	d := screens.FleetData{Offset: m.fleetPort.Offset, Selected: m.fleetPort.Cursor}

	if len(m.fleetMembers) == 0 {
		d.Message = "No fleet configured. List the contexts to watch under `fleet:` in " +
			orNone(m.configPath) + ", or add every context in this kubeconfig for this session " +
			"from the command palette."
		return d
	}

	summary := fleet.Summarise(m.fleetMembers)
	d.Title = "Fleet"
	d.Subtitle = fleetSubtitle(summary)

	sorted := m.sortedFleetMembers()

	// Clusters, worst first: reachable, degraded and unreachable clusters are
	// grouped by urgency here rather than left in configuration order, so a
	// production cluster on fire is never scrolled past to reach it.
	clusters := screens.DetailSection{
		Title:   "Clusters",
		Columns: []string{"Cluster", "State", "Applications", "Detail"},
	}
	target := 0
	for i := range sorted {
		member := &sorted[i]
		row := screens.DetailRow{Cells: []string{
			memberName(member),
			member.State.String(),
			memberApplications(member),
			memberDetail(member),
		}, Target: target}
		row.Status = memberStatus(member)
		clusters.Rows = append(clusters.Rows, row)
		target++
	}

	nodes := screens.DetailSection{
		// Everything unusual about a machine belongs here, a cordon included:
		// it is not a fault, and it is the reason a rollout will not land.
		Title:   "Nodes",
		Columns: []string{"Cluster", "Node", "State", "Detail"},
		Empty:   nodesEmpty(m.fleetMembers),
	}
	for i := range sorted {
		member := &sorted[i]
		for _, node := range member.UnhealthyNodes() {
			nodes.Rows = append(nodes.Rows, screens.DetailRow{
				Cells: []string{
					memberLabel(member.Context, member.Production),
					node.Name,
					nodeState(node),
					fleet.NodeDigest(node),
				},
				Status: nodeStatus(node),
				Target: target,
			})
			target++
		}
	}

	storage := screens.DetailSection{
		// A claim that never bound is invisible in an application-only view:
		// nothing about the pod that mounts it says why it never started.
		Title:   "Storage",
		Columns: []string{"Cluster", "Namespace", "Claim", "Detail"},
		Empty:   storageEmpty(m.fleetMembers),
	}
	for i := range sorted {
		member := &sorted[i]
		for _, claim := range member.UnboundClaims() {
			storage.Rows = append(storage.Rows, screens.DetailRow{
				Cells: []string{
					memberLabel(member.Context, member.Production),
					orNone(claim.Namespace),
					claim.Name,
					fleet.ClaimDigest(claim),
				},
				Status: claimStatus(claim),
				Target: target,
			})
			target++
		}
	}

	services := screens.DetailSection{
		// A Service with no ready endpoint looks like nothing is wrong to any
		// view that only reads workloads: the pods it selects can all be
		// healthy while it routes to none of them.
		Title:   "Services",
		Columns: []string{"Cluster", "Namespace", "Service", "Detail"},
		Empty:   servicesEmpty(m.fleetMembers),
	}
	for i := range sorted {
		member := &sorted[i]
		for _, set := range member.UnreadyEndpoints() {
			services.Rows = append(services.Rows, screens.DetailRow{
				Cells: []string{
					memberLabel(member.Context, member.Production),
					orNone(set.Namespace),
					set.Service,
					fleet.EndpointDigest(set),
				},
				Status: theme.StatusCritical,
				Target: target,
			})
			target++
		}
	}

	applications := screens.DetailSection{
		Title: "What is broken",
		// The namespace is not decoration: the same application name in five
		// namespaces of one cluster is five different things, and without this
		// column they render as five identical lines.
		Columns: []string{"Application", "Cluster", "Namespace", "Health", "Pods", "Detail"},
		Empty:   fleetEmpty(summary),
	}
	for _, row := range m.fleetRows() {
		for _, instance := range row.Instances {
			if instance.Health == application.Healthy {
				continue
			}
			applications.Rows = append(applications.Rows, screens.DetailRow{
				Cells: []string{
					row.Name,
					memberLabel(instance.Context, instance.Production),
					orNone(instance.Namespace),
					instance.Health.String(),
					itoa(int(instance.ReadyPods)) + "/" + itoa(int(instance.DesiredPods)),
					instance.Digest(),
				},
				Status: healthStatus(instance.Health),
				Target: target,
			})
			target++
		}
	}

	d.Sections = []screens.DetailSection{clusters, nodes, storage, services, applications}
	return d
}

// fleetSubtitle says what the numbers cover, and never implies they cover a
// cluster that did not answer.
func fleetSubtitle(s fleet.Summary) string {
	parts := []string{itoa(s.Clusters) + " " + clusterWord(s.Clusters)}
	if s.Pending > 0 {
		parts = append(parts, itoa(s.Pending)+" still connecting")
	}
	if s.Failed > 0 {
		parts = append(parts, itoa(s.Failed)+" unreachable")
	}
	if s.Answered > 0 {
		count := itoa(s.Counts.Total) + " applications"
		if !s.Complete() {
			count += " across " + itoa(s.Answered) + " of " + itoa(s.Clusters)
		}
		parts = append(parts, count)
		if s.Unhealthy > 0 {
			parts = append(parts, itoa(s.Unhealthy)+" not healthy")
		}
		if node := nodeSummary(s); node != "" {
			parts = append(parts, node)
		}
		if claim := storageSummary(s); claim != "" {
			parts = append(parts, claim)
		}
		if svc := serviceSummary(s); svc != "" {
			parts = append(parts, svc)
		}
	}
	return strings.Join(parts, "   ")
}

// nodeSummary states everything odd about the fleet's machines in one phrase.
func nodeSummary(s fleet.Summary) string {
	var parts []string
	if s.NodesNotReady > 0 {
		parts = append(parts, itoa(s.NodesNotReady)+" of "+itoa(s.Nodes)+" nodes not ready")
	}
	if s.NodesPressure > 0 {
		parts = append(parts, itoa(s.NodesPressure)+" under pressure")
	}
	if s.NodesCordoned > 0 {
		parts = append(parts, itoa(s.NodesCordoned)+" cordoned")
	}
	return strings.Join(parts, ", ")
}

// storageSummary states everything odd about the fleet's claims.
func storageSummary(s fleet.Summary) string {
	if s.ClaimsUnbound == 0 {
		return ""
	}
	return itoa(s.ClaimsUnbound) + " of " + itoa(s.Claims) + " claims unbound"
}

// serviceSummary states everything odd about the fleet's services.
func serviceSummary(s fleet.Summary) string {
	if s.ServicesUnreachable == 0 {
		return ""
	}
	return itoa(s.ServicesUnreachable) + " of " + itoa(s.Services) + " services unreachable"
}

func fleetEmpty(s fleet.Summary) string {
	switch {
	case s.Answered == 0:
		return "no cluster has answered yet"
	case !s.Complete():
		return "nothing broken in the clusters that answered"
	default:
		return "nothing is broken anywhere"
	}
}

func clusterWord(n int) string {
	if n == 1 {
		return "cluster"
	}
	return "clusters"
}

func memberName(m *fleet.Member) string { return memberLabel(m.Context, m.Production) }

// memberLabel marks a production cluster in text, wherever it appears: in a
// list of eight clusters, which one is production is the fact that matters.
func memberLabel(context string, production bool) string {
	if production {
		return context + "  PROD"
	}
	return context
}

func memberApplications(m *fleet.Member) string {
	if m.State != fleet.Ready {
		return "—"
	}
	counts := m.Counts()
	return itoa(counts.Total)
}

func memberDetail(m *fleet.Member) string {
	switch m.State {
	case fleet.Failed:
		return shortError(m.Err)
	case fleet.Loading, fleet.Pending:
		return ""
	}
	counts := m.Counts()
	var parts []string
	if counts.Down > 0 {
		parts = append(parts, itoa(counts.Down)+" down")
	}
	if counts.Degraded > 0 {
		parts = append(parts, itoa(counts.Degraded)+" degraded")
	}

	// Everything odd about the machines, not only the worst of it: a cluster
	// with one node down and two cordoned has two facts worth knowing, and
	// showing only the first is how the second is discovered too late.
	trouble := m.NodeTrouble()
	if m.NodesErr != nil {
		parts = append(parts, "nodes not readable")
	}
	if trouble.NotReady > 0 {
		parts = append(parts, itoa(trouble.NotReady)+" of "+itoa(trouble.Total)+" nodes not ready")
	}
	if trouble.Pressure > 0 {
		parts = append(parts, itoa(trouble.Pressure)+" under pressure")
	}
	if trouble.Cordoned > 0 {
		parts = append(parts, itoa(trouble.Cordoned)+" cordoned")
	}

	// Storage and services belong to no application either; a cluster whose
	// applications are all healthy can still be serving nothing.
	storage := m.StorageTrouble()
	if m.ClaimsErr != nil {
		parts = append(parts, "claims not readable")
	}
	if storage.Unbound > 0 {
		parts = append(parts, itoa(storage.Unbound)+" claim(s) unbound")
	}
	services := m.ServiceTrouble()
	if m.EndpointsErr != nil {
		parts = append(parts, "endpoints not readable")
	}
	if services.NoReadyEndpoints > 0 {
		parts = append(parts, itoa(services.NoReadyEndpoints)+" service(s) unreachable")
	}

	if len(m.Gaps) > 0 {
		parts = append(parts, itoa(len(m.Gaps))+" kind(s) unreadable")
	}
	if len(parts) == 0 {
		return "nothing broken"
	}
	return strings.Join(parts, ", ")
}

// nodeState names what is wrong with a machine, in the order it matters.
func nodeState(n application.Node) string {
	switch {
	case !n.Ready:
		return "not ready"
	case len(n.Pressure) > 0:
		return "under pressure"
	case n.Unschedulable:
		return "cordoned"
	default:
		return "ready"
	}
}

func nodeStatus(n application.Node) theme.Status { return statusFor(fleet.NodeSeverity(n)) }

// claimStatus colours a storage row by how urgent its trouble is.
func claimStatus(c application.Claim) theme.Status { return statusFor(fleet.ClaimSeverity(c)) }

// statusFor maps the domain's severity band onto the terminal's four render
// colours. Two bands sharing a colour still read apart because the word next
// to the glyph is never the same one (ADR 9): "degraded" is not "warning".
func statusFor(s fleet.Severity) theme.Status {
	switch s {
	case fleet.SeverityCritical:
		return theme.StatusCritical
	case fleet.SeverityDegraded, fleet.SeverityWarning:
		return theme.StatusWarning
	case fleet.SeverityHealthy:
		return theme.StatusHealthy
	default:
		return theme.StatusUnknown
	}
}

// nodesEmpty says which nothing this is: no trouble, or nothing read yet.
func nodesEmpty(members []fleet.Member) string {
	answered, unreadable := 0, 0
	for i := range members {
		if members[i].State != fleet.Ready {
			continue
		}
		answered++
		if members[i].NodesErr != nil {
			unreadable++
		}
	}
	switch {
	case answered == 0:
		return "no cluster has answered yet"
	case unreadable == answered:
		return "the nodes could not be listed in any cluster"
	case unreadable > 0:
		return "nothing wrong with the nodes that could be read"
	default:
		return "every node is ready"
	}
}

// memberStatus colours a cluster row by the domain's own severity band, so
// the colour a cluster gets here is exactly the fact that put it where it is
// in the sort order — never a second opinion computed only for display.
func memberStatus(m *fleet.Member) theme.Status { return statusFor(m.Severity()) }

// storageEmpty says which nothing this is: no trouble, or nothing read yet.
func storageEmpty(members []fleet.Member) string {
	answered, unreadable := 0, 0
	for i := range members {
		if members[i].State != fleet.Ready {
			continue
		}
		answered++
		if members[i].ClaimsErr != nil {
			unreadable++
		}
	}
	switch {
	case answered == 0:
		return "no cluster has answered yet"
	case unreadable == answered:
		return "storage could not be listed in any cluster"
	case unreadable > 0:
		return "nothing unbound in the storage that could be read"
	default:
		return "every claim is bound"
	}
}

// servicesEmpty says which nothing this is: no trouble, or nothing read yet.
func servicesEmpty(members []fleet.Member) string {
	answered, unreadable := 0, 0
	for i := range members {
		if members[i].State != fleet.Ready {
			continue
		}
		answered++
		if members[i].EndpointsErr != nil {
			unreadable++
		}
	}
	switch {
	case answered == 0:
		return "no cluster has answered yet"
	case unreadable == answered:
		return "endpoints could not be listed in any cluster"
	case unreadable > 0:
		return "every service that could be read routes somewhere"
	default:
		return "every service routes to a ready endpoint"
	}
}
