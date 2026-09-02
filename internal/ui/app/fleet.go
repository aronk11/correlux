package app

import (
	"context"
	"sort"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/domain/fleet"
	"github.com/aronk11/kubeui/internal/kube/workloads"
	"github.com/aronk11/kubeui/internal/ui/screens"
	"github.com/aronk11/kubeui/internal/ui/theme"
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
	m.fleetOffset, m.fleetCursor = 0, 0
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
						readCancel()

						switch {
						case err != nil:
							member.State, member.Err = fleet.Failed, err
						default:
							member.State = fleet.Ready
							member.Applications = apps
							member.Gaps = snapshot.Gaps
							member.ReadAt = snapshot.FetchedAt
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
func (m *Model) applyFleetMember(msg fleetMemberMsg) tea.Cmd {
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
	if m.fleetCursor < 0 || m.fleetCursor >= len(targets) {
		return nil
	}
	target := targets[m.fleetCursor]

	m.stopFleet()

	if target.context == m.contextName {
		// Already here: leaving the overview is still the point, and the
		// dashboard for this cluster is already loaded.
		m.view = viewApplications
		m.rebuildCommands()
		if target.application != "" {
			return m.openApplication(target.application)
		}
		return nil
	}

	cmd := m.switchContextScoped(target.context, target.namespace)
	if target.application == "" {
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
}

// fleetTargets lists the rows in the order the screen renders them.
func (m *Model) fleetTargets() []fleetTarget {
	targets := make([]fleetTarget, 0, len(m.fleetMembers))
	for _, member := range m.fleetMembers {
		targets = append(targets, fleetTarget{context: member.Context})
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

// fleetRows merges what the clusters answered.
func (m *Model) fleetRows() []fleet.Row { return fleet.Rows(m.fleetMembers) }

// fleetData assembles the overview.
func (m *Model) fleetData() screens.FleetData {
	d := screens.FleetData{Offset: m.fleetOffset, Selected: m.fleetCursor}

	if len(m.fleetMembers) == 0 {
		d.Message = "No fleet configured. List the contexts to watch under `fleet:` in " +
			orNone(m.configPath) + ", or add every context in this kubeconfig for this session " +
			"from the command palette."
		return d
	}

	summary := fleet.Summarise(m.fleetMembers)
	d.Title = "Fleet"
	d.Subtitle = fleetSubtitle(summary)

	clusters := screens.DetailSection{
		Title:   "Clusters",
		Columns: []string{"Cluster", "State", "Applications", "Detail"},
	}
	target := 0
	for _, member := range m.fleetMembers {
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
					instanceDetail(instance),
				},
				Status: healthStatus(instance.Health),
				Target: target,
			})
			target++
		}
	}

	d.Sections = []screens.DetailSection{clusters, applications}
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
	}
	return strings.Join(parts, "   ")
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

func memberName(m fleet.Member) string { return memberLabel(m.Context, m.Production) }

// memberLabel marks a production cluster in text, wherever it appears: in a
// list of eight clusters, which one is production is the fact that matters.
func memberLabel(context string, production bool) string {
	if production {
		return context + "  PROD"
	}
	return context
}

func memberApplications(m fleet.Member) string {
	if m.State != fleet.Ready {
		return "—"
	}
	counts := m.Counts()
	return itoa(counts.Total)
}

func memberDetail(m fleet.Member) string {
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
	if len(m.Gaps) > 0 {
		parts = append(parts, itoa(len(m.Gaps))+" kind(s) unreadable")
	}
	if len(parts) == 0 {
		return "nothing broken"
	}
	return strings.Join(parts, ", ")
}

func memberStatus(m fleet.Member) theme.Status {
	switch m.State {
	case fleet.Failed:
		return theme.StatusCritical
	case fleet.Ready:
		counts := m.Counts()
		switch {
		case counts.Down > 0:
			return theme.StatusCritical
		case counts.Degraded > 0:
			return theme.StatusWarning
		default:
			return theme.StatusHealthy
		}
	default:
		return theme.StatusUnknown
	}
}

func instanceDetail(i fleet.Instance) string {
	if len(i.Problems) > 0 {
		parts := make([]string, 0, len(i.Problems))
		for _, p := range i.Problems {
			parts = append(parts, itoa(p.Count)+" "+p.Reason)
		}
		sort.Strings(parts)
		return strings.Join(parts, ", ")
	}
	return i.Summary
}
