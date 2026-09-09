package screens

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
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

// WideMode decides which columns a table draws.
//
// The default is WideAuto, which shows every column that fits. A resource
// table on a 200-column terminal used to draw the same four columns it draws
// on eighty and leave the rest of the screen blank, because "wide" was a
// property of the column rather than a question about the space in front of
// the user. It is the space that decides; the toggle is there for when the
// user disagrees.
type WideMode int

const (
	// WideAuto draws the wide columns when the full set fits.
	WideAuto WideMode = iota
	// WideOn draws them whatever the width, cutting the row to fit.
	WideOn
	// WideOff draws only the compact set even when there is room for more.
	WideOff
)

// TableData is everything the table screen renders.
type TableData struct {
	Columns []TableColumn
	Rows    []TableRow
	// Cursor is the index of the selected row, or -1 for none.
	Cursor int
	// Offset is the first visible row.
	Offset int
	// Wide decides whether the columns marked Wide are drawn.
	Wide WideMode
	// Sort names the column the rows were ordered by, empty for the order the
	// server gave them. The heading carries an arrow so the order on screen is
	// never a thing you have to remember having asked for.
	Sort     string
	SortDesc bool
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

	cols := selectColumns(t, d, width)
	widths := columnWidths(t, d, cols, width)
	cols = cols[:len(widths)]

	aligned := make([]bool, len(cols))
	for i, c := range cols {
		aligned[i] = d.Columns[c].Right
	}

	var b strings.Builder
	b.WriteString(t.Muted.Render(renderRow(header(t, d, cols), widths, aligned)))

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

func header(t *theme.Theme, d TableData, cols []int) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, heading(t, d, c))
	}
	return out
}

// heading is one column title as it is drawn: the name, and an arrow when the
// table is ordered by it.
//
// The arrow is part of the heading text rather than something painted over it,
// so the column is sized to hold both and a sorted heading is never the one
// that gets cut.
func heading(t *theme.Theme, d TableData, col int) string {
	title := strings.ToUpper(d.Columns[col].Title)
	if d.Sort == "" || !strings.EqualFold(d.Sort, d.Columns[col].Title) {
		return title
	}
	mark := t.Glyphs.SortAsc
	if d.SortDesc {
		mark = t.Glyphs.SortDesc
	}
	return title + " " + mark
}

// selectColumns picks the column indices to show.
//
// WideAuto is the interesting case: the wide columns are drawn when the full
// set fits the terminal, and dropped when it does not. "Wide" describes a
// column the compact view can live without, not one the user never wants to
// see — on a screen with room for it, hiding it behind a keystroke costs the
// user the information and gains them nothing but blank space to the right.
func selectColumns(t *theme.Theme, d TableData, width int) []int {
	all := make([]int, 0, len(d.Columns))
	compact := make([]int, 0, len(d.Columns))
	for i, c := range d.Columns {
		all = append(all, i)
		if !c.Wide {
			compact = append(compact, i)
		}
	}
	// Every column is wide: there is no compact set to fall back to, and a
	// table with no columns at all is not an answer to anything.
	if len(compact) == 0 {
		return all
	}

	switch d.Wide {
	case WideOn:
		return all
	case WideOff:
		return compact
	}
	if fits(naturalWidths(t, d, all), width) {
		return all
	}
	return compact
}

// WideAvailable reports whether showing and hiding the wide columns would draw
// two different tables at this width.
//
// It is what lets the status bar offer the toggle only where it does
// something: on a wide terminal that already shows every column, a key
// advertised as "Wide" that changes nothing on screen is a key the user
// presses twice and then stops trusting (SPEC 17).
func (d TableData) WideAvailable(t *theme.Theme, width int) bool {
	if len(d.Columns) == 0 || !d.hasWide() {
		return false
	}
	all := make([]int, 0, len(d.Columns))
	compact := make([]int, 0, len(d.Columns))
	for i, c := range d.Columns {
		all = append(all, i)
		if !c.Wide {
			compact = append(compact, i)
		}
	}
	if len(compact) == 0 || len(compact) == len(all) {
		return false
	}
	// Both sets already render alike: the wide columns fit and are on screen.
	return d.Wide != WideAuto || !fits(naturalWidths(t, d, all), width)
}

