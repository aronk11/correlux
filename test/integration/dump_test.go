//go:build integration

package integration

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestDumpRealFrames writes plain-text renderings of kubeui against the live
// cluster to KUBEUI_DUMP_DIR. It is a review aid — the fastest way to see what
// the UI actually looks like on real data — and a no-op otherwise.
func TestDumpRealFrames(t *testing.T) {
	dir := os.Getenv("KUBEUI_DUMP_DIR")
	if dir == "" {
		t.Skip("set KUBEUI_DUMP_DIR to dump rendered frames")
	}

	cases := []struct {
		name      string
		namespace string
		resource  string
		key       string
	}{
		{name: "overview"},
		{name: "pods", namespace: "kubeui-load-000", resource: "pods"},
		{name: "widgets", namespace: "kubeui-load-000", resource: "widgets"},
		// The context default namespace is empty: this frame is the honest
		// empty state, which is worth reviewing too.
		{name: "pods-empty-scope", resource: "pods"},
		{name: "resource-picker", key: "ctrl+b"},
	}

	for _, tc := range cases {
		m := newModelFor(t)
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
		drain(t, m, m.Init())

		if tc.namespace != "" {
			drain(t, m, m.SwitchNamespaceForTest(tc.namespace))
		}
		if tc.resource != "" {
			drain(t, m, m.OpenResourceForTest(tc.resource))
		}
		if tc.key != "" {
			_, cmd := m.Update(keyPress(tc.key))
			drain(t, m, cmd)
		}

		out := ansi.ReplaceAllString(frame(m), "")
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s frame is empty", tc.name)
			continue
		}
		if err := os.WriteFile(dir+"/live-"+tc.name+".txt", []byte(out+"\n"), 0o600); err != nil {
			t.Fatalf("write dump: %v", err)
		}
	}
}

// keyPress builds a key event for a ctrl chord such as "ctrl+b".
func keyPress(keystroke string) tea.KeyPressMsg {
	key := tea.Key{}
	rest := strings.TrimPrefix(keystroke, "ctrl+")
	if rest != keystroke {
		key.Mod |= tea.ModCtrl
	}
	r, _ := utf8.DecodeRuneInString(rest)
	key.Code = r
	if key.Mod == 0 {
		key.Text = rest
	}
	return tea.KeyPressMsg(key)
}
