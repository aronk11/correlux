package app

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/usage"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// dumpApplications is a small, deliberately mixed dashboard: a healthy
// application next to a degraded one and one that is down, which is what the
// layout has to stay readable for.
func dumpApplications() []application.Application {
	return []application.Application{
		brokenApplication(),
		testApplication("worker", application.Degraded, 7, 8),
		testApplication("api", application.Healthy, 12, 12),
		testApplication("frontend", application.Healthy, 6, 6),
	}
}

// dumpEvidence is what the cluster would have said about the broken
// application in dumpApplications.
func dumpEvidence() application.Context {
	return application.Context{
		Events: []application.Event{{
			Type: "Warning", Reason: "BackOff", Count: 41,
			Message:  "Back-off restarting failed container payments in pod payments-7d8f-0",
			LastSeen: time.Now().Add(-90 * time.Second),
			About:    application.ObjectRef{Kind: "Pod", Name: "payments-7d8f-0"},
		}},
		Endpoints: []application.EndpointSet{{Service: "payments", Namespace: "default"}},
	}
}

// dumpUsage is a small cluster with one machine under load and one that the
// metrics API has not reported on.
func dumpUsage() usage.Live {
	return usage.Live{
		Nodes: []application.Node{
			usageNode("node-1", true, 4000, 8<<30, 110),
			usageNode("node-2", true, 4000, 8<<30, 110),
		},
		Metrics: liveMetrics([]usage.NodeSample{{Name: "node-1", Used: application.Amounts{
			CPUMilli: 2600, MemoryBytes: 5 << 30, HasCPU: true, HasMemory: true}}}, nil),
	}
}

// dumpUsageScope is what those machines are running: an application sized by
// its spec, and one that asked for nothing at all.
func dumpUsageScope() ([]application.Application, []application.Pod) {
	pods := []application.Pod{
		usagePod("api-0", "node-1", 250, 512<<20),
		usagePod("api-1", "node-2", 250, 512<<20),
		usagePod("batch-0", "node-1", 0, 0),
	}
	return []application.Application{
		usageApplication("api", pods[0], pods[1]),
		usageApplication("batch", pods[2]),
	}, pods
}

// TestDumpFrames writes plain-text renderings of the main screen and each
// overlay to CORRELUX_DUMP_DIR. It is a development aid for reviewing the
// layout without a terminal, and a no-op in normal test runs.
func TestDumpFrames(t *testing.T) {
	dir := os.Getenv("CORRELUX_DUMP_DIR")
	if dir == "" {
		t.Skip("set CORRELUX_DUMP_DIR to dump rendered frames")
	}

	frames := map[string]string{
		"main":        "",
		"application": "",
		"why":         "",
		"session":     "",
		"palette":     "ctrl+p",
		"clusters":    "ctrl+k",
		"resources":   "ctrl+b",
		"help":        "?",
		"table":       "",
		"usage":       "",
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
			loadEvidenceInto(m, dumpEvidence())
		case "why":
			m.openApplication("payments")
			loadEvidenceInto(m, dumpEvidence())
			m.explain()
		case "usage":
			press(t, m, "u")
			apps, pods := dumpUsageScope()
			loadScopeInto(m, apps, pods...)
			m.Update(usageLoadedMsg{gen: m.usage.Generation(), live: dumpUsage()})
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
