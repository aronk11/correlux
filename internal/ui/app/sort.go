package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// tableSort is the order a table is in: a column heading and a direction, or
// nothing at all for the order the data arrived in.
//
// It is held by column *name* rather than by index, because the columns on
// screen change underneath it — the wide set appears when the terminal grows,
// a custom resource contributes a column its neighbour does not — and an index
// that survived one of those would be pointing at a different column than the
// one the user chose.
type tableSort struct {
	column string
	desc   bool
}

func (s tableSort) active() bool { return s.column != "" }

// sortFor is the order the current screen is in. Each table keeps its own:
// ordering the dashboard by restarts says nothing about how you want a list of
// nodes, and carrying one across would be a change the user did not ask for.
func (m *Model) sortFor(view viewKind) tableSort {
	switch view {
	case viewApplications:
		return m.appSort
	case viewTable:
		return m.tableSort
	case viewFleetResource:
		return m.fleetSort
	}
	return tableSort{}
}

func (m *Model) setSortFor(view viewKind, s tableSort) {
	switch view {
	case viewApplications:
		m.appSort = s
	case viewTable:
		m.tableSort = s
	case viewFleetResource:
		m.fleetSort = s
	}
}

// currentSort is the order of the table in front of the user.
func (m *Model) currentSort() tableSort { return m.sortFor(m.view) }

// sortable reports whether this screen is a table that can be reordered.
func (m *Model) sortable() bool {
	switch m.view {
	case viewApplications, viewTable, viewFleetResource:
		return len(m.sortColumns()) > 0
	}
	return false
}

// sortColumns are the headings the user may order by: the ones the table is
// actually drawing at this width.
func (m *Model) sortColumns() []string {
	body := m.screen.Body
	if body.Empty() {
		return nil
	}
	d, ok := m.tableForFrame()
	if !ok {
		return nil
	}
	return d.VisibleColumns(m.theme, body.Width)
}

// sortBy orders the current table by a column.
//
// Choosing the column it is already ordered by reverses it, which is the one
// gesture every table in every tool shares. A column is entered ascending
// except where the interesting end is the top — restarts, age and anything
// counted — because the reason to order a table by restarts during an incident
// is never to find the pod that has not restarted.
func (m *Model) sortBy(column string) tea.Cmd {
	column = strings.TrimSpace(column)
	if column == "" {
		return m.clearSort()
	}
	current := m.currentSort()
	next := tableSort{column: column, desc: descendingFirst(column)}
	if strings.EqualFold(current.column, column) {
		next.desc = !current.desc
	}
	m.setSortFor(m.view, next)
	m.resetSortedCursor()

	direction := "ascending"
	if next.desc {
		direction = "descending"
	}
	notice := "Sorted by " + column + ", " + direction
	if m.tablePagedOpen() {
		// Said at the moment the order is chosen, as well as in the header
		// while it holds: this is the one keystroke where somebody decides
		// what the worst row is, and on a paged table the answer covers only
		// what has been read.
		notice += " — among the rows loaded so far"
	}
	m.notice(notice, theme.StatusHealthy)
	return m.expireNotice()
}

// clearSort puts a table back into the order it arrived in — worst-first for
// the dashboard, the server's own order for a resource table.
func (m *Model) clearSort() tea.Cmd {
	if !m.currentSort().active() {
		return nil
	}
	m.setSortFor(m.view, tableSort{})
	m.resetSortedCursor()
	m.notice(m.defaultOrderLabel(), theme.StatusHealthy)
	return m.expireNotice()
}

// defaultOrderLabel names the order a table falls back to, because "sorting
// off" is not a state a user can picture and "worst first" is.
func (m *Model) defaultOrderLabel() string {
	if m.view == viewApplications {
		return "Back to the default order: worst first"
	}
	return "Back to the order the server returned"
}

// resetSortedCursor puts the cursor back on the first row. Reordering moves
// every row out from under it, and a cursor left at index 12 would land on
// something the user never selected — which matters, because the next key they
// press may act on it.
func (m *Model) resetSortedCursor() {
	switch m.view {
	case viewApplications:
		m.appPort.Cursor, m.appPort.Offset = 0, 0
	case viewTable:
		m.tablePort.Cursor, m.tablePort.Offset = 0, 0
	case viewFleetResource:
		m.fleetTablePort.Cursor, m.fleetTablePort.Offset = 0, 0
	}
}

// descendingFirst reports the columns worth opening at the top rather than the
// bottom. The list is about what the words mean, not about what any one kind
// prints: a column called "restarts" counts failures wherever it appears.
func descendingFirst(column string) bool {
	switch strings.ToLower(strings.TrimSpace(column)) {
	case "restarts", "age", "count", "errors", "failures":
		return true
	}
	return false
}

// sortLabel says what order a table is in, for the header and the palette.
func (s tableSort) label(t *theme.Theme) string {
	if !s.active() {
		return ""
	}
	mark := t.Glyphs.SortAsc
	if s.desc {
		mark = t.Glyphs.SortDesc
	}
	return s.column + " " + mark
}

