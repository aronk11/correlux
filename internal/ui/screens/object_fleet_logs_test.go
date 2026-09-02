package screens

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/ui/theme"
)

func objectData() ObjectData {
	return ObjectData{
		Kind: "Pod", Name: "payments-7d8f-0", Namespace: "shop",
		Subtitle: "v1   age 3h12m",
		Headline: "Running, not ready", Glyph: "⚠", Status: theme.StatusWarning,
		Sections: []DetailSection{{
			Title:   "Related",
			Columns: []string{"Direction", "Kind", "Name"},
			Rows: []DetailRow{
				{Cells: []string{"controller", "ReplicaSet", "payments-7d8f"}, Target: 0},
			},
		}},
		YAML:     []string{"apiVersion: v1", "kind: Pod", "spec:", "  containers:"},
		Selected: -1,
	}
}

func TestAnObjectShowsItsDetailsOrItsDocument(t *testing.T) {
	details := RenderObject(testTheme(), objectData(), 80, 30)
	if !strings.Contains(details, "RELATED") || !strings.Contains(details, "ReplicaSet") {
		t.Errorf("the details must show what it is related to:\n%s", details)
	}
	if strings.Contains(details, "apiVersion") {
		t.Errorf("the document belongs behind the toggle:\n%s", details)
	}

	data := objectData()
	data.ShowYAML = true
	document := RenderObject(testTheme(), data, 80, 30)
	if !strings.Contains(document, "apiVersion: v1") {
		t.Errorf("the document must be shown when asked for:\n%s", document)
	}
	if strings.Contains(document, "RELATED") {
		t.Errorf("and only the document:\n%s", document)
	}
}

func TestTheDocumentHasNothingToSelect(t *testing.T) {
	data := objectData()
	data.ShowYAML = true
	if lines := data.TargetLines(80); len(lines) != 0 {
		t.Errorf("targets = %v, want none in the document view", lines)
	}
}

func TestALongDocumentIsClippedNotWrapped(t *testing.T) {
	// Indentation carries meaning in YAML; a reflowed document is a lie about
	// its structure.
	data := objectData()
	data.ShowYAML = true
	data.YAML = []string{strings.Repeat("x", 200)}

	out := RenderObject(testTheme(), data, 40, 10)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 200 {
			t.Errorf("line was wrapped rather than clipped: %q", line)
		}
	}
}

func fleetData() FleetData {
	return FleetData{
		Title:    "Fleet",
		Subtitle: "3 clusters   1 unreachable",
		Sections: []DetailSection{{
			Title:   "Clusters",
			Columns: []string{"Cluster", "State"},
			Rows: []DetailRow{
				{Cells: []string{"prod-eu  PROD", "connected"}, Target: 0, Status: theme.StatusHealthy},
				{Cells: []string{"sandbox", "unreachable"}, Target: 1, Status: theme.StatusCritical},
			},
		}},
		Selected: -1,
	}
}

func TestTheFleetRendersItsClusters(t *testing.T) {
	out := RenderFleet(testTheme(), fleetData(), 80, 20)
	for _, want := range []string{"Fleet", "1 unreachable", "CLUSTERS", "prod-eu", "sandbox"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if lines := fleetData().TargetLines(80); len(lines) != 2 {
		t.Errorf("both cluster rows must be selectable, got %v", lines)
	}
}

func TestAnUnconfiguredFleetSaysWhatToDo(t *testing.T) {
	data := fleetData()
	data.Sections = nil
	data.Message = "No fleet configured. List the contexts to watch under `fleet:` in your config."

	out := RenderFleet(testTheme(), data, 60, 10)
	if !strings.Contains(out, "No fleet configured") {
		t.Errorf("the message must be shown:\n%s", out)
	}
	// It is prose, so it wraps rather than being cut at the edge.
	if strings.Count(out, "\n") == 0 {
		t.Errorf("a long message must wrap into the width given:\n%s", out)
	}
}

func logsData() LogsData {
	return LogsData{
		Title:    "Logs of Deployment/payments",
		Subtitle: "3 containers   following   4 lines",
		Lines: []LogLine{
			{Source: "payments-1", Text: "listening on :8080"},
			{Source: "payments-2", Text: "connected to postgres", Time: "10:00:01.500"},
			{Source: "payments-3", Text: "[payments-3: container is waiting to start]", Status: theme.StatusWarning},
		},
		ShowSource: true,
	}
}

func TestLogsAreAttributedAndTimestamped(t *testing.T) {
	out := RenderLogs(testTheme(), logsData(), 100, 20)
	for _, want := range []string{"Logs of Deployment/payments", "payments-1", "listening on :8080", "10:00:01.500"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestAnEmptyLogSaysSoRatherThanShowingNothing(t *testing.T) {
	data := logsData()
	data.Lines = nil
	if out := RenderLogs(testTheme(), data, 80, 10); !strings.Contains(out, "No output yet") {
		t.Errorf("an empty log must say it is empty:\n%s", out)
	}
}

func TestFollowingPinsTheViewToTheEnd(t *testing.T) {
	data := logsData()
	for i := 0; i < 100; i++ {
		data.Lines = append(data.Lines, LogLine{Source: "payments-1", Text: "line " + itoaTest(i)})
	}
	data.Follow = true
	data.Offset = 0 // following overrides whatever the offset says

	out := RenderLogs(testTheme(), data, 80, 6)
	if !strings.Contains(out, "line 99") {
		t.Errorf("a followed log shows its end:\n%s", out)
	}
}

func TestLongLinesAreClippedUnlessWrappingIsAskedFor(t *testing.T) {
	data := logsData()
	data.Lines = []LogLine{{Source: "payments-1", Text: strings.Repeat("word ", 60)}}

	clipped := RenderLogs(testTheme(), data, 40, 10)
	if strings.Count(clipped, "\n") > 3 {
		t.Errorf("clipped output must stay on one line per log line:\n%s", clipped)
	}

	data.Wrap = true
	wrapped := RenderLogs(testTheme(), data, 40, 10)
	if strings.Count(wrapped, "\n") <= strings.Count(clipped, "\n") {
		t.Errorf("wrapping must produce more lines:\n%s", wrapped)
	}
}

func TestAWordLongerThanTheScreenIsBrokenRatherThanLost(t *testing.T) {
	// A base64 blob has no spaces and still has to fit.
	chunks := wrapText(strings.Repeat("x", 100), 20)
	if len(chunks) < 5 {
		t.Fatalf("got %d chunks, want the word broken across the width", len(chunks))
	}
	joined := strings.Join(chunks, "")
	if len(joined) < 100 {
		t.Errorf("characters were lost: %d of 100", len(joined))
	}
}

// itoaTest keeps the fixtures free of strconv.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
