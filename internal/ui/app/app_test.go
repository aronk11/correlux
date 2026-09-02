package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/config"
	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	"github.com/aronk11/kubeui/internal/kube/kubeconfig"
	"github.com/aronk11/kubeui/internal/ui/components"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// newTestModel builds a model over the fixture kubeconfig. No network call is
// ever made: commands are returned but never executed, and cluster state is fed
// in as messages, exactly as the runtime would.
func newTestModel(t *testing.T, opts ...func(*Options)) *Model {
	t.Helper()

	classifier := kubeconfig.DefaultClassifier()
	kc, err := kubeconfig.Load(kubeconfig.LoadOptions{
		ExplicitPath: filepath.Join("testdata", "kubeconfig.yaml"),
		Classifier:   classifier,
	})
	if err != nil {
		t.Fatalf("load fixture kubeconfig: %v", err)
	}

	o := Options{
		Config:      config.Default(),
		Kubeconfig:  kc,
		Factory:     kubeclient.New(kc.Raw(), kc.LoadingRules(), kubeclient.Options{}),
		Classifier:  classifier,
		ContextName: "staging",
		Env:         theme.MapEnv(map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}),
	}
	for _, fn := range opts {
		fn(&o)
	}

	m := New(o)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

func press(t *testing.T, m *Model, keystroke string) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(keyMsg(keystroke))
	return cmd
}

// keyMsg builds the key event for a keystroke like "ctrl+k", "enter" or "a",
// through the same function the application itself uses.
func keyMsg(keystroke string) tea.KeyPressMsg { return keyPress(keystroke) }

// typeInto sends a string one keystroke at a time, the way a person types.
func typeInto(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		press(t, m, string(r))
	}
}

func view(m *Model) string { return m.View().Content }

func TestStartsWithResolvedContextAndNamespace(t *testing.T) {
	m := newTestModel(t)

	if m.Context() != "staging" {
		t.Errorf("context = %q, want staging", m.Context())
	}
	if m.Namespace() != "default" {
		t.Errorf("namespace = %q, want the context default", m.Namespace())
	}
	if out := view(m); !strings.Contains(out, "staging") {
		t.Error("the active context must always be visible in the header")
	}
}

func TestFlagNamespaceWins(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.ContextName = "prod-eu"
		o.Namespace = "billing"
	})
	if m.Namespace() != "billing" {
		t.Errorf("namespace = %q, want the requested billing", m.Namespace())
	}
}

func TestConnectionStatesAreDistinguishable(t *testing.T) {
	m := newTestModel(t)

	if out := view(m); !strings.Contains(out, "connecting") {
		t.Error("before the first probe answers the UI must say it is connecting")
	}

	m.Update(clusterProbedMsg{
		gen: m.cluster.Generation(),
		info: kubeclient.ClusterInfo{
			State:         kubeclient.ConnOK,
			ServerVersion: "v1.31.2",
			Latency:       18 * time.Millisecond,
		},
	})

	out := view(m)
	if !strings.Contains(out, "connected") || !strings.Contains(out, "v1.31.2") {
		t.Errorf("a successful probe must be reflected in the header:\n%s", out)
	}
	if !strings.Contains(out, "18ms") {
		t.Error("probe latency must be shown")
	}
}

func TestUnreachableClusterShowsHintNotRawError(t *testing.T) {
	m := newTestModel(t)
	m.Update(clusterProbedMsg{
		gen: m.cluster.Generation(),
		info: kubeclient.ClusterInfo{
			State: kubeclient.ConnUnreachable,
			Err:   errors.New("dial tcp 10.0.0.1:6443: connect: no route to host"),
			Hint:  "No route to the API server. Are you on the right network or VPN?",
		},
	})

	out := view(m)
	if !strings.Contains(out, "unreachable") {
		t.Error("the failure state must be named")
	}
	if !strings.Contains(out, "VPN") {
		t.Error("the actionable hint must be shown, not just the raw error")
	}
}

func TestStaleProbeIsIgnoredAfterContextSwitch(t *testing.T) {
	m := newTestModel(t)
	stale := m.cluster.Generation()

	m.switchContext("prod-eu")
	m.Update(clusterProbedMsg{
		gen:  stale,
		info: kubeclient.ClusterInfo{State: kubeclient.ConnOK, ServerVersion: "v1.20.0"},
	})

	if strings.Contains(view(m), "v1.20.0") {
		t.Error("a probe answer for the previous cluster must not be displayed")
	}
}

