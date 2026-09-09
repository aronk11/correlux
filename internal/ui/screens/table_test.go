package screens

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

func podTable() TableData {
	return TableData{
		Columns: []TableColumn{
			{Title: "Name"},
			{Title: "Ready"},
			{Title: "Status"},
			{Title: "Restarts", Right: true},
			{Title: "Node", Wide: true},
		},
		Rows: []TableRow{
			{Cells: []string{"payments-7d8f", "1/1", "Running", "0", "node-1"}},
			{Cells: []string{"worker-8a91", "0/1", "CrashLoopBackOff", "7", "node-2"}, Status: theme.StatusCritical},
		},
		Cursor: 0,
	}
}

func TestRenderTableShowsHeaderAndRows(t *testing.T) {
	out := RenderTable(testTheme(), podTable(), 100, 10)

	if !strings.Contains(out, "NAME") || !strings.Contains(out, "READY") {
		t.Errorf("the header must be rendered:\n%s", out)
	}
	if !strings.Contains(out, "payments-7d8f") || !strings.Contains(out, "CrashLoopBackOff") {
		t.Errorf("rows must be rendered:\n%s", out)
	}
}

func TestWideColumnsFollowTheSpaceAvailable(t *testing.T) {
	d := podTable()
	// Narrow: the compact set is all that fits, so the wide column goes.
	if strings.Contains(RenderTable(testTheme(), d, 44, 10), "node-1") {
		t.Error("a wide column must be dropped when the row does not fit")
	}
	// Wide: there is room for every column, so hiding one would waste the
	// screen the user paid for.
	if !strings.Contains(RenderTable(testTheme(), d, 100, 10), "node-1") {
		t.Error("a wide column must be drawn when it fits")
	}
}

func TestWideModeOverridesTheWidth(t *testing.T) {
	d := podTable()
	d.Wide = WideOff
	if strings.Contains(RenderTable(testTheme(), d, 100, 10), "node-1") {
		t.Error("WideOff must hide the wide columns even where they fit")
	}
	d.Wide = WideOn
	if !strings.Contains(RenderTable(testTheme(), d, 44, 10), "node-1") {
		t.Error("WideOn must show the wide columns even where they do not fit")
	}
}

func TestWideAvailableReportsWhetherTheToggleWouldChangeAnything(t *testing.T) {
	d := podTable()
	if !d.WideAvailable(testTheme(), 44) {
		t.Error("a narrow table hiding a column has something to toggle")
	}
	if d.WideAvailable(testTheme(), 100) {
		t.Error("a table already showing every column has nothing to toggle")
	}
	d.Wide = WideOff
	if !d.WideAvailable(testTheme(), 100) {
		t.Error("a column hidden on purpose can still be brought back")
	}
}

func TestSortedHeadingCarriesItsDirection(t *testing.T) {
	d := podTable()
	d.Sort = "Restarts"
	out := RenderTable(testTheme(), d, 100, 10)
	if !strings.Contains(out, "RESTARTS "+testTheme().Glyphs.SortAsc) {
		t.Errorf("the sorted heading must say which way it is sorted:\n%s", out)
	}
	d.SortDesc = true
	out = RenderTable(testTheme(), d, 100, 10)
	if !strings.Contains(out, "RESTARTS "+testTheme().Glyphs.SortDesc) {
		t.Errorf("reversing the sort must reverse the arrow:\n%s", out)
	}
	if strings.Contains(out, "NAME "+testTheme().Glyphs.SortDesc) {
		t.Error("only the sorted column carries an arrow")
	}
}

func TestColumnAtXNamesTheHeadingUnderThePointer(t *testing.T) {
	d := podTable()
	if name, ok := d.ColumnAtX(testTheme(), 100, 0); !ok || name != "Name" {
		t.Errorf("the first cell is the first column, got %q %v", name, ok)
	}
	// Past the last column there is no heading to sort by.
	if _, ok := d.ColumnAtX(testTheme(), 100, 99); ok {
		t.Error("empty space to the right of the table is not a column")
	}
}

func TestRenderTableNeverExceedsItsBounds(t *testing.T) {
	d := podTable()
	d.Wide = WideOn
	d.Rows[0].Cells[0] = strings.Repeat("very-long-object-name-", 12)

	for _, width := range []int{200, 120, 80, 60, 40, 20} {
		out := RenderTable(testTheme(), d, width, 8)
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line is %d cells wide: %q", width, w, line)
			}
		}
	}
}

func TestNarrowTerminalKeepsTheIdentifyingColumns(t *testing.T) {
	out := RenderTable(testTheme(), podTable(), 30, 8)
	if !strings.Contains(out, "NAME") {
		t.Errorf("the name column must survive a narrow terminal:\n%s", out)
	}
}

func TestRowsShorterThanTheColumnsDoNotPanic(t *testing.T) {
	// A custom resource may omit a printer column entirely.
	d := podTable()
	d.Rows = append(d.Rows, TableRow{Cells: []string{"only-a-name"}})
	out := RenderTable(testTheme(), d, 100, 10)
	if !strings.Contains(out, "only-a-name") {
		t.Error("a short row must still render")
	}
}

func TestLoadingIsNotTheSameAsEmpty(t *testing.T) {
	d := podTable()
	d.Rows = nil

	d.Message = "Loading…"
	if !strings.Contains(RenderTable(testTheme(), d, 60, 6), "Loading…") {
		t.Error("a pending load must say so")
	}

	d.Message = "No resources found."
	out := RenderTable(testTheme(), d, 60, 6)
	if !strings.Contains(out, "No resources found.") || strings.Contains(out, "Loading") {
		t.Errorf("an empty result must read differently from a pending one:\n%s", out)
	}
}

func TestScrollingShowsTheSelectedRow(t *testing.T) {
	d := podTable()
	d.Rows = nil
	for i := 0; i < 500; i++ {
		d.Rows = append(d.Rows, TableRow{Cells: []string{"pod-" + itoa(i), "1/1", "Running", "0", "node"}})
	}
	d.Cursor = 300
	d.Offset = 295

	out := RenderTable(testTheme(), d, 100, 10)
	if !strings.Contains(out, "pod-300") {
		t.Error("the selected row must be visible")
	}
	if strings.Contains(out, "pod-0 ") {
		t.Error("rows above the window must not be rendered")
	}
}

func TestRenderTableFillsItsHeight(t *testing.T) {
	const height = 12
	if got := len(strings.Split(RenderTable(testTheme(), podTable(), 100, height), "\n")); got != height-1+1 {
		t.Errorf("rendered %d lines, want %d (header plus %d row slots)", got, height, height-1)
	}
}

func TestDegenerateSizes(t *testing.T) {
	if got := RenderTable(testTheme(), podTable(), 0, 10); got != "" {
		t.Errorf("zero width must render nothing, got %q", got)
	}
	if got := RenderTable(testTheme(), podTable(), 40, 0); got != "" {
		t.Errorf("zero height must render nothing, got %q", got)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}
