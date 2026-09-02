// Package usage answers two questions about a scope: where the pods are, and
// what they use against what they asked for.
//
// It is pure arithmetic over what has already been read — the pod specs, the
// nodes, and whatever the metrics API said — and it is written so that the
// three ways a number can be absent never collapse into one. A node with no
// sample, a container with no request and a container asking for nothing are
// three different facts, and a view that renders them alike is a view that
// lies (ADR 5).
//
// Nothing here knows about Kubernetes clients or the terminal. Every rule is
// one test with objects written down by hand.
package usage

import (
	"sort"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// Live is what only the cluster can say at this instant: its machines, and —
// where the metrics API is installed — what is running on them right now.
type Live struct {
	Nodes []application.Node
	// NodesReason says why the nodes could not be listed, empty when they
	// were. A namespace-scoped service account routinely may not see them,
	// and that is different from a cluster with no nodes.
	NodesReason string
	Metrics     Metrics
}

// Metrics is what the metrics API reported, and whether it reported at all.
type Metrics struct {
	// Available is false when the API is not installed, not permitted or not
	// answering; Reason then says which. Every number derived from it is
	// absent rather than zero.
	Available bool
	Reason    string
	// Note qualifies a partial answer: pod metrics without node metrics.
	Note string
	// At is when the samples were taken and Window over how long they were
	// averaged. A number with no age is a number nobody can judge.
	At     time.Time
	Window time.Duration
	Nodes  []NodeSample
	Pods   []PodSample
}

// NodeSample and PodSample are one object's current use.
type NodeSample struct {
	Name string
	Used application.Amounts
}

// PodSample is one pod's use, summed over its containers.
type PodSample struct {
	Namespace string
	Name      string
	Used      application.Amounts
}

// NodeUsage is one machine: what is on it, and how full it is.
type NodeUsage struct {
	Node application.Node
	// Pods is how many pods in scope the node is running. Pods that have
	// finished are not counted: they hold no resources and no pod slot.
	Pods int
	// Requests and Limits are summed over those pods.
	Requests application.Amounts
	Limits   application.Amounts
	// Used is what the node reports using right now, absent when no sample
	// covers it.
	Used application.Amounts
	// Unsized counts the pods on this node that request nothing at all. They
	// are the reason a node with 20% requested can still be on its knees.
	Unsized int
}

// AppUsage is one application's share.
type AppUsage struct {
	Name      string
	Namespace string
	Pods      int
	// Measured is how many of those pods a live sample covered. Less than Pods
	// means the total below is a floor, not the answer.
	Measured int
	// Unsized counts pods with no request at all.
	Unsized int
	// Nodes are the machines its pods are spread over, sorted.
	Nodes    []string
	Requests application.Amounts
	Limits   application.Amounts
	Used     application.Amounts
}

// Totals is the whole scope in one row.
type Totals struct {
	Nodes int
	// Pods counts the running pods; Unscheduled the ones no node has taken.
	Pods        int
	Unscheduled int
	Unsized     int
	Capacity    application.Capacity
	Allocatable application.Capacity
	Requests    application.Amounts
	Limits      application.Amounts
	Used        application.Amounts
	// Measured is how many pods a live sample covered.
	Measured int
}

// Unscheduled is one pod that is nowhere yet, with the scheduler's verdict.
type Unscheduled struct {
	Name      string
	Namespace string
	Reason    string
	Requests  application.Amounts
}

// Report is the whole answer.
type Report struct {
	// Scope is the namespace this covers, empty for the whole cluster.
	Scope   string
	Metrics Metrics
	Nodes   []NodeUsage
	Apps    []AppUsage
	Totals  Totals
	// Unscheduled are the pods no node has accepted, worst question first.
	Unscheduled []Unscheduled
	// Notes qualify the report itself: a scope that hides other namespaces'
	// pods, nodes that could not be listed, half the metrics missing.
	Notes []string
}

// maxUnscheduled bounds the list of pods that are nowhere. A cluster that
// cannot schedule anything produces thousands of them and the first handful
// say the same thing as all of them.
const maxUnscheduled = 20

// Build rolls a snapshot up per node and per application.
//
// The node rollup is taken from the snapshot's pods rather than from the
// grouped applications, because a node carries everything in scope — including
// the pods that belong to no application anybody named.
func Build(live Live, snapshot application.Snapshot, apps []application.Application) Report {
	report := Report{Scope: snapshot.Scope, Metrics: live.Metrics}

	nodeSamples := make(map[string]application.Amounts, len(live.Metrics.Nodes))
	for _, s := range live.Metrics.Nodes {
		nodeSamples[s.Name] = s.Used
	}
	podSamples := make(map[string]application.Amounts, len(live.Metrics.Pods))
	for _, s := range live.Metrics.Pods {
		podSamples[s.Namespace+"/"+s.Name] = s.Used
	}

	byNode := map[string]*NodeUsage{}
	for i := range live.Nodes {
		node := live.Nodes[i]
		usage := &NodeUsage{Node: node}
		if sample, ok := nodeSamples[node.Name]; ok {
			usage.Used = sample
		}
		byNode[node.Name] = usage
	}

	for i := range snapshot.Pods {
		pod := &snapshot.Pods[i]
		if !Running(pod) {
			continue
		}
		requests, limits := Requests(pod), Limits(pod)
		report.Totals.Pods++
		if !requests.HasCPU && !requests.HasMemory {
			report.Totals.Unsized++
		}
		report.Totals.Requests = report.Totals.Requests.Add(requests)
		report.Totals.Limits = report.Totals.Limits.Add(limits)
		if sample, ok := podSamples[pod.Namespace+"/"+pod.Name]; ok {
			report.Totals.Used = report.Totals.Used.Add(sample)
			report.Totals.Measured++
		}

		if pod.Node == "" {
			report.Totals.Unscheduled++
			if len(report.Unscheduled) < maxUnscheduled {
				report.Unscheduled = append(report.Unscheduled, Unscheduled{
					Name:      pod.Name,
					Namespace: pod.Namespace,
					Reason:    unscheduledReason(pod),
					Requests:  requests,
				})
			}
			continue
		}

		node, known := byNode[pod.Node]
		if !known {
			// A pod on a node Correlux could not read. Counting it under a node
			// that is not on screen would make the totals disagree with the
			// rows; it stays in the scope totals only.
			continue
		}
		node.Pods++
		node.Requests = node.Requests.Add(requests)
		node.Limits = node.Limits.Add(limits)
		if !requests.HasCPU && !requests.HasMemory {
			node.Unsized++
		}
	}

	for _, node := range byNode {
		report.Totals.Capacity = addCapacity(report.Totals.Capacity, node.Node.Capacity)
		report.Totals.Allocatable = addCapacity(report.Totals.Allocatable, node.Node.Allocatable)
		report.Nodes = append(report.Nodes, *node)
	}
	report.Totals.Nodes = len(report.Nodes)
	sortNodes(report.Nodes)

	report.Apps = buildApps(apps, podSamples)
	report.Notes = notes(live, snapshot)
	return report
}

// buildApps rolls the same pods up the other way: per application, which is
// the unit anybody asking "what is this costing" thinks in.
func buildApps(apps []application.Application, podSamples map[string]application.Amounts) []AppUsage {
	out := make([]AppUsage, 0, len(apps))
	for i := range apps {
		app := &apps[i]
		usage := AppUsage{Name: app.Name, Namespace: app.Namespace}
		nodes := map[string]bool{}
		for j := range app.Pods {
			pod := &app.Pods[j]
			if !Running(pod) {
				continue
			}
			requests := Requests(pod)
			usage.Pods++
			usage.Requests = usage.Requests.Add(requests)
			usage.Limits = usage.Limits.Add(Limits(pod))
			if !requests.HasCPU && !requests.HasMemory {
				usage.Unsized++
			}
			if pod.Node != "" {
				nodes[pod.Node] = true
			}
			if sample, ok := podSamples[pod.Namespace+"/"+pod.Name]; ok {
				usage.Used = usage.Used.Add(sample)
				usage.Measured++
			}
		}
		if usage.Pods == 0 {
			// An application whose pods have all finished holds nothing; it
			// belongs on the dashboard, not in a usage list.
			continue
		}
		usage.Nodes = keys(nodes)
		out = append(out, usage)
	}

	// Biggest first, by what was asked for rather than by what is used: the
	// request is stable between refreshes, so the rows do not reshuffle under
	// a reader every ten seconds.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests.CPUMilli != out[j].Requests.CPUMilli {
			return out[i].Requests.CPUMilli > out[j].Requests.CPUMilli
		}
		if out[i].Requests.MemoryBytes != out[j].Requests.MemoryBytes {
			return out[i].Requests.MemoryBytes > out[j].Requests.MemoryBytes
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}

// sortNodes puts the machines worth looking at first: the ones that are not
// well, then the fullest. Fullness is measured by requests, which do not move
// between two refreshes the way live usage does.
func sortNodes(nodes []NodeUsage) {
	sort.Slice(nodes, func(i, j int) bool {
		a, b := &nodes[i], &nodes[j]
		if trouble(a) != trouble(b) {
			return trouble(a) > trouble(b)
		}
		if pressure(a) != pressure(b) {
			return pressure(a) > pressure(b)
		}
		return a.Node.Name < b.Node.Name
	})
}

// trouble ranks what is wrong with a node the way the fleet overview does.
func trouble(n *NodeUsage) int {
	switch {
	case !n.Node.Ready:
		return 3
	case len(n.Node.Pressure) > 0:
		return 2
	case n.Node.Unschedulable:
		return 1
	default:
		return 0
	}
}

// pressure is how full a node is: whichever of CPU and memory is nearer its
// allocatable, because that is the one that will refuse the next pod.
func pressure(n *NodeUsage) int {
	cpu := Percent(n.Requests.CPUMilli, n.Node.Allocatable.CPUMilli)
	memory := Percent(n.Requests.MemoryBytes, n.Node.Allocatable.MemoryBytes)
	if memory > cpu {
		return memory
	}
	return cpu
}

// notes state what the report does not cover, so a number is never read as
// more than it is.
func notes(live Live, snapshot application.Snapshot) []string {
	var out []string
	if snapshot.Scope != "" {
		out = append(out, "Requests and limits are summed over the pods in "+snapshot.Scope+
			"; other namespaces' pods run on these nodes too.")
	}
	if snapshot.Truncated {
		out = append(out, "The scope was truncated: these totals are a subset.")
	}
	if live.NodesReason != "" {
		out = append(out, "Nodes could not be listed: "+live.NodesReason+".")
	}
	if live.Metrics.Note != "" {
		out = append(out, "Partly measured — "+live.Metrics.Note+".")
	}
	return out
}

// Running reports whether a pod holds resources on a node. A pod that has
// finished — succeeded or failed — holds none and does not occupy a pod slot.
func Running(pod *application.Pod) bool {
	return pod.Phase != "Succeeded" && pod.Phase != "Failed"
}

// Requests is what the scheduler reserves for a pod.
//
// It is not simply the sum of every container: the init containers run one
// after another and before the rest, so the pod needs whichever of them is
// largest, not all of them. A sidecar (an init container that keeps running)
// does add to the sum, because it is still there when the others start.
func Requests(pod *application.Pod) application.Amounts {
	return effective(pod, func(c *application.Container) application.Amounts { return c.Requests })
}

// Limits is the same rollup over what the pod may not exceed. Limits are not
// what the scheduler reserves, but the shape of the sum is the same, and the
// number answers the same question: how much may this run away with.
func Limits(pod *application.Pod) application.Amounts {
	return effective(pod, func(c *application.Container) application.Amounts { return c.Limits })
}

func effective(pod *application.Pod, of func(*application.Container) application.Amounts) application.Amounts {
	var running, init application.Amounts
	for i := range pod.Containers {
		c := &pod.Containers[i]
		switch {
		case c.Init && !c.Sidecar:
			init = init.Max(of(c))
		default:
			running = running.Add(of(c))
		}
	}
	return running.Max(init)
}

// Percent renders part of whole as a whole-number percentage, and -1 when
// there is no denominator. A caller must render -1 as unknown: a node that
// does not report its allocatable is not a node that is empty.
func Percent(part, whole int64) int {
	if whole <= 0 {
		return -1
	}
	return int((part*100 + whole/2) / whole)
}

func addCapacity(a, b application.Capacity) application.Capacity {
	return application.Capacity{
		CPUMilli:    a.CPUMilli + b.CPUMilli,
		MemoryBytes: a.MemoryBytes + b.MemoryBytes,
		Pods:        a.Pods + b.Pods,
	}
}

// unscheduledReason quotes the scheduler when it said something, and falls
// back to whatever the pod itself reports.
func unscheduledReason(pod *application.Pod) string {
	if pod.ScheduledReason != "" {
		return pod.ScheduledReason
	}
	if pod.Reason != "" {
		return pod.Reason
	}
	return pod.Phase
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
