package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/domain/usage"
	"github.com/aronk11/kubeui/internal/ui/async"
)

// usagePod is one pod with one container, placed on a node and asking for
// something. Zero amounts mean the spec set none, which is the case the whole
// view is careful about.
func usagePod(name, node string, cpuMilli, memBytes int64) application.Pod {
	c := application.Container{Name: "app", State: "running", Ready: true}
	if cpuMilli > 0 {
		c.Requests.CPUMilli, c.Requests.HasCPU = cpuMilli, true
		c.Limits.CPUMilli, c.Limits.HasCPU = cpuMilli*2, true
	}
	if memBytes > 0 {
		c.Requests.MemoryBytes, c.Requests.HasMemory = memBytes, true
		c.Limits.MemoryBytes, c.Limits.HasMemory = memBytes*2, true
	}
	return application.Pod{
		Meta:       application.Meta{Kind: "Pod", Name: name, Namespace: "default"},
		Phase:      "Running",
		Ready:      true,
		Node:       node,
		Scheduled:  node != "",
		Containers: []application.Container{c},
	}
}

func usageNode(name string, ready bool, cpuMilli, memBytes, pods int64) application.Node {
	capacity := application.Capacity{CPUMilli: cpuMilli, MemoryBytes: memBytes, Pods: pods}
	return application.Node{
		Meta:        application.Meta{Kind: "Node", Name: name},
		Ready:       ready,
		Capacity:    capacity,
		Allocatable: capacity,
	}
}

// usageApplication wraps pods into an application the way the dashboard would.
func usageApplication(name string, pods ...application.Pod) application.Application {
	return application.Application{
		Name: name, Namespace: "default",
		Health:      application.Healthy,
		ReadyPods:   int32(len(pods)),
		DesiredPods: int32(len(pods)),
		Pods:        pods,
	}
}

// loadScopeInto feeds the dashboard *and* the snapshot behind it: the usage
// view rolls the node numbers up from the snapshot's pods, not from the
// grouped applications.
func loadScopeInto(m *Model, apps []application.Application, pods ...application.Pod) {
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{Apps: apps, Snapshot: application.Snapshot{
			Scope: "default", Pods: pods, FetchedAt: time.Now(),
		}},
	})
}

func loadUsageInto(m *Model, live usage.Live) {
	m.Update(usageLoadedMsg{gen: m.usage.Generation(), live: live})
}

// openUsageWith is the whole path a user takes: press the key, let the two
// requests answer.
func openUsageWith(t *testing.T, m *Model, live usage.Live, apps []application.Application, pods ...application.Pod) {
	t.Helper()
	press(t, m, "u")
	loadScopeInto(m, apps, pods...)
	loadUsageInto(m, live)
	if m.view != viewUsage {
		t.Fatalf("u must open the usage view, got view %v", m.view)
	}
}

// liveMetrics is a metrics API that answered for everything asked of it.
func liveMetrics(nodes []usage.NodeSample, pods []usage.PodSample) usage.Metrics {
	return usage.Metrics{
		Available: true,
		At:        time.Now().Add(-30 * time.Second),
		Window:    30 * time.Second,
		Nodes:     nodes, Pods: pods,
	}
}

func TestUsageKeyOpensAndClosesTheView(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("api", application.Healthy, 2, 2))

	press(t, m, "u")
	if m.view != viewUsage {
		t.Fatalf("u must open the usage view, got %v", m.view)
	}
	press(t, m, "u")
	if m.view != viewApplications {
		t.Fatalf("u again must return to the dashboard, got %v", m.view)
	}

	press(t, m, "u")
	press(t, m, "esc")
	if m.view != viewApplications {
		t.Fatalf("esc must leave the usage view, got %v", m.view)
	}
}

func TestUsageSaysWhichAnswerItIsWaitingFor(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "u")

	if out := view(m); !strings.Contains(out, "Looking for pods") {
		t.Errorf("with no snapshot yet the view must say so:\n%s", out)
	}

	loadScopeInto(m, nil)
	if out := view(m); !strings.Contains(out, "Measuring") {
		t.Errorf("with the pods in and the nodes still out, the view must say so:\n%s", out)
	}

	loadUsageInto(m, usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}})
	out := view(m)
	for _, unwanted := range []string{"Looking for pods", "Measuring"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an answered request must not still claim to be loading:\n%s", out)
		}
	}
	if !strings.Contains(out, "node-1") {
		t.Errorf("the node must be on screen:\n%s", out)
	}
}

