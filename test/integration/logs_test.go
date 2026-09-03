//go:build integration

package integration

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/app"
)

// corednsPod finds a pod that actually writes something. The seeded pods have
// no kubelet behind them and never will; the control plane's own pods are the
// only real containers in a kind cluster.
func corednsPod(t *testing.T) string {
	t.Helper()
	res, ok := catalogFor(t).Lookup("pods")
	if !ok {
		t.Fatal("a cluster always serves Pods")
	}
	table, err := shared.factory.ListTable(ctx(t), shared.context, res,
		resources.ListOptions{Namespace: "kube-system", Limit: 50})
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}
	for _, row := range table.Rows {
		if strings.HasPrefix(row.Name, "coredns-") {
			return row.Name
		}
	}
	t.Skip("no coredns pod in this cluster")
	return ""
}

// step runs one command and feeds its message back, returning whatever the
// model asks for next. Unlike drain it does not follow the chain, which matters
// for a log stream: following it would never end.
func step(t *testing.T, m *app.Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg, ok := runCommand(cmd)
	if !ok || msg == nil {
		t.Fatal("the command produced nothing")
	}
	_, next := m.Update(msg)
	return next
}

func TestReadingTheLogsOfARealContainer(t *testing.T) {
	pod := corednsPod(t)

	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest("kube-system"))

	// Open, then walk the stream by hand: it is a follow, so it has no end to
	// drain to.
	next := step(t, m, m.OpenLogsForTest("Pod", pod, "kube-system"))
	if next == nil {
		t.Fatal("opening a log must ask for the first batch")
	}
	step(t, m, next)

	out := frame(m)
	if !strings.Contains(out, "Logs of Pod/"+pod) {
		t.Errorf("the view must name what it is reading:\n%s", out)
	}
	if !strings.Contains(out, "following") {
		t.Errorf("a log opens following:\n%s", out)
	}
	// CoreDNS announces itself on start-up; whatever it says, the view must not
	// still be empty.
	if strings.Contains(out, "No output yet.") {
		t.Errorf("nothing was read from a container that writes:\n%s", out)
	}
}

func TestAContainerThatCannotBeReadIsReportedNotHidden(t *testing.T) {
	// A seeded pod has no container behind it, so the kubelet cannot serve its
	// log. That must appear as a line, not as an empty screen.
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))

	apps, _ := applicationsIn(t, seededNamespace)
	if len(apps) == 0 || len(apps[0].Pods) == 0 {
		t.Skip("no seeded pods; run `task kind:seed`")
	}
	pod := apps[0].Pods[0]

	next := step(t, m, m.OpenLogsForTest("Pod", pod.Name, pod.Namespace))
	step(t, m, next)

	out := frame(m)
	if !strings.Contains(out, "unreadable:") && !strings.Contains(out, pod.Name) {
		t.Errorf("a source that cannot be read must be named:\n%s", out)
	}
}
