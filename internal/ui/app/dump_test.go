package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// dumpApplications is a small, deliberately mixed dashboard: a healthy
// application next to a degraded one and one that is down, which is what the
// layout has to stay readable for.
func dumpApplications() []application.Application {
	return []application.Application{
		testApplication("payments", application.Down, 0, 3),
		testApplication("worker", application.Degraded, 7, 8),
		testApplication("api", application.Healthy, 12, 12),
		testApplication("frontend", application.Healthy, 6, 6),
	}
}

// TestDumpFrames writes plain-text renderings of the main screen and each
// overlay to KUBEUI_DUMP_DIR. It is a development aid for reviewing the layout
// without a terminal, and a no-op in normal test runs.
func TestDumpFrames(t *testing.T) {
	dir := os.Getenv("KUBEUI_DUMP_DIR")
	if dir == "" {
		t.Skip("set KUBEUI_DUMP_DIR to dump rendered frames")
	}

	frames := map[string]string{
		"main":        "",
		"application": "",
		"session":     "",
		"palette":     "ctrl+p",
		"clusters":    "ctrl+k",
		"resources":   "ctrl+b",
		"help":        "?",
		"table":       "",
	}
	for name, key := range frames {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
		loadCatalogInto(m, testCatalog())
		loadApplicationsInto(m, dumpApplications()...)
		switch name {
		case "table":
			m.openResource("pods")
			m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage(
				"payments-7d8f9c", "payments-8a91bd", "worker-91abcd", "frontend-2f4e6a")})
		case "application":
			m.openApplication("payments")
		case "session":
			m.backToOverview()
		}
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
