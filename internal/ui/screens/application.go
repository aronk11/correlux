package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// DetailRow is one object inside an application.
type DetailRow struct {
	Cells []string
	// Status colours the row; StatusUnknown renders it plain.
	Status theme.Status
}

// DetailSection groups rows of one kind under a heading.
type DetailSection struct {
	Title   string
	Columns []string
	Rows    []DetailRow
	// Empty is shown instead of the rows when there are none. It says which
	// nothing this is: "no ingresses" and "ingresses not readable" are
	// different facts and must not render alike.
	Empty string
}

// ApplicationData is one application's detail view.
type ApplicationData struct {
	Name         string
	Namespace    string
	Health       string
	HealthGlyph  string
	HealthStatus theme.Status
	Summary      string
	// Notes are qualifications about the data itself: a kind that could not be
	// read, a scope that was truncated.
	Notes    []string
	Sections []DetailSection
	// Offset is the first visible line, so a long application scrolls.
	Offset int
	// Message replaces the whole body while loading or after a failure.
	Message       string
	MessageStatus theme.Status
}

// RenderApplication draws an application: what it is, how it is doing, and what
// it is made of.
//
// The layout is deliberately a list of small tables rather than a graph. A
// topology drawing looks impressive in a screenshot and is unreadable in an
// eighty-column terminal at three in the morning (SPEC 12).
func RenderApplication(t *theme.Theme, d ApplicationData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}

	lines := applicationLines(t, d, width)
	offset := clamp(d.Offset, 0, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// LineCount reports how many lines the detail view renders, so the model can
// bound scrolling without rendering twice.
func (d ApplicationData) LineCount(width int) int {
	return len(applicationLines(nil, d, width))
}

// applicationLines renders the view to individual lines. A nil theme measures
// without styling, which is what LineCount needs.
func applicationLines(t *theme.Theme, d ApplicationData, width int) []string {
	style := func(get func(*theme.Theme) lipgloss.Style, s string) string {
		if t == nil {
			return s
		}
		return get(t).Render(s)
	}

	title := d.Name
	if d.Namespace != "" {
		title += "  " + d.Namespace
	}
	head := style(func(t *theme.Theme) lipgloss.Style { return t.Title }, truncateTo(title, width))

	state := strings.TrimSpace(d.HealthGlyph + " " + d.Health)
	if d.Summary != "" {
		state += "   " + d.Summary
	}
	if t != nil {
		state = t.Style(d.HealthStatus).Render(truncateTo(state, width))
	}

	lines := []string{head, state}
	for _, note := range d.Notes {
		lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted }, truncateTo(note, width)))
	}

	for _, section := range d.Sections {
		lines = append(lines, "", style(func(t *theme.Theme) lipgloss.Style { return t.PanelTitle },
			truncateTo(strings.ToUpper(section.Title), width)))

		if len(section.Rows) == 0 {
			if section.Empty != "" {
				lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted },
					"  "+truncateTo(section.Empty, max(width-2, 1))))
			}
			continue
		}

		widths := detailWidths(section, max(width-2, 1))
		if len(section.Columns) > 0 {
			titles := make([]string, len(section.Columns))
			for i, c := range section.Columns {
				titles[i] = strings.ToUpper(c)
			}
			header := renderRow(titles, widths, nil)
			lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted }, "  "+header))
		}
		for _, row := range section.Rows {
			line := "  " + renderRow(row.Cells, widths, nil)
			if t != nil && row.Status != theme.StatusUnknown {
				line = t.Style(row.Status).Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// detailWidths sizes a section's columns to its own content: the sections are
// independent little tables, and forcing one alignment across all of them would
// leave a column of empty space next to every short list.
func detailWidths(section DetailSection, width int) []int {
	count := len(section.Columns)
	for _, r := range section.Rows {
		if len(r.Cells) > count {
			count = len(r.Cells)
		}
	}
	widths := make([]int, count)
	for i, c := range section.Columns {
		widths[i] = lipgloss.Width(strings.ToUpper(c))
	}
	for _, r := range section.Rows {
		for i, cell := range r.Cells {
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	const gap = 2
	total := 0
	for i := range widths {
		widths[i] = min(widths[i], maxColumnWidth)
		total += widths[i] + gap
	}
	for total > width && len(widths) > 0 {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 6 {
			break
		}
		shrink := min(total-width, widths[widest]-6)
		widths[widest] -= shrink
		total -= shrink
	}
	return widths
}