// wideMode is how the tables should draw their wide columns: what the user
// asked for, or the width-driven default when they have not asked.
func (m *Model) wideMode() screens.WideMode { return m.tableWide }

// sortDefaultID is the row that puts a table back into the order it arrived
// in. It leads the list rather than closing it: "undo what I asked for" is the
// row somebody reaches for in a hurry, and the bottom of a filtered list is
// not where a hurried eye lands.
const sortDefaultID = "__default"

// sortSubject names what is about to be reordered, for the picker's title.
func (m *Model) sortSubject() string {
	switch m.view {
	case viewApplications:
		return "applications in " + m.scopeLabel()
	case viewTable:
		return m.resource.Plural()
	case viewFleetResource:
		return m.fleetResource.Plural() + " across the fleet"
	}
	return "this table"
}

// sortPickerFooter says what the keys do and what order the table is in now.
func (m *Model) sortPickerFooter() string {
	current := m.defaultOrderLabel()
	if s := m.currentSort(); s.active() {
		current = "now: " + s.label(m.theme)
	}
	return "Enter sort   Esc cancel   " + m.theme.Glyphs.Bullet + " " + current
}

// filterSortColumns lists the columns the table draws, plus the way back to
// its own order.
func (m *Model) filterSortColumns(q string) []components.Item {
	columns := m.sortColumns()
	current := m.currentSort()

	rows := make([]components.Item, 0, len(columns)+1)
	rows = append(rows, components.Item{
		ID:       sortDefaultID,
		Title:    defaultOrderTitle(m.view),
		Subtitle: defaultOrderSubtitle(m.view),
	})
	for _, name := range columns {
		item := components.Item{ID: name, Title: name}
		if strings.EqualFold(current.column, name) {
			item.Right = "sorting " + directionWord(current.desc)
			item.Subtitle = "Enter reverses it"
		} else if descendingFirst(name) {
			item.Subtitle = "highest first"
		}
		rows = append(rows, item)
	}

	q = strings.TrimSpace(q)
	if q == "" {
		return rows
	}
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Title
	}
	out := make([]components.Item, 0, len(rows))
	for _, match := range fuzzy.Find(q, names) {
		item := rows[match.Index]
		item.Highlight = match.MatchedIndexes
		out = append(out, item)
	}
	return out
}

func defaultOrderTitle(view viewKind) string {
	if view == viewApplications {
		return "Worst first (default)"
	}
	return "The order the server returned (default)"
}

func defaultOrderSubtitle(view viewKind) string {
	if view == viewApplications {
		return "down, then degraded, then healthy"
	}
	return "no client-side ordering"
}

func directionWord(desc bool) string {
	if desc {
		return "descending"
	}
	return "ascending"
}

// sortHint is what the sort key advertises in the status bar: the column the
// table is in, or the offer to choose one.
func (m *Model) sortHint() string {
	if s := m.currentSort(); s.active() {
		return "Sort: " + s.label(m.theme)
	}
	return "Sort"
}

// sortPaletteSubtitle says what the palette entry would change, which is a
// different sentence depending on whether the table is already ordered.
func (m *Model) sortPaletteSubtitle() string {
	if s := m.currentSort(); s.active() {
		return "currently " + s.column + ", " + directionWord(s.desc)
	}
	return strings.Join(m.sortColumns(), ", ")
}

// clickBody turns a click inside the table area into the thing it looks like
// it should do: a heading sorts by that column, a row moves the cursor onto it.
//
// The mouse already scrolls these tables and already works in the navigation
// bar, so a click that landed on a row and did nothing was the one place the
// pointer stopped behaving like a pointer.
func (m *Model) clickBody(x, y int) tea.Cmd {
	body := m.screen.Body
	if body.Empty() {
		return nil
	}
	row := y - body.Y

	switch m.view {
	case viewApplications, viewTable, viewFleetResource:
		d, ok := m.tableForFrame()
		if !ok {
			return nil
		}
		switch m.view {
		case viewApplications:
			return m.clickTableRow(d, row, x, len(m.visibleApplications()),
				&m.appPort, m.applicationsVisible(), m.openSelectedApplication)
		case viewTable:
			return m.clickTableRow(d, row, x, len(m.visibleRows()),
				&m.tablePort, m.rowsPerScreen(), m.openSelectedRow)
		case viewFleetResource:
			return m.clickTableRow(d, row, x, len(m.visibleFleetRows()),
				&m.fleetTablePort, m.rowsPerScreen(), m.openFleetRow)
		}
	case viewApplication:
		data, targets := m.applicationView()
		return m.clickTargetRow(data.TargetLines(m.screen.Body.Width), len(targets), &m.detailPort, row, m.openSelectedObject)
	case viewObject:
		if m.objectYAML {
			// The document has nothing to select in this mode; a click has
			// nothing to do here either.
			return nil
		}
		data, targets := m.objectView()
		return m.clickTargetRow(data.TargetLines(m.screen.Body.Width), len(targets), &m.objectPort, row, m.openSelectedRelation)
	case viewFleet:
		data := m.fleetData()
		return m.clickTargetRow(data.TargetLines(m.screen.Body.Width), len(m.fleetTargets()), &m.fleetPort, row, m.enterFleetRow)
	case viewUsage:
		data, targets := m.usageView()
		return m.clickTargetRow(data.TargetLines(m.screen.Body.Width), len(targets), &m.usagePort, row, m.openSelectedUsageTarget)
	case viewActivity:
		data, targets := m.activityView()
		return m.clickTargetRow(data.TargetLines(m.screen.Body.Width), len(targets), &m.activityPort, row, m.openSelectedActivityObject)
	}
	return nil
}