func TestClusterPickerSwitchesContextAndScope(t *testing.T) {
	m := newTestModel(t)

	press(t, m, "ctrl+k")
	if m.overlay != overlayContexts {
		t.Fatal("ctrl+k must open the cluster picker")
	}
	typeInto(t, m, "prod")
	press(t, m, "enter")

	if m.overlay != overlayNone {
		t.Error("choosing a cluster must close the picker")
	}
	if m.Context() != "prod-eu" {
		t.Errorf("context = %q, want prod-eu", m.Context())
	}
	if m.Namespace() != "payments" {
		t.Errorf("namespace = %q, want the new context's namespace", m.Namespace())
	}
}

func TestSwitchingToProductionIsAnnounced(t *testing.T) {
	m := newTestModel(t)
	m.switchContext("prod-eu")

	out := view(m)
	if !strings.Contains(out, "PROD") {
		t.Error("a production context must be badged in the header")
	}
	if !strings.Contains(strings.ToLower(m.message), "production") {
		t.Errorf("switching to production must be called out, message = %q", m.message)
	}
}

func TestEscapeClosesOverlay(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+k")
	press(t, m, "esc")
	if m.overlay != overlayNone {
		t.Error("esc must close the overlay")
	}
}

func TestQuitKeyDoesNotFireWhileTypingInAnOverlay(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+k")
	press(t, m, "q")

	if m.quitting {
		t.Fatal("typing q into a filter must not quit kubeui")
	}
	if got := m.ctxPicker.Query(); got != "q" {
		t.Errorf("query = %q, want the typed q", got)
	}
}

func TestQuitKeyQuitsOutsideOverlays(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "q")
	if !m.quitting {
		t.Error("q must quit when no overlay is open")
	}
}

func TestPaletteFindsCommandsByKeyword(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+p")
	if m.overlay != overlayPalette {
		t.Fatal("ctrl+p must open the command palette")
	}

	typeInto(t, m, "ns")
	found := false
	for _, it := range m.cmdPal.Items() {
		if it.Title == "Switch namespace" {
			found = true
			break
		}
	}
	if !found {
		t.Error("typing the keyword ns must surface the namespace command")
	}
}

func TestPaletteOffersDirectClusterJumps(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+p")
	typeInto(t, m, "prod-eu")

	for _, it := range m.cmdPal.Items() {
		if strings.Contains(it.Title, "Switch to cluster prod-eu") {
			return
		}
	}
	t.Error("every context must be reachable as a direct palette command")
}

func TestPaletteRunsTheSelectedCommand(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+p")
	typeInto(t, m, "all namespaces")
	press(t, m, "enter")

	if !m.allNamespaces {
		t.Error("running the command must apply it")
	}
	if m.overlay != overlayNone {
		t.Error("running a command must close the palette")
	}
	if !strings.Contains(view(m), "all namespaces") {
		t.Error("the scope change must be visible in the header")
	}
}

func TestNamespacePickerDistinguishesLoadingFromEmpty(t *testing.T) {
	m := newTestModel(t)
	m.namespaces.Start()
	press(t, m, "ctrl+o")

	if !hasTitle(m.nsPicker.Items(), "Loading namespaces…") {
		t.Error("a pending request must be shown as loading, never as an empty cluster")
	}

	m.Update(namespacesLoadedMsg{gen: m.namespaces.Generation(), list: kubeclient.NamespaceList{}})
	if !hasTitle(m.nsPicker.Items(), "No namespaces returned") {
		t.Error("an empty result must say so explicitly")
	}
}

func TestNamespacePickerAllowsManualEntryWhenListingIsDenied(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+o")
	m.Update(namespacesLoadedMsg{
		gen:  m.namespaces.Generation(),
		list: kubeclient.NamespaceList{Restricted: true},
	})

	if !hasTitle(m.nsPicker.Items(), "Listing namespaces is not permitted for this user") {
		t.Fatal("a denied list must be explained, not silently empty")
	}

	typeInto(t, m, "payments")
	press(t, m, "enter")
	if m.Namespace() != "payments" {
		t.Errorf("namespace = %q, want the manually typed payments", m.Namespace())
	}
}

