package usage

import (
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// The fixtures below are written the way a cluster expresses these facts:
// millicores and bytes, and an absent request left absent rather than zeroed.

func cpu(milli int64) application.Amounts {
	return application.Amounts{CPUMilli: milli, HasCPU: true}
}

func both(milli, bytes int64) application.Amounts {
	return application.Amounts{CPUMilli: milli, MemoryBytes: bytes, HasCPU: true, HasMemory: true}
}

func container(name string, requests, limits application.Amounts) application.Container {
	return application.Container{Name: name, Requests: requests, Limits: limits}
}

func pod(name, node string, containers ...application.Container) application.Pod {
	return application.Pod{
		Meta:       application.Meta{Kind: "Pod", Name: name, Namespace: "payments"},
		Phase:      "Running",
		Ready:      true,
		Node:       node,
		Containers: containers,
	}
}

func node(name string, cpuMilli, memoryBytes, pods int64) application.Node {
	capacity := application.Capacity{CPUMilli: cpuMilli, MemoryBytes: memoryBytes, Pods: pods}
	return application.Node{
		Meta:        application.Meta{Kind: "Node", Name: name},
		Ready:       true,
		Capacity:    capacity,
		Allocatable: capacity,
	}
}

const gib = int64(1) << 30

func TestPodsAreCountedOnTheNodeTheyRunOn(t *testing.T) {
	snapshot := application.Snapshot{Pods: []application.Pod{
		pod("a", "node-1", container("app", cpu(100), cpu(200))),
		pod("b", "node-1", container("app", cpu(150), cpu(300))),
		pod("c", "node-2", container("app", cpu(250), cpu(500))),
	}}
	live := Live{Nodes: []application.Node{node("node-1", 4000, 8*gib, 110), node("node-2", 4000, 8*gib, 110)}}

	report := Build(live, snapshot, nil)

	if len(report.Nodes) != 2 {
		t.Fatalf("nodes = %d, want both machines", len(report.Nodes))
	}
	byName := map[string]NodeUsage{}
	for _, n := range report.Nodes {
		byName[n.Node.Name] = n
	}
	if got := byName["node-1"]; got.Pods != 2 || got.Requests.CPUMilli != 250 {
		t.Errorf("node-1 = %d pods requesting %dm, want 2 pods requesting 250m",
			got.Pods, got.Requests.CPUMilli)
	}
	if got := byName["node-2"]; got.Pods != 1 || got.Limits.CPUMilli != 500 {
		t.Errorf("node-2 = %d pods limited to %dm, want 1 pod limited to 500m",
			got.Pods, got.Limits.CPUMilli)
	}
	if report.Totals.Pods != 3 || report.Totals.Requests.CPUMilli != 500 {
		t.Errorf("totals = %d pods requesting %dm, want 3 pods requesting 500m",
			report.Totals.Pods, report.Totals.Requests.CPUMilli)
	}
}

func TestFinishedPodsHoldNothing(t *testing.T) {
	succeeded := pod("done", "node-1", container("app", cpu(500), cpu(500)))
	succeeded.Phase = "Succeeded"
	failed := pod("gone", "node-1", container("app", cpu(500), cpu(500)))
	failed.Phase = "Failed"

	report := Build(
		Live{Nodes: []application.Node{node("node-1", 4000, 8*gib, 110)}},
		application.Snapshot{Pods: []application.Pod{succeeded, failed}},
		nil,
	)

	if report.Totals.Pods != 0 || report.Nodes[0].Requests.CPUMilli != 0 {
		t.Errorf("a pod that has finished holds no CPU and no pod slot, got %d pods and %dm",
			report.Totals.Pods, report.Nodes[0].Requests.CPUMilli)
	}
}

func TestAPodWithNoRequestsIsUnsizedNotZero(t *testing.T) {
	report := Build(
		Live{Nodes: []application.Node{node("node-1", 4000, 8*gib, 110)}},
		application.Snapshot{Pods: []application.Pod{
			pod("bare", "node-1", application.Container{Name: "app"}),
		}},
		nil,
	)

	n := report.Nodes[0]
	if n.Unsized != 1 || report.Totals.Unsized != 1 {
		t.Errorf("unsized = %d on the node and %d overall, want 1 and 1", n.Unsized, report.Totals.Unsized)
	}
	if n.Requests.HasCPU || n.Requests.HasMemory {
		t.Error("a node whose pods request nothing must report nothing set, never a zero request")
	}
}

func TestTheInitPhaseContributesItsLargestContainerAndSidecarsAddUp(t *testing.T) {
	init := container("migrate", cpu(2000), application.Amounts{})
	init.Init = true
	small := container("wait", cpu(50), application.Amounts{})
	small.Init = true
	sidecar := container("proxy", cpu(100), application.Amounts{})
	sidecar.Init, sidecar.Sidecar = true, true

	p := pod("api", "node-1", init, small, sidecar, container("app", cpu(500), application.Amounts{}))

	// The regular container and the sidecar run together (600m); the init
	// containers run before them, one at a time, and the largest of them
	// (2000m) is what the pod has to be able to fit.
	if got := Requests(&p).CPUMilli; got != 2000 {
		t.Errorf("pod request = %dm, want the init phase's 2000m", got)
	}

	p.Containers[0].Requests = cpu(100) // the big init container shrinks
	if got := Requests(&p).CPUMilli; got != 600 {
		t.Errorf("pod request = %dm, want the running containers' 600m", got)
	}
}

func TestUnscheduledPodsAreListedWithTheSchedulersVerdict(t *testing.T) {
	waiting := pod("api-7d8f", "", container("app", cpu(500), application.Amounts{}))
	waiting.Phase = "Pending"
	waiting.Scheduled = false
	waiting.ScheduledReason = "Unschedulable"

	report := Build(
		Live{Nodes: []application.Node{node("node-1", 4000, 8*gib, 110)}},
		application.Snapshot{Pods: []application.Pod{waiting}},
		nil,
	)

	if report.Totals.Unscheduled != 1 || len(report.Unscheduled) != 1 {
		t.Fatalf("unscheduled = %d counted, %d listed, want 1 and 1",
			report.Totals.Unscheduled, len(report.Unscheduled))
	}
	if got := report.Unscheduled[0]; got.Reason != "Unschedulable" || got.Requests.CPUMilli != 500 {
		t.Errorf("unscheduled pod = %+v, want the scheduler's reason and what it is asking for", got)
	}
	if report.Nodes[0].Pods != 0 {
		t.Error("a pod with no node must not be counted on one")
	}
}

func TestANodeWithNoSampleIsNotANodeUsingNothing(t *testing.T) {
	live := Live{
		Nodes: []application.Node{node("measured", 4000, 8*gib, 110), node("silent", 4000, 8*gib, 110)},
		Metrics: Metrics{
			Available: true,
			At:        time.Now(),
			Nodes:     []NodeSample{{Name: "measured", Used: both(1000, 2*gib)}},
		},
	}

	report := Build(live, application.Snapshot{}, nil)
	for _, n := range report.Nodes {
		switch n.Node.Name {
		case "measured":
			if !n.Used.HasCPU || n.Used.CPUMilli != 1000 {
				t.Errorf("measured node = %+v, want 1000m of live usage", n.Used)
			}
		case "silent":
			if n.Used.HasCPU || n.Used.HasMemory {
				t.Error("a node the metrics API said nothing about must carry no usage at all")
			}
		}
	}
}

func TestWithoutTheMetricsAPIEverythingElseStillAddsUp(t *testing.T) {
	live := Live{
		Nodes:   []application.Node{node("node-1", 4000, 8*gib, 110)},
		Metrics: Metrics{Available: false, Reason: "the metrics API is not installed in this cluster"},
	}
	snapshot := application.Snapshot{Pods: []application.Pod{
		pod("a", "node-1", container("app", both(500, gib), both(1000, 2*gib))),
	}}

	report := Build(live, snapshot, nil)

	if report.Metrics.Available {
		t.Fatal("the report must carry the absence of metrics, not hide it")
	}
	if report.Totals.Used.HasCPU || report.Totals.Measured != 0 {
		t.Error("without metrics nothing is measured, and no total may claim otherwise")
	}
	n := report.Nodes[0]
	if n.Requests.CPUMilli != 500 || n.Limits.MemoryBytes != 2*gib {
		t.Errorf("node = %+v, want the requests and limits the pod specs carry", n)
	}
	if got := Percent(n.Requests.CPUMilli, n.Node.Allocatable.CPUMilli); got != 13 {
		t.Errorf("requested share = %d%%, want 13%% of the node's 4000m", got)
	}
}

func TestApplicationsAreRolledUpWithWhereTheirPodsLanded(t *testing.T) {
	pods := []application.Pod{
		pod("api-a", "node-1", container("app", both(200, gib), both(400, 2*gib))),
		pod("api-b", "node-2", container("app", both(200, gib), both(400, 2*gib))),
	}
	apps := []application.Application{{Name: "api", Namespace: "payments", Pods: pods}}
	live := Live{
		Nodes: []application.Node{node("node-1", 4000, 8*gib, 110), node("node-2", 4000, 8*gib, 110)},
		Metrics: Metrics{
			Available: true,
			Pods:      []PodSample{{Namespace: "payments", Name: "api-a", Used: both(120, gib/2)}},
		},
	}

	report := Build(live, application.Snapshot{Pods: pods}, apps)

	if len(report.Apps) != 1 {
		t.Fatalf("apps = %d, want one", len(report.Apps))
	}
	got := report.Apps[0]
	if got.Pods != 2 || len(got.Nodes) != 2 {
		t.Errorf("api = %d pods over %d nodes, want 2 and 2", got.Pods, len(got.Nodes))
	}
	if got.Requests.CPUMilli != 400 || got.Limits.CPUMilli != 800 {
		t.Errorf("api = %dm requested, %dm allowed, want 400m and 800m",
			got.Requests.CPUMilli, got.Limits.CPUMilli)
	}
	if got.Measured != 1 || got.Used.CPUMilli != 120 {
		t.Errorf("api = %d of 2 pods measured at %dm; a partial measurement must say so",
			got.Measured, got.Used.CPUMilli)
	}
}

func TestAnApplicationWithNoRunningPodsIsLeftOut(t *testing.T) {
	finished := pod("job-a", "node-1", container("app", cpu(100), cpu(100)))
	finished.Phase = "Succeeded"
	apps := []application.Application{{Name: "nightly", Namespace: "payments",
		Pods: []application.Pod{finished}}}

	report := Build(Live{}, application.Snapshot{}, apps)

	if len(report.Apps) != 0 {
		t.Errorf("apps = %+v, want nothing: an application holding no pod holds no resources", report.Apps)
	}
}

func TestTheWorstNodesSortFirstAndThenTheFullest(t *testing.T) {
	broken := node("broken", 4000, 8*gib, 110)
	broken.Ready = false
	pressured := node("pressured", 4000, 8*gib, 110)
	pressured.Pressure = []string{"MemoryPressure"}
	cordoned := node("cordoned", 4000, 8*gib, 110)
	cordoned.Unschedulable = true

	snapshot := application.Snapshot{Pods: []application.Pod{
		pod("busy", "full", container("app", cpu(3000), cpu(3000))),
		pod("idle", "empty", container("app", cpu(100), cpu(100))),
	}}
	live := Live{Nodes: []application.Node{
		node("empty", 4000, 8*gib, 110), node("full", 4000, 8*gib, 110), cordoned, pressured, broken,
	}}

	report := Build(live, snapshot, nil)

	var order []string
	for _, n := range report.Nodes {
		order = append(order, n.Node.Name)
	}
	want := []string{"broken", "pressured", "cordoned", "full", "empty"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("node order = %v, want %v", order, want)
		}
	}
}