func TestUsageWithoutMetricsShowsNoLiveColumns(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{
			Nodes:   []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)},
			Metrics: usage.Metrics{Reason: "Metrics Server is not installed"},
		},
		[]application.Application{usageApplication("api", usagePod("api-0", "node-1", 100, 128<<20))},
		usagePod("api-0", "node-1", 100, 128<<20),
	)

	out := view(m)
	if !strings.Contains(out, "no live usage — Metrics Server is not installed") {
		t.Errorf("a missing metrics API must be named, not implied:\n%s", out)
	}
	if strings.Contains(out, "CPU USED") {
		t.Errorf("without samples there must be no used column at all:\n%s", out)
	}
	if !strings.Contains(out, "CPU REQUESTED") {
		t.Errorf("what the pods asked for needs no metrics API:\n%s", out)
	}
	if !strings.Contains(out, "none of them needs Metrics Server") {
		t.Errorf("the view must say the numbers it does show are still whole:\n%s", out)
	}
}

func TestUsageWithMetricsShowsWhatIsRunningNow(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{
			Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)},
			Metrics: liveMetrics(
				[]usage.NodeSample{{Name: "node-1", Used: application.Amounts{
					CPUMilli: 2000, MemoryBytes: 4 << 30, HasCPU: true, HasMemory: true}}},
				[]usage.PodSample{{Namespace: "default", Name: "api-0", Used: application.Amounts{
					CPUMilli: 50, MemoryBytes: 64 << 20, HasCPU: true, HasMemory: true}}}),
		},
		[]application.Application{usageApplication("api", usagePod("api-0", "node-1", 100, 128<<20))},
		usagePod("api-0", "node-1", 100, 128<<20),
	)

	out := view(m)
	for _, want := range []string{"live usage measured", "CPU USED", "MEM USED", "50%"} {
		if !strings.Contains(out, want) {
			t.Errorf("the view must show %q:\n%s", want, out)
		}
	}
}

func TestNodeWithNoSampleIsNotReportedAsIdle(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{
			Nodes: []application.Node{
				usageNode("node-1", true, 4000, 8<<30, 110),
				usageNode("node-2", true, 4000, 8<<30, 110),
			},
			Metrics: liveMetrics([]usage.NodeSample{{Name: "node-1", Used: application.Amounts{
				CPUMilli: 2000, HasCPU: true, HasMemory: true}}}, nil),
		},
		nil,
	)

	if out := view(m); !strings.Contains(out, "no sample") {
		t.Errorf("a node the metrics API said nothing about must say so, not read as 0%%:\n%s", out)
	}
}

func TestPodsThatAskForNothingAreNotReportedAsZero(t *testing.T) {
	m := newTestModel(t)
	pod := usagePod("batch-0", "node-1", 0, 0)
	openUsageWith(t, m,
		usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}},
		[]application.Application{usageApplication("batch", pod)},
		pod,
	)

	if out := view(m); !strings.Contains(out, "none set") {
		t.Errorf("a node whose pods reserve nothing must say so, not read as empty:\n%s", out)
	}
}

func TestUnscheduledPodsAreNamedWithTheSchedulersReason(t *testing.T) {
	m := newTestModel(t)
	pending := usagePod("api-2", "", 100, 128<<20)
	pending.Phase, pending.Scheduled = "Pending", false
	pending.ScheduledReason = "Insufficient cpu"

	openUsageWith(t, m,
		usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}},
		[]application.Application{usageApplication("api", pending)},
		pending,
	)

	out := view(m)
	for _, want := range []string{"NOT SCHEDULED", "api-2", "Insufficient cpu", "1 not scheduled"} {
		if !strings.Contains(out, want) {
			t.Errorf("a pod no node took must be shown with %q:\n%s", want, out)
		}
	}
}

func TestNodesThatCannotBeListedSayWhyRatherThanNone(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{NodesReason: "nodes is forbidden for this service account"},
		[]application.Application{usageApplication("api", usagePod("api-0", "node-1", 100, 128<<20))},
		usagePod("api-0", "node-1", 100, 128<<20),
	)

	if out := view(m); !strings.Contains(out, "not readable — nodes is forbidden") {
		t.Errorf("nodes this user may not see must not read as a cluster with none:\n%s", out)
	}
}

func TestUsageFailureIsNotAnEmptyCluster(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "u")
	loadScopeInto(m, nil)
	m.Update(usageLoadedMsg{gen: m.usage.Generation(), err: errors.New("context deadline exceeded")})

	out := view(m)
	if !strings.Contains(out, "Could not read the nodes") {
		t.Errorf("a failed measurement must say it failed:\n%s", out)
	}
	if m.usage.State() != async.Failed {
		t.Errorf("the value must be Failed, got %v", m.usage.State())
	}
}

