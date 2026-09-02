package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// UsageData is the resource-usage view: where the pods are, and what they use.
//
// It is built from the same DetailSections the application, object and fleet
// views are made of, so the keys that scroll one scroll this too and a reader
// who has learned one screen has learned this one. Nothing in it is
// selectable: every row is something to read.
type UsageData struct {
	Title    string
	Subtitle string
	// Notes qualify the numbers themselves — a scope that hides other
	// namespaces' pods, nodes that could not be listed, metrics that are not
	// installed. They sit above the sections because they change what every
	// number below them means.
	Notes    []string
	Sections []DetailSection
	Offset   int

	// Message replaces the body while loading or after a failure.
	Message       string
	MessageStatus theme.Status
}

// RenderUsage draws the view.
func RenderUsage(t *theme.Theme, d UsageData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}

	lines := usageLines(t, d, width)
	offset := clamp(d.Offset, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// LineCount reports how tall the view is, so the model can bound scrolling
// without rendering twice.
func (d UsageData) LineCount(width int) int { return len(usageLines(nil, d, width)) }

func usageLines(t *theme.Theme, d UsageData, width int) []string {
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
	for _, note := range d.Notes {
		lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted },
			truncateTo(note, width)))
	}

	body, _ := sectionLines(t, d.Sections, -1, width, len(lines))
	return append(lines, body...)
}