func TestAScopedReportSaysWhatItDoesNotCover(t *testing.T) {
	report := Build(Live{}, application.Snapshot{Scope: "payments"}, nil)
	if len(report.Notes) == 0 {
		t.Fatal("a namespace-scoped report must say the nodes carry other namespaces' pods too")
	}

	report = Build(Live{NodesReason: "not permitted for this user"}, application.Snapshot{}, nil)
	found := false
	for _, note := range report.Notes {
		if note == "Nodes could not be listed: not permitted for this user." {
			found = true
		}
	}
	if !found {
		t.Errorf("notes = %v, want the reason the machines are missing", report.Notes)
	}
}

func TestPercentHasNoAnswerWithoutADenominator(t *testing.T) {
	if got := Percent(500, 0); got != -1 {
		t.Errorf("Percent(500, 0) = %d, want -1: a node that reports no capacity is not an empty node", got)
	}
	if got := Percent(0, 4000); got != 0 {
		t.Errorf("Percent(0, 4000) = %d, want 0: a measured zero is a number", got)
	}
	if got := Percent(4400, 4000); got != 110 {
		t.Errorf("Percent(4400, 4000) = %d, want 110: overcommitment must be visible", got)
	}
}

func TestPodsOnNodesThatCouldNotBeReadStayInTheTotals(t *testing.T) {
	report := Build(
		Live{NodesReason: "not permitted for this user"},
		application.Snapshot{Pods: []application.Pod{
			pod("a", "node-1", container("app", cpu(100), cpu(200))),
		}},
		nil,
	)

	if report.Totals.Pods != 1 || report.Totals.Requests.CPUMilli != 100 {
		t.Errorf("totals = %d pods requesting %dm, want the pod counted even with no node to put it on",
			report.Totals.Pods, report.Totals.Requests.CPUMilli)
	}
	if len(report.Nodes) != 0 {
		t.Error("a node that could not be read must not appear as a row")
	}
}