func TestSwitchingClusterForgetsTheMeasurement(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}},
		nil,
	)
	if m.usage.State() != async.Ready {
		t.Fatalf("precondition: the reading must be in, got %v", m.usage.State())
	}

	m.switchContext("prod-eu")
	if m.usage.State() == async.Ready {
		t.Error("numbers measured in one cluster must never describe another")
	}
	if m.usageLive.Nodes != nil {
		t.Error("the live reading must be dropped with the cluster it came from")
	}
}

func TestUsageScrollsAndStaysInsideTheView(t *testing.T) {
	m := newTestModel(t)
	nodes := make([]application.Node, 0, 30)
	pods := make([]application.Pod, 0, 30)
	for i := range 30 {
		name := "node-" + itoa(i)
		nodes = append(nodes, usageNode(name, true, 4000, 8<<30, 110))
		pods = append(pods, usagePod("api-"+itoa(i), name, 100, 128<<20))
	}
	openUsageWith(t, m, usage.Live{Nodes: nodes},
		[]application.Application{usageApplication("api", pods...)}, pods...)

	press(t, m, "down")
	if m.usagePort.Offset != 1 {
		t.Fatalf("down must scroll one line, offset is %d", m.usagePort.Offset)
	}
	press(t, m, "home")
	if m.usagePort.Offset != 0 {
		t.Fatalf("home must return to the top, offset is %d", m.usagePort.Offset)
	}

	press(t, m, "end")
	end := m.usagePort.Offset
	if end == 0 {
		t.Fatal("end must scroll to the bottom of a view taller than the screen")
	}
	press(t, m, "down")
	if m.usagePort.Offset != end {
		t.Errorf("scrolling past the last line must stop there, %d became %d", end, m.usagePort.Offset)
	}
}

func TestUsageIsRolledUpAgainstTheNewestSnapshot(t *testing.T) {
	m := newTestModel(t)
	first := usagePod("api-0", "node-1", 100, 128<<20)
	openUsageWith(t, m,
		usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}},
		[]application.Application{usageApplication("api", first)}, first,
	)
	if got := m.usage.Get().Totals.Pods; got != 1 {
		t.Fatalf("precondition: one pod in scope, got %d", got)
	}

	second := usagePod("api-1", "node-1", 100, 128<<20)
	loadScopeInto(m, []application.Application{usageApplication("api", first, second)}, first, second)

	if got := m.usage.Get().Totals.Pods; got != 2 {
		t.Errorf("a reloaded dashboard must be rolled up again, still counting %d pods", got)
	}
}

func TestRefreshMeasuresAgain(t *testing.T) {
	m := newTestModel(t)
	openUsageWith(t, m,
		usage.Live{Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)}},
		nil,
	)

	gen := m.usage.Generation()
	if cmd := press(t, m, "ctrl+r"); cmd == nil {
		t.Fatal("refresh must issue a command")
	}
	if m.usage.Generation() == gen {
		t.Error("refresh on the usage view must ask the cluster again")
	}
}

func TestUsageIsInTheCommandPalette(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("api", application.Healthy, 2, 2))
	press(t, m, "ctrl+p")
	typeInto(t, m, "usage")

	if !hasTitle(m.cmdPal.Items(), "Show resource usage") {
		t.Errorf("the palette must offer the usage view, got %v", m.cmdPal.Items())
	}
	press(t, m, "enter")
	if m.view != viewUsage {
		t.Fatalf("running it must open the view, got %v", m.view)
	}
}

// The dump frame is a development aid; the usage view belongs in it for the
// same reason every other screen does.
func TestUsageFrameRendersAtEightyColumns(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	time.Sleep(2 * m.resize.Interval())
	m.Update(resizeSettledMsg{})
	openUsageWith(t, m,
		usage.Live{
			Nodes: []application.Node{usageNode("node-1", true, 4000, 8<<30, 110)},
			Metrics: liveMetrics([]usage.NodeSample{{Name: "node-1", Used: application.Amounts{
				CPUMilli: 2000, MemoryBytes: 4 << 30, HasCPU: true, HasMemory: true}}}, nil),
		},
		[]application.Application{usageApplication("api", usagePod("api-0", "node-1", 100, 128<<20))},
		usagePod("api-0", "node-1", 100, 128<<20),
	)

	for _, line := range strings.Split(view(m), "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("a line is %d columns wide on an 80-column terminal: %q", width, line)
		}
	}
}
