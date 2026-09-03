package screens

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func testTheme() *theme.Theme {
	return theme.New(theme.Capabilities{Attributes: true}, config.ThemeAuto)
}

func sampleData() OverviewData {
	return OverviewData{
		Panels: []Panel{
			{
				Title: "Connection",
				Fields: []Field{
					{Label: "Status", Value: "connected", Status: theme.StatusHealthy, Glyph: true},
					{Label: "Server", Value: "https://api.some-very-long-cluster-name.example.com:6443"},
				},
				Note: "A note that is quite long and would happily run past the edge of a narrow panel.",
			},
			{
				Title:  "Session",
				Fields: []Field{{Label: "Context", Value: "prod-eu"}},
			},
		},
		Roadmap: []string{"Application dashboard"},
	}
}

func TestRenderOverviewNeverExceedsItsWidth(t *testing.T) {
	for _, width := range []int{60, 79, 80, 107, 108, 120, 200} {
		out := RenderOverview(testTheme(), sampleData(), width, 30)
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line is %d cells wide: %q", width, w, line)
			}
		}
	}
}

func TestRenderOverviewNeverExceedsItsHeight(t *testing.T) {
	for _, height := range []int{3, 8, 20, 40} {
		out := RenderOverview(testTheme(), sampleData(), 100, height)
		if got := len(strings.Split(out, "\n")); got > height {
			t.Errorf("height %d: rendered %d lines", height, got)
		}
	}
}

func TestRenderOverviewHandlesDegenerateSizes(t *testing.T) {
	if got := RenderOverview(testTheme(), sampleData(), 0, 10); got != "" {
		t.Errorf("zero width must render nothing, got %q", got)
	}
	if got := RenderOverview(testTheme(), sampleData(), 40, 0); got != "" {
		t.Errorf("zero height must render nothing, got %q", got)
	}
}

func TestRoadmapIsLabelledAsNotImplemented(t *testing.T) {
	// An empty area must never be mistaken for an empty cluster.
	out := RenderOverview(testTheme(), sampleData(), 100, 30)
	if !strings.Contains(out, "Not implemented yet") {
		t.Error("planned features must be labelled as such")
	}
}

func TestAProblemPanelColoursItsBorderTitleAndWholeProblemRow(t *testing.T) {
	themeWithColour := theme.New(theme.Capabilities{Color: true, Unicode: true, Dark: true, Attributes: true}, config.ThemeDark)
	plain := renderPanel(themeWithColour, Panel{
		Title: "Cluster problems", Fields: []Field{{Label: "Node/worker", Value: "not ready"}},
	}, 80)
	critical := renderPanel(themeWithColour, Panel{
		Title: "Cluster problems", Status: theme.StatusCritical,
		Fields: []Field{{
			Label: "Node/worker", Value: "not ready", Status: theme.StatusCritical, Glyph: true, Emphasize: true,
		}},
	}, 80)

	if critical == plain {
		t.Fatal("a critical problem panel must not look like a neutral metadata panel")
	}
	styledLabel := themeWithColour.Critical.Bold(true).Render("Node/worker")
	if !strings.Contains(critical, styledLabel) {
		t.Errorf("the problem label itself must carry the critical style:\n%s", critical)
	}
}
