package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// FleetData is several clusters at once.
//
// It is built from the same sections the object and application views are, for
// the same reason: the keys that move through a list of pods must move through
// a list of clusters, and a reader who has learned one screen has learned this
// one.
type FleetData struct {
	Title    string
	Subtitle string
	Sections []DetailSection
	Selected int
	Offset   int

	// Message replaces the body: no fleet configured, nothing read yet.
	Message       string
	MessageStatus theme.Status
}

// RenderFleet draws the overview.
func RenderFleet(t *theme.Theme, d FleetData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return strings.Join(wrapInto(d.Message, width, "", t), "\n")
	}

	lines, _ := fleetLines(t, d, width)
	offset := clamp(d.Offset, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// LineCount reports how tall the view is.
func (d FleetData) LineCount(width int) int {
	lines, _ := fleetLines(nil, d, width)
	return len(lines)
}

// TargetLines reports which line each selectable row landed on.
func (d FleetData) TargetLines(width int) map[int]int {
	_, targets := fleetLines(nil, d, width)
	return targets
}

func fleetLines(t *theme.Theme, d FleetData, width int) ([]string, map[int]int) {
	style := func(get func(*theme.Theme) lipgloss.Style, s string) string {
		if t == nil {
			return s
		}
		return get(t).Render(s)
	}

	lines := []string{style(func(t *theme.Theme) lipgloss.Style { return t.Title },
		truncateTo(d.Title, width))}
	if d.Subtitle != "" {
		lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted },
			truncateTo(d.Subtitle, width)))
	}

	body, targets := sectionLines(t, d.Sections, d.Selected, width, len(lines))
	return append(lines, body...), targets
}
