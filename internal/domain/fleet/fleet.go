// Package fleet answers one question across several clusters: is anything
// broken anywhere?
//
// It is the application model one level up. Each cluster is grouped into
// applications exactly as a single cluster is (internal/domain/application),
// and this package merges those lists: an application deployed to five clusters
// is one row with five instances, and a cluster that could not be read is a
// member whose state says so rather than a silence that reads as health.
//
// Nothing here talks to Kubernetes. It takes what each cluster answered —
// including the clusters that answered nothing — and arranges it.
package fleet

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// State is how far a member of the fleet has got.
//
// It is an explicit lifecycle for the same reason every remote value in
// Correlux has one: a fleet view that cannot tell "still connecting" from
// "nothing broken here" is a fleet view that lies at exactly the wrong moment.
type State int

const (
	// Pending means the cluster has not been asked yet.
	Pending State = iota
	// Loading means the request is out.
	Loading
	// Ready means the cluster answered.
	Ready
	// Failed means it could not be read; Member.Err says why.
	Failed
)

func (s State) String() string {
	switch s {
	case Loading:
		return "connecting"
	case Ready:
		return "connected"
	case Failed:
		return "unreachable"
	default:
		return "not read"
	}
}

// Member is one cluster in the fleet.
type Member struct {
	// Context is the kubeconfig context, which is what the user calls the
	// cluster and what every action is scoped to.
	Context string
	// Production marks a context classified as production, so a fleet view
	// cannot quietly put one next to a staging cluster (SPEC 7).
	Production bool
	// Scope is the namespace the member was read with, empty for all.
	Scope string

	State State
	Err   error
	// Applications are what was found there, worst first.
	Applications []application.Application
	// Nodes are the cluster's own machines. A broken node is the most common
	// thing that is wrong with a cluster and belongs to no application, so a
	// fleet view that only counts applications misses it entirely.
	Nodes []application.Node
	// NodesErr reports that the nodes could not be listed — a scoped service
	// account often may not — which is different from a cluster with no
	// problems.
	NodesErr error
	// Claims are the cluster's PersistentVolumeClaims. An unbound one is the
	// most common thing wrong with a workload's storage, and like a node it
	// belongs to no application.
	Claims    []application.Claim
	ClaimsErr error
	// Endpoints is how many addresses each service currently routes to. A
	// Service that publishes a slice with no ready address serves nothing,
	// which nothing about its workloads being healthy would ever say.
	Endpoints    []application.EndpointSet
	EndpointsErr error
	// Gaps are the kinds that could not be read in this cluster.
	Gaps []application.Gap
	// ReadAt is when the answer arrived.
	ReadAt time.Time
}

// Counts summarises one member by health.
func (m *Member) Counts() application.Counts { return application.Summarise(m.Applications) }

// NodeTrouble is what is wrong with a cluster's machines.
type NodeTrouble struct {
	Total    int
	NotReady int
	// Pressure counts nodes reporting memory, disk or PID pressure.
	Pressure int
	// Cordoned counts nodes that will take no new pods. Nothing is wrong with
	// them yet, and a rollout will not land there.
	Cordoned int
}

// Any reports whether anything about the nodes is worth showing.
func (n NodeTrouble) Any() bool { return n.NotReady > 0 || n.Pressure > 0 || n.Cordoned > 0 }

// Nodes summarises the member's machines.
func (m *Member) NodeTrouble() NodeTrouble {
	trouble := NodeTrouble{Total: len(m.Nodes)}
	for _, node := range m.Nodes {
		switch {
		case !node.Ready:
			trouble.NotReady++
		case len(node.Pressure) > 0:
			trouble.Pressure++
		case node.Unschedulable:
			trouble.Cordoned++
		}
	}
	return trouble
}

