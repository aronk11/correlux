package screens

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// The section renderer is shared by the application, object and fleet views, so
// these tests state its rules once: rows line up under their headings, a
// navigable row can be found by line, and nothing is silently dropped.

func sampleSections() []DetailSection {
	return []DetailSection{
		{
			Title:   "Workloads",
			Columns: []string{"Kind", "Name", "Ready"},
			Rows: []DetailRow{
				{Cells: []string{"Deployment", "payments", "0/3"}, Target: 0, Status: theme.StatusCritical},
			},
		},
		{
			Title:   "Pods",
			Columns: []string{"Name", "Phase"},
			Rows: []DetailRow{
				{Cells: []string{"payments-1", "Pending"}, Target: 1},
				{Cells: []string{"payments-2", "Running"}, Target: 2},
			},
		},
		{Title: "Network", Empty: "no service belongs to this application"},
	}
}

func applicationData() ApplicationData {
	return ApplicationData{
		Name: "payments", Namespace: "shop",
		Health: "down", HealthGlyph: "✖", HealthStatus: theme.StatusCritical,
		Summary:  "0 of 3 pods ready",
		Sections: sampleSections(),
		Selected: -1,
	}
}

func TestAnApplicationRendersItsSections(t *testing.T) {
	out := RenderApplication(testTheme(), applicationData(), 80, 40)

	for _, want := range []string{
		"payments", "shop", "down", "0 of 3 pods ready",
		"WORKLOADS", "Deployment", "PODS", "payments-1", "NETWORK",
		"no service belongs to this application",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestAnEmptySectionSaysWhichNothingItIs(t *testing.T) {
	data := applicationData()
	data.Sections = []DetailSection{{Title: "Recent events", Empty: "not read yet"}}

	out := RenderApplication(testTheme(), data, 80, 20)
	if !strings.Contains(out, "not read yet") {
		t.Errorf("an empty section must say why it is empty:\n%s", out)
	}
}

func TestEveryNavigableRowHasALine(t *testing.T) {
	data := applicationData()
	lines := data.TargetLines(80)

	for target := 0; target < 3; target++ {
		line, ok := lines[target]
		if !ok {
			t.Errorf("target %d has no line", target)
			continue
		}
		if line <= 0 || line >= data.LineCount(80) {
			t.Errorf("target %d is on line %d, outside 0..%d", target, line, data.LineCount(80))
		}
	}
	// A row that is not navigable must not claim a line.
	if _, ok := lines[99]; ok {
		t.Error("an unknown target must not resolve to a line")
	}
}

func TestTheSelectedRowIsMarked(t *testing.T) {
	plain := RenderApplication(testTheme(), applicationData(), 80, 40)

	selected := applicationData()
	selected.Selected = 1
	marked := RenderApplication(testTheme(), selected, 80, 40)

	if plain == marked {
		t.Error("the selected row must look different from the rest")
	}
}

func TestScrollingShowsTheLinesAskedFor(t *testing.T) {
	data := applicationData()
	total := data.LineCount(80)

	top := RenderApplication(testTheme(), data, 80, 3)
	if strings.Count(top, "\n") != 2 {
		t.Errorf("a height of three draws three lines:\n%s", top)
	}

	data.Offset = total - 1
	bottom := RenderApplication(testTheme(), data, 80, 3)
	if top == bottom {
		t.Error("an offset must move the window")
	}
}

func TestAMessageReplacesTheWholeBody(t *testing.T) {
	data := applicationData()
	data.Message = "Application payments is no longer in shop."
	data.MessageStatus = theme.StatusWarning

	out := RenderApplication(testTheme(), data, 80, 20)
	if !strings.Contains(out, "no longer in shop") {
		t.Errorf("the message must be shown:\n%s", out)
	}
	if strings.Contains(out, "WORKLOADS") {
		t.Errorf("and nothing else:\n%s", out)
	}
}

func TestNothingIsDrawnIntoNoSpace(t *testing.T) {
	// A terminal mid-resize reports zero, and every renderer must survive it.
	data := applicationData()
	for _, size := range [][2]int{{0, 10}, {10, 0}, {0, 0}, {-5, -5}} {
		if got := RenderApplication(testTheme(), data, size[0], size[1]); got != "" {
			t.Errorf("size %v rendered %q, want nothing", size, got)
		}
		if got := RenderObject(testTheme(), objectData(), size[0], size[1]); got != "" {
			t.Errorf("object at %v rendered %q", size, got)
		}
		if got := RenderFleet(testTheme(), fleetData(), size[0], size[1]); got != "" {
			t.Errorf("fleet at %v rendered %q", size, got)
		}
		if got := RenderLogs(testTheme(), logsData(), size[0], size[1]); got != "" {
			t.Errorf("logs at %v rendered %q", size, got)
		}
	}
}

func TestAVeryNarrowScreenStillRenders(t *testing.T) {
	// Narrow enough that every column has to give something up.
	out := RenderApplication(testTheme(), applicationData(), 12, 20)
	if out == "" {
		t.Fatal("a narrow screen must still draw something")
	}
	for _, line := range strings.Split(out, "\n") {
		if width := lipgloss.Width(line); width > 12 {
			t.Errorf("line %q is %d cells wide, the screen is 12", line, width)
		}
	}
}