func TestNamespacePickerSwitchesScope(t *testing.T) {
	m := newTestModel(t)
	m.Update(namespacesLoadedMsg{
		gen:  m.namespaces.Generation(),
		list: kubeclient.NamespaceList{Names: []string{"kube-system", "payments"}},
	})

	press(t, m, "ctrl+o")
	typeInto(t, m, "pay")
	press(t, m, "enter")

	if m.Namespace() != "payments" {
		t.Errorf("namespace = %q, want payments", m.Namespace())
	}
	if m.allNamespaces {
		t.Error("choosing a namespace must leave all-namespaces scope")
	}
}

func TestHelpOverlayListsRealBindings(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "?")
	if m.overlay != overlayHelp {
		t.Fatal("? must open help")
	}
	out := view(m)
	for _, want := range []string{"Ctrl+P", "Ctrl+K", "Ctrl+O", "Command palette"} {
		if !strings.Contains(out, want) {
			t.Errorf("help must document %q", want)
		}
	}
}

func TestFirstResizeAppliesImmediatelyAndLaterOnesAreDebounced(t *testing.T) {
	m := newTestModel(t)
	if m.screen.Width != 120 {
		t.Fatalf("first size must be applied at once, got %d", m.screen.Width)
	}

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("a later resize must schedule a settle tick")
	}
	if m.screen.Width != 120 {
		t.Error("the layout must not be recomputed until the burst settles")
	}

	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 28}); cmd != nil {
		t.Error("a second event in the same burst must not schedule another tick")
	}

	time.Sleep(2 * m.resize.Interval())
	m.Update(resizeSettledMsg{})
	if m.screen.Width != 90 || m.screen.Height != 28 {
		t.Errorf("after settling the layout is %dx%d, want 90x28", m.screen.Width, m.screen.Height)
	}
}

func TestTinyTerminalShowsAResizeNotice(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	time.Sleep(2 * m.resize.Interval())
	m.Update(resizeSettledMsg{})

	if !strings.Contains(view(m), "needs at least") {
		t.Errorf("a too-small terminal must explain itself:\n%s", view(m))
	}
}

func TestTransientMessageExpires(t *testing.T) {
	m := newTestModel(t)
	m.notice("Refreshing…", theme.StatusUnknown)
	seq := m.messageSeq

	m.Update(messageExpiredMsg{seq: seq - 1})
	if m.message == "" {
		t.Error("an expiry for a superseded message must not clear the current one")
	}

	m.Update(messageExpiredMsg{seq: seq})
	if m.message != "" {
		t.Errorf("message = %q, want it cleared", m.message)
	}
}

func TestContextSwitchDoesNotTouchTheKubeconfigFile(t *testing.T) {
	path := filepath.Join("testdata", "kubeconfig.yaml")
	before := readFile(t, path)

	m := newTestModel(t)
	m.switchContext("prod-eu")

	if after := readFile(t, path); after != before {
		t.Error("switching context inside kubeui must never rewrite the user's kubeconfig")
	}
}

func TestRenderedFrameFillsTheTerminalExactly(t *testing.T) {
	m := newTestModel(t)
	lines := strings.Split(view(m), "\n")
	if len(lines) != 40 {
		t.Errorf("frame has %d lines, want exactly 40", len(lines))
	}
}

func hasTitle(items []components.Item, title string) bool {
	for _, it := range items {
		if it.Title == title {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestOverlaysNeverOverflowTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{200, 60}, {120, 40}, {90, 30}, {80, 24}, {60, 12}} {
		for _, key := range []string{"ctrl+p", "ctrl+k", "ctrl+o", "?"} {
			m := newTestModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			time.Sleep(2 * m.resize.Interval())
			m.Update(resizeSettledMsg{})
			press(t, m, key)

			lines := strings.Split(view(m), "\n")
			if len(lines) > size[1] {
				t.Errorf("%dx%d %s: frame has %d lines", size[0], size[1], key, len(lines))
			}
			for _, line := range lines {
				if w := lipgloss.Width(line); w > size[0] {
					t.Errorf("%dx%d %s: line is %d cells wide", size[0], size[1], key, w)
				}
			}
		}
	}
}
