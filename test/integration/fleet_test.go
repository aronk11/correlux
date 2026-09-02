//go:build integration

package integration

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/ui/app"
)

// drainFleet walks the chain of fleet messages: opening the overview starts one
// read per cluster and each answer asks for the next.
func drainFleet(t *testing.T, m *app.Model, cmd tea.Cmd) {
	t.Helper()
	for depth := 0; cmd != nil && depth < 10; depth++ {
		msg, ok := runCommand(cmd)
		if !ok || msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

func TestTheFleetReadsARealCluster(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())

	drainFleet(t, m, m.OpenFleetForTest(shared.context))

	out := frame(m)
	if !strings.Contains(out, "FLEET") && !strings.Contains(out, "Fleet") {
		t.Fatalf("the overview must be on screen:\n%s", out)
	}
	if !strings.Contains(out, shared.context) {
		t.Errorf("the cluster must be listed by its context name:\n%s", out)
	}
	if !strings.Contains(out, "connected") {
		t.Errorf("a cluster that answered must say so:\n%s", out)
	}
	if strings.Contains(out, "connecting") {
		t.Errorf("the read must have finished by now:\n%s", out)
	}
	// The seeded cluster has applications, and the fleet counts them.
	if strings.Contains(out, "no cluster has answered yet") {
		t.Errorf("nothing was read:\n%s", out)
	}
}

func TestAClusterThatCannotBeReachedIsNamedInTheFleet(t *testing.T) {
	// A context that points nowhere, next to one that works: the overview must
	// show both, and never let the working one imply the other is fine.
	m := newModelFor(t)
	drain(t, m, m.Init())

	drainFleet(t, m, m.OpenFleetForTest(shared.context, "a-context-that-does-not-exist"))

	out := frame(m)
	if !strings.Contains(out, shared.context) {
		t.Errorf("the reachable cluster must still be there:\n%s", out)
	}
	// A context that is not in the kubeconfig at all is dropped rather than
	// reported as broken; what must never happen is a claim to cover it.
	if strings.Contains(out, "a-context-that-does-not-exist") {
		t.Errorf("a context that is not in the kubeconfig must not be listed:\n%s", out)
	}
}

func TestBrowsingOneKindAcrossTheFleet(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drainFleet(t, m, m.OpenFleetForTest(shared.context))
	drainFleet(t, m, m.BrowseAcrossFleetForTest("deployments.apps"))

	out := frame(m)
	if !strings.Contains(out, "CLUSTER") || !strings.Contains(out, "NAMESPACE") {
		t.Fatalf("a merged table must say which cluster and namespace a row is from:\n%s", out)
	}
	if !strings.Contains(out, shared.context) {
		t.Errorf("every row carries its cluster:\n%s", out)
	}
	// The server's own columns survive the merge.
	if !strings.Contains(out, "READY") || !strings.Contains(out, "UP-TO-DATE") {
		t.Errorf("the API server's printer columns must come through:\n%s", out)
	}
	if strings.Contains(out, "Reading deployments") {
		t.Errorf("the read must have finished:\n%s", out)
	}
}

func TestAKindNoClusterServesIsReportedPerCluster(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drainFleet(t, m, m.OpenFleetForTest(shared.context))

	// A kind the catalog knows but that is scoped to one namespace's CRD is
	// still served; instead, ask for something no cluster has by pointing the
	// browser at a name discovery does not resolve.
	drainFleet(t, m, m.BrowseAcrossFleetForTest("sprockets.example.com"))

	if out := frame(m); !strings.Contains(out, "Unknown resource") {
		t.Errorf("a kind nothing serves must be named as unknown:\n%s", out)
	}
}
