package screens

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// TableColumn is a column ready to render.
type TableColumn struct {
	Title string
	// Wide marks a column the compact view hides.
	Wide bool
	// Right aligns numeric columns to the right.
	Right bool
}

// TableRow is one rendered row.
type TableRow struct {
	Cells []string
	// Status colours the row's first cell; StatusUnknown renders it plain.
	Status theme.Status
	// Age, when non-zero, is appended as a trailing column.
	Age time.Time
}

// TableData is everything the table screen renders.
type TableData struct {
	Columns []TableColumn
	Rows    []TableRow
	// Cursor is the index of the selected row, or -1 for none.
	Cursor int
	// Offset is the first visible row.
	Offset int
	// ShowWide includes the columns the compact view hides.
	ShowWide bool
	// Message replaces the rows: "Loading…", "No resources found." and an
	// error are three different things and must read as such.
	Message string
	// MessageStatus colours the message.
	MessageStatus theme.Status
}

// Visible reports how many rows fit in a body of the given height.
func (d TableData) Visible(height int) int { return max(height-1, 0) } // one row for the header

// RenderTable draws a resource table.
//
// Column widths are computed from the data that is actually on screen, so a
// single very long name in row 4000 does not squeeze every visible column. When
// space runs out, columns are dropped from the right — the leftmost columns are
// the identifying ones in every Kubernetes printer.
func RenderTable(t *theme.Theme, d TableData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}
	if len(d.Columns) == 0 {
		return t.Muted.Render(truncateTo("No columns returned by the server.", width))
	}

	visible := d.Visible(height)
	end := min(d.Offset+visible, len(d.Rows))
	start := clamp(d.Offset, max(len(d.Rows)-1, 0))
	if start > end {
		start = end
	}

	cols := selectColumns(d)
	widths := columnWidths(d, cols, width)

	aligned := make([]bool, len(cols))
	for i, c := range cols {
		aligned[i] = d.Columns[c].Right
	}

	var b strings.Builder
	b.WriteString(t.Muted.Render(renderRow(header(d, cols), widths, aligned)))

	for i := start; i < end; i++ {
		b.WriteString("\n")
		line := renderRow(project(d.Rows[i].Cells, cols), widths, aligned)
		switch {
		case i == d.Cursor:
			b.WriteString(t.SelectedRow.Render(pad(line, width)))
		case d.Rows[i].Status != theme.StatusUnknown:
			b.WriteString(t.Style(d.Rows[i].Status).Render(line))
		default:
			b.WriteString(line)
		}
	}
	for i := end - start; i < visible; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func header(d TableData, cols []int) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, strings.ToUpper(d.Columns[c].Title))
	}
	return out
}

// selectColumns picks the column indices to show.
func selectColumns(d TableData) []int {
	out := make([]int, 0, len(d.Columns))
	for i, c := range d.Columns {
		if c.Wide && !d.ShowWide {
			continue
		}
		out = append(out, i)
	}
	return out
}

// columnWidths sizes columns to their widest visible value, then trims from the
// right until the row fits.
func columnWidths(d TableData, cols []int, width int) []int {
	const gap = 2
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = lipgloss.Width(d.Columns[c].Title)
	}

	visibleEnd := min(d.Offset+200, len(d.Rows)) // sample the visible window generously
	for r := d.Offset; r < visibleEnd; r++ {
		for i, c := range cols {
			if c >= len(d.Rows[r].Cells) {
				continue
			}
			if w := lipgloss.Width(d.Rows[r].Cells[c]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	total := 0
	for i, w := range widths {
		widths[i] = min(w, maxColumnWidth)
		total += widths[i] + gap
	}

	// Drop columns from the right while the row does not fit, but never the
	// first two: they identify the object.
	for len(widths) > 2 && total > width {
		total -= widths[len(widths)-1] + gap
		widths = widths[:len(widths)-1]
	}
	// Still too wide: shrink the widest remaining column.
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

const maxColumnWidth = 48

// project selects a row's cells for the visible columns. A row shorter than the
// column definitions is normal — a custom resource may omit a printer column —
// and renders as an empty cell rather than a panic.
func project(cells []string, cols []int) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		if c < len(cells) {
			out[i] = cells[c]
		}
	}
	return out
}

func renderRow(values []string, widths []int, rightAligned []bool) string {
	var b strings.Builder
	for i := range widths {
		if i > 0 {
			b.WriteString("  ")
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		value = truncateTo(value, widths[i])
		if i < len(rightAligned) && rightAligned[i] {
			b.WriteString(padLeftTo(value, widths[i]))
			continue
		}
		b.WriteString(padRight(value, widths[i]))
	}
	return strings.TrimRight(b.String(), " ")
}

func padLeftTo(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return truncateTo(s, width)
}

// clamp keeps v inside [0, hi]: every position a screen tracks is an index
// into lines or rows, and those start at zero.
func clamp(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}