// clickTargetRow resolves a click against a target-line screen — one whose
// rows have no fixed height or heading, like an application's detail view or
// an object's relations. It follows clickTableRow's own convention exactly:
// a click selects the row under the pointer, and a click on the row already
// selected opens it, so the pointer means the same thing on every scrollable
// screen in Correlux rather than a different one depending on which of them
// happens to be a table.
func (m *Model) clickTargetRow(lines map[int]int, count int, port *layout.Viewport, row int, open func() tea.Cmd) tea.Cmd {
	if count == 0 {
		return nil
	}
	target, ok := lineTarget(lines, port.Offset+row)
	if !ok {
		return nil
	}
	if target == port.Cursor {
		return open()
	}
	port.Cursor = target
	return nil
}

// lineTarget reverse-looks-up a TargetLines map: which navigable row, if any,
// rendered to a given line.
func lineTarget(lines map[int]int, line int) (int, bool) {
	for target, l := range lines {
		if l == line {
			return target, true
		}
	}
	return -1, false
}

// clickTableRow resolves one click against a rendered table.
//
// Row 0 is the heading. Below it the click selects, and a click on the row
// already under the cursor opens it — the second click of a double-click, which
// is what a pointer user expects and what keeps a single click from navigating
// somewhere they only meant to look at.
func (m *Model) clickTableRow(
	d screens.TableData,
	row, x, count int,
	port *layout.Viewport,
	perScreen int,
	open func() tea.Cmd,
) tea.Cmd {
	if d.Message != "" || len(d.Columns) == 0 {
		return nil
	}
	if row == 0 {
		column, ok := d.ColumnAtX(m.theme, m.screen.Body.Width, x)
		if !ok {
			return nil
		}
		return m.sortBy(column)
	}
	index := port.Offset + row - 1
	if row < 1 || index < 0 || index >= count {
		return nil
	}
	if index == port.Cursor {
		return open()
	}
	port.MoveCursor(index-port.Cursor, count, perScreen)
	return nil
}

// tableForFrame is the table the current screen draws, built at most once per
// frame.
//
// Three things ask for it while one frame is rendered — the body, the status
// bar's wide-column label, and the palette — and building it means filtering
// and sorting every row the cluster returned. Doing that three times over five
// thousand pods is work the user pays for in latency and gets nothing for,
// since nothing between the calls can have changed: View is a pure function of
// the model (SPEC 21).
//
// The cache lives for exactly one View. Update is where state changes, and a
// value cached across that boundary would be a frame describing a model that
// no longer exists.
func (m *Model) tableForFrame() (screens.TableData, bool) {
	switch m.view {
	case viewApplications, viewTable, viewFleetResource:
	default:
		return screens.TableData{}, false
	}
	if m.frameTable != nil {
		return *m.frameTable, true
	}

	var d screens.TableData
	switch m.view {
	case viewApplications:
		d = m.applicationsData()
	case viewTable:
		d = m.tableData()
	case viewFleetResource:
		d = m.fleetResourceData()
	}
	if m.frameCaching {
		cached := d
		m.frameTable = &cached
	}
	return d, true
}

// beginFrame opens the per-frame cache, and returns the function that closes
// it. Outside a frame every caller builds its own table, which is what keeps a
// stale one from outliving the state it was built from.
func (m *Model) beginFrame() func() {
	m.frameCaching, m.frameTable = true, nil
	return func() { m.frameCaching, m.frameTable = false, nil }
}

// frameTableFor is tableForFrame for callers that already know the screen is a
// table, which is every renderer that draws one.
func (m *Model) frameTableFor() screens.TableData {
	d, _ := m.tableForFrame()
	return d
}

// tablePagedOpen reports whether the table on screen has rows the cluster has
// not been asked for yet.
func (m *Model) tablePagedOpen() bool {
	switch m.view {
	case viewTable:
		table := m.table.Get()
		return table != nil && table.HasMore()
	case viewFleetResource:
		return m.fleetPending > 0 || m.fleetTable.Truncated
	}
	return false
}