func TestClusterWideUsageRollsUpEveryRunningPodByNamespace(t *testing.T) {
	payments := pod("payments-0", "node-1", container("app", both(500, 512<<20), both(1000, gib)))
	payments.Namespace = "payments"
	worker := pod("worker-0", "node-2", container("app", both(200, 256<<20), both(400, 512<<20)))
	worker.Namespace = "jobs"
	unscheduled := pod("worker-1", "", container("app", cpu(100), cpu(200)))
	unscheduled.Namespace = "jobs"
	unscheduled.Phase = "Pending"

	apps := []application.Application{
		{Name: "payments", Namespace: "payments", Pods: []application.Pod{payments}},
		{Name: "worker", Namespace: "jobs", Pods: []application.Pod{worker, unscheduled}},
	}
	live := Live{Metrics: Metrics{Available: true, Pods: []PodSample{
		{Namespace: "payments", Name: "payments-0", Used: both(250, 300<<20)},
		{Namespace: "jobs", Name: "worker-0", Used: both(75, 100<<20)},
	}}}

	report := Build(live, application.Snapshot{Pods: []application.Pod{payments, worker, unscheduled}}, apps)
	if len(report.Namespaces) != 2 {
		t.Fatalf("namespaces = %+v, want payments and jobs", report.Namespaces)
	}
	byName := map[string]NamespaceUsage{}
	for _, namespace := range report.Namespaces {
		byName[namespace.Name] = namespace
	}
	if got := byName["payments"]; got.Apps != 1 || got.Pods != 1 || got.Measured != 1 ||
		got.Requests.CPUMilli != 500 || got.Used.CPUMilli != 250 || len(got.Nodes) != 1 {
		t.Errorf("payments = %+v, want one measured app pod on one node", got)
	}
	if got := byName["jobs"]; got.Apps != 1 || got.Pods != 2 || got.Measured != 1 ||
		got.Unscheduled != 1 || got.Requests.CPUMilli != 300 {
		t.Errorf("jobs = %+v, want both pods including the unscheduled one", got)
	}
	if report.Namespaces[0].Name != "payments" {
		t.Errorf("namespace order = %+v, want the largest stable request first", report.Namespaces)
	}
}

func TestNamespacedUsageDoesNotPretendToBeAClusterRollup(t *testing.T) {
	p := pod("payments-0", "node-1", container("app", cpu(100), cpu(200)))
	report := Build(Live{}, application.Snapshot{Scope: "payments", Pods: []application.Pod{p}}, nil)
	if len(report.Namespaces) != 0 {
		t.Errorf("namespaces = %+v, want none for an already-scoped report", report.Namespaces)
	}
}