// UnhealthyNodes returns the nodes worth naming, worst first.
func (m *Member) UnhealthyNodes() []application.Node {
	out := make([]application.Node, 0, len(m.Nodes))
	for _, node := range m.Nodes {
		if !node.Ready || len(node.Pressure) > 0 || node.Unschedulable {
			out = append(out, node)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return !out[i].Ready
		}
		if len(out[i].Pressure) != len(out[j].Pressure) {
			return len(out[i].Pressure) > len(out[j].Pressure)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// StorageTrouble is what is wrong with a cluster's persistent storage.
type StorageTrouble struct {
	Total int
	// Unbound counts claims that are not Bound: Pending or Lost.
	Unbound int
}

// Any reports whether any claim is worth showing.
func (s StorageTrouble) Any() bool { return s.Unbound > 0 }

// StorageTrouble summarises the member's PersistentVolumeClaims.
func (m *Member) StorageTrouble() StorageTrouble {
	trouble := StorageTrouble{Total: len(m.Claims)}
	for _, claim := range m.Claims {
		if claim.Phase != "Bound" {
			trouble.Unbound++
		}
	}
	return trouble
}

// UnboundClaims returns the claims worth naming, worst first: a claim that
// lost its volume before one still waiting to bind.
func (m *Member) UnboundClaims() []application.Claim {
	out := make([]application.Claim, 0, len(m.Claims))
	for _, claim := range m.Claims {
		if claim.Phase != "Bound" {
			out = append(out, claim)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Phase == "Lost") != (out[j].Phase == "Lost") {
			return out[i].Phase == "Lost"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ClaimDigest states what is wrong with a claim, in the cluster's own words.
func ClaimDigest(c application.Claim) string {
	switch c.Phase {
	case "Lost":
		return "volume lost"
	case "", "Pending":
		return "unbound"
	default:
		return c.Phase
	}
}

// ServiceTrouble is what is wrong with a cluster's services.
type ServiceTrouble struct {
	Total int
	// NoReadyEndpoints counts services that publish a slice and route to
	// nothing: every address is not ready, or none exist at all.
	NoReadyEndpoints int
}

// Any reports whether any service is worth showing.
func (s ServiceTrouble) Any() bool { return s.NoReadyEndpoints > 0 }

// ServiceTrouble summarises the member's endpoint health.
func (m *Member) ServiceTrouble() ServiceTrouble {
	trouble := ServiceTrouble{Total: len(m.Endpoints)}
	for _, set := range m.Endpoints {
		if set.Ready == 0 {
			trouble.NoReadyEndpoints++
		}
	}
	return trouble
}

// UnreadyEndpoints returns the services worth naming: every one that routes
// to nothing at all.
func (m *Member) UnreadyEndpoints() []application.EndpointSet {
	out := make([]application.EndpointSet, 0, len(m.Endpoints))
	for _, set := range m.Endpoints {
		if set.Ready == 0 {
			out = append(out, set)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Service < out[j].Service
	})
	return out
}

// EndpointDigest states what is wrong with a service's endpoints.
func EndpointDigest(e application.EndpointSet) string {
	if e.NotReady > 0 {
		return "no ready endpoints, " + strconv.Itoa(e.NotReady) + " not ready"
	}
	return "no ready endpoints"
}

// NodeDigest states what is wrong with a node, in the cluster's own words,
// and says so plainly when the cluster gave no reason at all.
func NodeDigest(n application.Node) string {
	switch {
	case n.Message != "":
		return n.Message
	case n.Reason != "":
		return n.Reason
	case len(n.Pressure) > 0:
		return strings.Join(n.Pressure, ", ")
	case n.Unschedulable:
		return "cordoned: no new pods will be placed here"
	default:
		return "no reason given"
	}
}

// Healthy reports whether the member answered and nothing in it is broken —
// applications, machines and storage all.
func (m *Member) Healthy() bool {
	if m.State != Ready {
		return false
	}
	counts := m.Counts()
	trouble := m.NodeTrouble()
	storage := m.StorageTrouble()
	services := m.ServiceTrouble()
	return counts.Down == 0 && counts.Degraded == 0 &&
		trouble.NotReady == 0 && trouble.Pressure == 0 &&
		!storage.Any() && !services.Any()
}

// Severity ranks how urgently a row on the fleet overview deserves a look,
// worst first: Critical, Degraded, Warning, Unknown, Healthy. It is the
// ordering principle for the whole view — relevance, never the alphabet — and
// it cuts across where a row came from: an application, a node, a claim, a
// service, or a cluster that could not be read at all.
type Severity int

const (
	// SeverityCritical is nothing working: an application with no ready pods,
	// a node that is not ready, a claim that lost its volume, a service with
	// no ready endpoint, or a cluster that could not be reached at all.
	SeverityCritical Severity = iota
	// SeverityDegraded is serving, but not fully: an application missing
	// replicas or restarting.
	SeverityDegraded
	// SeverityWarning is not broken yet: a node under pressure or cordoned, a
	// claim still waiting to bind.
	SeverityWarning
	// SeverityUnknown is something Correlux cannot vouch for: a kind that could
	// not be listed, a cluster still connecting.
	SeverityUnknown
	// SeverityHealthy is nothing wrong.
	SeverityHealthy
)

// String renders the severity as the word shown beside its glyph, so meaning
// never rides on colour alone (ADR 9).
func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityDegraded:
		return "degraded"
	case SeverityWarning:
		return "warning"
	case SeverityHealthy:
		return "healthy"
	default:
		return "unknown"
	}
}

// InstanceSeverity bands an application instance by its health.
func InstanceSeverity(i Instance) Severity {
	switch i.Health {
	case application.Down:
		return SeverityCritical
	case application.Degraded:
		return SeverityDegraded
	case application.Unknown:
		return SeverityUnknown
	default:
		return SeverityHealthy
	}
}

// NodeSeverity bands a node: not ready is critical, pressure or a cordon is a
// warning worth a look before it becomes one.
func NodeSeverity(n application.Node) Severity {
	switch {
	case !n.Ready:
		return SeverityCritical
	case len(n.Pressure) > 0, n.Unschedulable:
		return SeverityWarning
	default:
		return SeverityHealthy
	}
}

// ClaimSeverity bands a claim: a lost volume is unrecoverable, a pending one
// is merely not there yet.
func ClaimSeverity(c application.Claim) Severity {
	switch c.Phase {
	case "Lost":
		return SeverityCritical
	case "Bound":
		return SeverityHealthy
	case "":
		return SeverityUnknown
	default:
		return SeverityWarning
	}
}

// EndpointSeverity bands a service's endpoints: nothing ready is as critical
// as the application behind it being down, because it means the same thing to
// whoever calls it.
func EndpointSeverity(e application.EndpointSet) Severity {
	if e.Ready == 0 {
		return SeverityCritical
	}
	return SeverityHealthy
}

// Severity bands the member as a whole, worst thing about it first: a cluster
// that could not be read is never healthy, whatever it last reported.
func (m *Member) Severity() Severity {
	switch m.State {
	case Failed:
		return SeverityCritical
	case Pending, Loading:
		return SeverityUnknown
	}

	counts := m.Counts()
	trouble := m.NodeTrouble()
	storage := m.StorageTrouble()
	services := m.ServiceTrouble()

	switch {
	case counts.Down > 0, trouble.NotReady > 0, services.NoReadyEndpoints > 0:
		return SeverityCritical
	case counts.Degraded > 0:
		return SeverityDegraded
	case trouble.Pressure > 0, trouble.Cordoned > 0, storage.Unbound > 0:
		return SeverityWarning
	case counts.Unknown > 0, m.NodesErr != nil, m.ClaimsErr != nil, m.EndpointsErr != nil, len(m.Gaps) > 0:
		return SeverityUnknown
	default:
		return SeverityHealthy
	}
}

// SortMembers orders members the way the overview reads: worst first, and
// production ahead of the rest at equal severity, so a production cluster
// never sits buried under staging noise. It returns a new slice; the caller's
// order is left alone.
func SortMembers(members []Member) []Member {
	out := append([]Member(nil), members...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Severity() != b.Severity() {
			return a.Severity() < b.Severity()
		}
		if a.Production != b.Production {
			return a.Production
		}
		return a.Context < b.Context
	})
	return out
}

// Instance is one application as it exists in one cluster.
type Instance struct {
	Context     string
	Production  bool
	Namespace   string
	Health      application.Health
	Summary     string
	ReadyPods   int32
	DesiredPods int32
	Problems    []application.Problem
	// Restarts is the container restart count across the instance's pods,
	// which is the fact that turns "CrashLoopBackOff" from a label into a
	// number worth reacting to.
	Restarts int32
}

// Digest renders what is wrong with the instance as a short, honest phrase.
//
// It never invents a reason: a problem the cluster reported is quoted as
// such, "0 of 4 ready" is quoted when nothing else is known, and when neither
// is available the phrase says plainly that no reason was given, rather than
// falling silent.
func (i Instance) Digest() string {
	var parts []string
	if len(i.Problems) > 0 {
		if i.ReadyPods == 0 && i.DesiredPods > 0 {
			// Nothing is ready at all; that fact belongs beside whatever
			// problem caused it, not instead of it.
			parts = append(parts, strconv.Itoa(int(i.ReadyPods))+"/"+strconv.Itoa(int(i.DesiredPods))+" ready")
		}
		parts = append(parts, i.Problems[0].Reason)
		if i.Restarts > 0 {
			parts = append(parts, strconv.Itoa(int(i.Restarts))+" restarts")
		}
		return strings.Join(parts, ", ")
	}
	if i.DesiredPods > 0 && i.ReadyPods < i.DesiredPods {
		return strconv.Itoa(int(i.ReadyPods)) + "/" + strconv.Itoa(int(i.DesiredPods)) + " ready"
	}
	if i.Summary != "" {
		return i.Summary
	}
	return "no reason given"
}

// Row is one application across the fleet.
type Row struct {
	Name string
	// Instances are where it runs, worst first.
	Instances []Instance
	// Worst is the health of its unhappiest instance, which is what the row is
	// sorted and coloured by.
	Worst application.Health
}

// Missing reports the clusters an application is *not* in, given the members
// that answered. An application that exists in four of five production clusters
// is a question worth asking, and it is invisible in any per-cluster view.
func (r Row) Missing(members []Member) []string {
	present := map[string]bool{}
	for _, i := range r.Instances {
		present[i.Context] = true
	}
	var out []string
	for i := range members {
		if members[i].State == Ready && !present[members[i].Context] {
			out = append(out, members[i].Context)
		}
	}
	return out
}

// Summary is the whole fleet in numbers.
type Summary struct {
	// Clusters that were asked, answered and failed.
	Clusters int
	Answered int
	Failed   int
	Pending  int
	Counts   application.Counts
	// Unhealthy is how many application instances are degraded or down.
	Unhealthy int
	// Nodes across the fleet, and how many of them are not well.
	Nodes         int
	NodesNotReady int
	NodesPressure int
	NodesCordoned int
	// Claims across the fleet, and how many are not bound.
	Claims        int
	ClaimsUnbound int
	// Services across the fleet, and how many route to nothing.
	Services            int
	ServicesUnreachable int
}

// Rows merges the members' applications into one list, worst first.
func Rows(members []Member) []Row {
	byName := map[string]*Row{}
	for m := range members {
		member := &members[m]
		if member.State != Ready {
			continue
		}
		for i := range member.Applications {
			app := &member.Applications[i]
			row, ok := byName[app.Name]
			if !ok {
				row = &Row{Name: app.Name}
				byName[app.Name] = row
			}
			row.Instances = append(row.Instances, Instance{
				Context:     member.Context,
				Production:  member.Production,
				Namespace:   app.Namespace,
				Health:      app.Health,
				Summary:     app.Summary,
				ReadyPods:   app.ReadyPods,
				DesiredPods: app.DesiredPods,
				Problems:    app.Problems,
				Restarts:    app.Restarts,
			})
		}
	}

	out := make([]Row, 0, len(byName))
	for _, row := range byName {
		sortInstances(row.Instances)
		row.Worst = worstOf(row.Instances)
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if severity(out[i].Worst) != severity(out[j].Worst) {
			return severity(out[i].Worst) > severity(out[j].Worst)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Summarise counts the fleet, and is explicit about what it does not cover: a
// count that silently leaves out two unreachable clusters is the mistake this
// whole package exists to avoid.
func Summarise(members []Member) Summary {
	s := Summary{Clusters: len(members)}
	for i := range members {
		member := &members[i]
		switch member.State {
		case Ready:
			s.Answered++
			trouble := member.NodeTrouble()
			s.Nodes += trouble.Total
			s.NodesNotReady += trouble.NotReady
			s.NodesPressure += trouble.Pressure
			s.NodesCordoned += trouble.Cordoned
			storage := member.StorageTrouble()
			s.Claims += storage.Total
			s.ClaimsUnbound += storage.Unbound
			services := member.ServiceTrouble()
			s.Services += services.Total
			s.ServicesUnreachable += services.NoReadyEndpoints
			counts := member.Counts()
			s.Counts.Total += counts.Total
			s.Counts.Healthy += counts.Healthy
			s.Counts.Degraded += counts.Degraded
			s.Counts.Down += counts.Down
			s.Counts.Unknown += counts.Unknown
			s.Unhealthy += counts.Degraded + counts.Down
		case Failed:
			s.Failed++
		case Pending, Loading:
			s.Pending++
		}
	}
	return s
}

// Complete reports whether every cluster answered. When it is false, no total
// derived from the fleet is the whole picture.
func (s Summary) Complete() bool { return s.Answered == s.Clusters }

// sortInstances puts the unhappiest cluster first, and production before the
// rest at equal health: which cluster is broken matters as much as that one is.
func sortInstances(instances []Instance) {
	sort.Slice(instances, func(i, j int) bool {
		a, b := instances[i], instances[j]
		if severity(a.Health) != severity(b.Health) {
			return severity(a.Health) > severity(b.Health)
		}
		if a.Production != b.Production {
			return a.Production
		}
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		// One cluster can run the same application in several namespaces; they
		// are different installations and must not shuffle between renders.
		return a.Namespace < b.Namespace
	})
}

func worstOf(instances []Instance) application.Health {
	worst := application.Healthy
	for _, i := range instances {
		if severity(i.Health) > severity(worst) {
			worst = i.Health
		}
	}
	return worst
}

// severity orders health the way the dashboard does: down, degraded, unknown,
// healthy.
func severity(h application.Health) int {
	switch h {
	case application.Down:
		return 3
	case application.Degraded:
		return 2
	case application.Unknown:
		return 1
	default:
		return 0
	}
}
