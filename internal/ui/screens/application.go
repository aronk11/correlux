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
	// Target indexes the caller's list of navigable objects, or -1 when the row
	// is something to read rather than somewhere to go.
	Target int
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
	// Selected is the target index under the cursor, or -1 for none.
	Selected int
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

	lines, _ := applicationLines(t, d, width)
	offset := clamp(d.Offset, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// LineCount reports how many lines the detail view renders, so the model can
// bound scrolling without rendering twice.
func (d ApplicationData) LineCount(width int) int {
	lines, _ := applicationLines(nil, d, width)
	return len(lines)
}

// TargetLines reports which line each navigable row lands on, so the model can
// keep the selection and the viewport agreeing with each other.
func (d ApplicationData) TargetLines(width int) map[int]int {
	_, lines := applicationLines(nil, d, width)
	return lines
}

// applicationLines renders the view to individual lines, and reports which line
// each navigable row landed on. A nil theme measures without styling, which is
// what the two functions above need.
func applicationLines(t *theme.Theme, d ApplicationData, width int) ([]string, map[int]int) {
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

	body, targets := sectionLines(t, d.Sections, d.Selected, width, len(lines))
	lines = append(lines, body...)
	return lines, targets
}

// sectionLines renders titled sections of rows, reporting which line each
// navigable row landed on. Both the application view and the object view are
// made of these, and they must behave identically: the same keys move through
// them and the same highlight marks the row in hand.
func sectionLines(
	t *theme.Theme,
	sections []DetailSection,
	selected, width, offset int,
) ([]string, map[int]int) {
	style := func(get func(*theme.Theme) lipgloss.Style, s string) string {
		if t == nil {
			return s
		}
		return get(t).Render(s)
	}
	muted := func(s string) string {
		return style(func(t *theme.Theme) lipgloss.Style { return t.Muted }, s)
	}

	targets := map[int]int{}
	lines := make([]string, 0, len(sections)*4)

	for _, section := range sections {
		lines = append(lines, "", style(func(t *theme.Theme) lipgloss.Style { return t.PanelTitle },
			truncateTo(strings.ToUpper(section.Title), width)))

		if len(section.Rows) == 0 {
			if section.Empty != "" {
				lines = append(lines, muted("  "+truncateTo(section.Empty, max(width-2, 1))))
			}
			continue
		}

		widths := detailWidths(section, max(width-2, 1))
		if len(section.Columns) > 0 {
			titles := make([]string, len(section.Columns))
			for i, c := range section.Columns {
				titles[i] = strings.ToUpper(c)
			}
			lines = append(lines, muted("  "+renderRow(titles, widths, nil)))
		}
		for _, row := range section.Rows {
			text := "  " + renderRow(row.Cells, widths, nil)
			line := text
			switch {
			case t == nil:
			case row.Target >= 0 && row.Target == selected:
				line = t.SelectedRow.Render(padTo(text, width))
			case row.Status != theme.StatusUnknown:
				line = t.Style(row.Status).Render(text)
			}
			if row.Target >= 0 {
				targets[row.Target] = offset + len(lines)
			}
			lines = append(lines, line)
		}
	}
	return lines, targets
}

// padTo pads a line to the full width so a selected row is highlighted across
// the screen rather than only under its text.
func padTo(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return truncateTo(s, width)
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
