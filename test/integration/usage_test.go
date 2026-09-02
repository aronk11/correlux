//go:build integration

package integration

import (
	"strings"
	"testing"
)

// The usage view is the one screen that asks the cluster about its machines.
// What a real cluster answers — and whether the metrics API answers at all —
// is exactly what a unit test cannot prove.
func TestUsageReadsTheRealNodes(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))
	drain(t, m, m.OpenUsageForTest())

	out := frame(m)
	for _, unwanted := range []string{"Looking for pods", "Measuring", "Could not read"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("the view must be answered by now:\n%s", out)
		}
	}
	for _, want := range []string{"Resource usage in " + seededNamespace, "NODES", "APPLICATIONS", "THIS SCOPE"} {
		if !strings.Contains(out, want) {
			t.Errorf("the view must show %q:\n%s", want, out)
		}
	}
	// kind's control plane is a node like any other, and the seeded pods are
	// on it.
	if !strings.Contains(out, "control-plane") {
		t.Errorf("the cluster's own machines must be listed:\n%s", out)
	}
	if !strings.Contains(out, "app-00") {
		t.Errorf("the seeded applications must have a share:\n%s", out)
	}
	if strings.Contains(out, "this cluster reports no nodes") {
		t.Errorf("a cluster with nodes must not report none:\n%s", out)
	}
}

// Metrics Server is not installed in the test cluster, which is the case the
// view is built to survive: the columns it cannot fill are absent, the ones it
// can are still there, and nothing is drawn as a zero.
func TestUsageWithoutMetricsStillAnswers(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))
	drain(t, m, m.OpenUsageForTest())

	out := frame(m)
	// Whether the metrics API is installed is a property of the cluster, not
	// of kubeui; both answers are correct and each has its own shape.
	if strings.Contains(out, "no live usage") {
		if strings.Contains(out, "CPU USED") {
			t.Errorf("without samples there must be no used column to misread:\n%s", out)
		}
	} else if !strings.Contains(out, "CPU USED") {
		t.Errorf("with metrics available the used column must be there:\n%s", out)
	}
	if !strings.Contains(out, "CPU REQUESTED") {
		t.Errorf("what the pods asked for needs no metrics API:\n%s", out)
	}
	if !strings.Contains(out, "ALLOCATABLE") {
		t.Errorf("what the machines have needs no metrics API either:\n%s", out)
	}
}

// A broken node is the most common thing wrong with a cluster, and the seeder
// leaves one that never became Ready.
func TestUsageShowsANodeThatIsNotReady(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.OpenUsageForTest())

	out := frame(m)
	if !strings.Contains(out, "NotReady") && !strings.Contains(out, "cordoned") {
		t.Skipf("this cluster has no unhealthy node to show:\n%s", out)
	}
	if !strings.Contains(out, "kubeui-load-node") {
		t.Errorf("the node that is not well must be named:\n%s", out)
	}
}
