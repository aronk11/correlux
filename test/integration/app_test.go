//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/kube/kubeconfig"
	"github.com/aronk11/correlux/internal/ui/app"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// newModelFor builds the real application against the real cluster.
func newModelFor(t testing.TB) *app.Model {
	t.Helper()
	m := app.New(app.Options{
		Config:      config.Default(),
		Kubeconfig:  shared.config,
		Factory:     shared.factory,
		Classifier:  kubeconfig.DefaultClassifier(),
		ContextName: shared.context,
		Env:         theme.MapEnv(map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}),
	})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	return m
}

// drainTimeout bounds a single command. Some of the application's commands are
// timers (a status message expiring after eight seconds); a test runs them for
// real and must not wait for them.
const drainTimeout = 5 * time.Second

// drain runs a command and feeds its message back into the model, the way the
// Bubble Tea runtime would. Commands are plain functions, which is what makes an
// end-to-end UI test against a live cluster possible at all.
func drain(t testing.TB, m *app.Model, cmd tea.Cmd) {
	t.Helper()
	for depth := 0; cmd != nil && depth < 20; depth++ {
		msg, ok := runCommand(cmd)
		if !ok || msg == nil {
			return
		}
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			for _, sub := range batch {
				drain(t, m, sub)
			}
			return
		}
		_, cmd = m.Update(msg)
	}
}

// runCommand executes cmd, giving up on anything that is really a timer.
func runCommand(cmd tea.Cmd) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(drainTimeout):
		return nil, false
	}
}

func frame(m *app.Model) string { return m.View().Content }

func TestApplicationRendersARealCluster(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())

	// The first screen is the application dashboard, in the context's own
	// namespace — which on a fresh kind cluster is legitimately empty.
	out := frame(m)
	if !strings.Contains(out, "connected") {
		t.Errorf("the header must show the live connection:\n%s", out)
	}
	if !strings.Contains(out, "Applications") {
		t.Errorf("Correlux must open on the applications:\n%s", out)
	}
	if strings.Contains(out, "Looking for applications") {
		t.Errorf("after Init the dashboard must be loaded:\n%s", out)
	}

	// The session view still answers "what am I connected to?".
	drain(t, m, m.ShowSessionForTest())
	session := frame(m)
	if !strings.Contains(session, "listable") {
		t.Errorf("the session view must report what discovery found:\n%s", session)
	}
	if strings.Contains(session, "not discovered") || strings.Contains(session, "not loaded") {
		t.Errorf("after Init everything must be loaded:\n%s", session)
	}
}

func TestBrowsingToPodsShowsRealRows(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())

	// The kind context starts in "default", which is legitimately empty.
	drain(t, m, m.SwitchNamespaceForTest("kube-system"))
	drain(t, m, m.OpenResourceForTest("pods"))
	out := frame(m)

	if !strings.Contains(out, "NAME") || !strings.Contains(out, "STATUS") {
		t.Errorf("the pod table must render the server's columns:\n%s", out)
	}
	if strings.Contains(out, "Loading pods…") {
		t.Errorf("the table must be loaded by now:\n%s", out)
	}
	if !strings.Contains(out, "kube-apiserver") && !strings.Contains(out, "etcd") {
		t.Errorf("the real pods of kube-system must be listed:\n%s", out)
	}
}

func TestBrowsingToACustomResourceShowsItsPrinterColumns(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())

	drain(t, m, m.SwitchNamespaceForTest("correlux-load-000"))
	drain(t, m, m.OpenResourceForTest("widgets"))
	out := frame(m)

	for _, want := range []string{"PHASE", "SIZE", "widget-"} {
		if !strings.Contains(out, want) {
			t.Errorf("a custom resource must render like a native one, missing %q:\n%s", want, out)
		}
	}
}

func TestUnhealthyPodsAreVisibleInTheTable(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest("correlux-load-000"))
	drain(t, m, m.OpenResourceForTest("pods"))

	out := frame(m)
	broken := []string{"CrashLoopBackOff", "ImagePullBackOff", "OOMKilled", "Error"}
	for _, state := range broken {
		if strings.Contains(out, state) {
			return
		}
	}
	t.Errorf("the seeded breakage must be visible, expected one of %v:\n%s", broken, out)
}
