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
	// Gaps are the kinds that could not be read in this cluster.
	Gaps []application.Gap
	// ReadAt is when the answer arrived.
	ReadAt time.Time
}

// Counts summarises one member by health.
func (m Member) Counts() application.Counts { return application.Summarise(m.Applications) }

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
func (m Member) NodeTrouble() NodeTrouble {
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
func (m Member) UnhealthyNodes() []application.Node {
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

// Healthy reports whether the member answered and nothing in it is broken —
// applications and machines both.
func (m Member) Healthy() bool {
	if m.State != Ready {
		return false
	}
	counts := m.Counts()
	trouble := m.NodeTrouble()
	return counts.Down == 0 && counts.Degraded == 0 && trouble.NotReady == 0 && trouble.Pressure == 0
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
	for _, m := range members {
		if m.State == Ready && !present[m.Context] {
			out = append(out, m.Context)
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
}

// Rows merges the members' applications into one list, worst first.
func Rows(members []Member) []Row {
	byName := map[string]*Row{}
	for _, member := range members {
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
	for _, member := range members {
		switch member.State {
		case Ready:
			s.Answered++
			trouble := member.NodeTrouble()
			s.Nodes += trouble.Total
			s.NodesNotReady += trouble.NotReady
			s.NodesPressure += trouble.Pressure
			s.NodesCordoned += trouble.Cordoned
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
