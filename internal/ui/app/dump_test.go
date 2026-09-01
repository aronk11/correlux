package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestDumpFrames writes plain-text renderings of the main screen and each
// overlay to KUBEUI_DUMP_DIR. It is a development aid for reviewing the layout
// without a terminal, and a no-op in normal test runs.
func TestDumpFrames(t *testing.T) {
	dir := os.Getenv("KUBEUI_DUMP_DIR")
	if dir == "" {
		t.Skip("set KUBEUI_DUMP_DIR to dump rendered frames")
	}

	frames := map[string]string{"main": "", "palette": "ctrl+p", "clusters": "ctrl+k", "help": "?"}
	for name, key := range frames {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
		if key != "" {
			press(t, m, key)
		}
		out := ansi.ReplaceAllString(view(m), "")
		if err := os.WriteFile(dir+"/"+name+".txt", []byte(out+"\n"), 0o600); err != nil {
			t.Fatalf("write dump: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s frame is empty", name)
		}
	}
}