// naturalWidths is what a set of columns would like to be, before anything is
// dropped or shrunk to fit.
func naturalWidths(t *theme.Theme, d TableData, cols []int) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = lipgloss.Width(heading(t, d, c))
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
	for i := range widths {
		widths[i] = min(widths[i], maxColumnWidth)
	}
	return widths
}

// fits reports whether these columns, at their natural widths, draw inside the
// terminal without anything being dropped or squeezed.
func fits(widths []int, width int) bool {
	const gap = 2
	total := 0
	for _, w := range widths {
		total += w + gap
	}
	return total <= width
}

// columnWidths sizes columns to their widest visible value, then trims from the
// right until the row fits.
func columnWidths(t *theme.Theme, d TableData, cols []int, width int) []int {
	const gap = 2
	widths := naturalWidths(t, d, cols)

	total := 0
	for _, w := range widths {
		total += w + gap
	}

	// Asked for the wide columns explicitly, squeeze before discarding: the
	// user pressed a key to see a column, and dropping it from the right — the
	// end the wide columns live at — answers that key by showing them exactly
	// nothing. Narrowed to a stub, the column is at least on screen and can be
	// read by widening the terminal; dropped, it is a keystroke that did
	// nothing. The automatic mode keeps the opposite preference, because there
	// nobody asked for the column and roomy cells are worth more than a
	// truncated extra one.
	if d.Wide == WideOn {
		total = shrinkToFit(widths, total, width)
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

// VisibleColumns names the columns this table currently draws, in order.
//
// The sort picker and the filter both offer what is on screen rather than
// what the data happens to carry: a column somebody cannot read is not one
// they can reasonably be asked to order by.
func (d TableData) VisibleColumns(t *theme.Theme, width int) []string {
	cols := selectColumns(t, d, width)
	widths := columnWidths(t, d, cols, width)
	cols = cols[:len(widths)]
	// columnWidths drops columns that did not fit; only the ones it kept are
	// actually drawn.
	out := make([]string, 0, len(widths))
	for i := range widths {
		out = append(out, d.Columns[cols[i]].Title)
	}
	return out
}

// ColumnAtX names the column drawn at a screen column, which is what turns a
// click on a heading into a sort. It reports false past the last column, so
// clicking the empty space to the right of a narrow table does nothing rather
// than sorting by whatever happens to be last.
func (d TableData) ColumnAtX(t *theme.Theme, width, x int) (string, bool) {
	if x < 0 || len(d.Columns) == 0 {
		return "", false
	}
	const gap = 2
	cols := selectColumns(t, d, width)
	widths := columnWidths(t, d, cols, width)
	cols = cols[:len(widths)]

	at := 0
	for i, w := range widths {
		// The gap after a column belongs to it: a click one cell right of a
		// heading is a click on that heading, not a miss.
		if x < at+w+gap {
			return d.Columns[cols[i]].Title, true
		}
		at += w + gap
	}
	return "", false
}

// shrinkToFit narrows the widest columns until the row fits, or until nothing
// can give any more. It returns the new total.
//
// The widest column goes first each time round, so the space is taken from
// whichever cell can best afford it rather than from whichever happens to sit
// on the right.
func shrinkToFit(widths []int, total, width int) int {
	const floor = 6
	for total > width {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if len(widths) == 0 || widths[widest] <= floor {
			return total
		}
		shrink := min(total-width, widths[widest]-floor)
		widths[widest] -= shrink
		total -= shrink
	}
	return total
}

// ShowingWide reports whether a column marked Wide is actually drawn.
//
// It asks what is on screen rather than what was requested, because those are
// not always the same: a terminal too narrow to hold them drops the wide
// columns whatever mode the table is in, and a status bar that answered from
// the mode would offer to "hide" columns the user cannot see.
func (d TableData) ShowingWide(t *theme.Theme, width int) bool {
	if !d.hasWide() {
		return false
	}
	cols := selectColumns(t, d, width)
	widths := columnWidths(t, d, cols, width)
	for i := range widths {
		if d.Columns[cols[i]].Wide {
			return true
		}
	}
	return false
}

// hasWide reports whether any column is marked wide, which is the cheap
// question to ask before measuring anything: most kinds have none, and for
// those the whole wide/compact distinction is moot.
func (d TableData) hasWide() bool {
	for _, c := range d.Columns {
		if c.Wide {
			return true
		}
	}
	return false
}
