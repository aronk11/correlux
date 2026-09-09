package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sahilm/fuzzy"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/query"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// Searching filters what is already on screen.
//
// It is a filter, not a query: Correlux narrows the rows it has rather than
// asking the API server a different question. That distinction is not a
// shortcut, it is the honest one — a server-side selector cannot match on the
// cells a printer produced, and a table that silently changed what it was
// listing would be worse than one that says how much it is showing.
//
// A word that names a column is a comparison and everything else is still
// fuzzy text, so `restarts>5 payments` reads as it looks. The comparisons work
// against the columns a view already draws, which means the same syntax serves
// the dashboard, any kind the API server prints and the fleet's merged table,
// including a custom resource nobody wrote code for (internal/domain/query).

// startSearch opens the filter on the current view.
func (m *Model) startSearch() tea.Cmd {
	if !m.searchable() {
		m.notice("Nothing to search on this screen", theme.StatusWarning)
		return m.expireNotice()
	}
	m.searching = true
	m.rebuildCommands()
	return nil
}

// endSearch closes the filter, keeping what was typed.
func (m *Model) endSearch() {
	m.searching = false
	m.rebuildCommands()
}

// clearSearch drops the filter entirely.
func (m *Model) clearSearch() {
	m.searching = false
	m.search.Reset()
	m.resetCursors()
	m.rebuildCommands()
}

// searchable reports whether the current view has rows to filter.
func (m *Model) searchable() bool {
	switch m.view {
	case viewTable, viewApplications, viewFleetResource, viewFleet:
		return true
	default:
		return false
	}
}

// query is what the user typed, trimmed.
func (m *Model) query() string { return strings.TrimSpace(m.search.Value()) }

// parsedQuery is what they typed, understood.
func (m *Model) parsedQuery() query.Query { return query.Parse(m.query()) }

// searchColumns are the column names the filter can compare against on the
// current screen. They are the headings the user is looking at, which is the
// only set they could reasonably guess from.
func (m *Model) searchColumns() []string {
	switch m.view {
	case viewApplications:
		return applicationColumns
	case viewTable:
		if table := m.table.Get(); table != nil {
			return columnNames(table.Columns)
		}
	case viewFleetResource:
		return mergedColumnNames(m.fleetTable.Columns)
	}
	return nil
}

func columnNames(columns []resources.Column) []string {
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		out = append(out, c.Name)
	}
	return out
}

func mergedColumnNames(columns []resources.Column) []string {
	return columnNames(columns)
}

// filtering reports whether a filter is in force.
func (m *Model) filtering() bool { return m.query() != "" }

// handleSearchKey routes a keystroke to the filter while it has focus, and
// reports whether the filter consumed it. Nothing here fetches anything: a
// filter narrows what is already there.
func (m *Model) handleSearchKey(keystroke, text string) bool {
	switch keystroke {
	case "esc":
		m.clearSearch()
		return true
	case "enter", "down", "up", "pgup", "pgdown":
		// Leave the input and act on the rows; the filter stays in force.
		m.endSearch()
		return keystroke == "enter"
	}
	changed, handled := m.search.HandleKey(keystroke, text)
	if changed {
		m.resetCursors()
	}
	return handled
}

// resetCursors puts every filtered list back at the top: the row that was under
// the cursor is usually not in the result.
func (m *Model) resetCursors() {
	m.tablePort.Cursor, m.tablePort.Offset = 0, 0
	m.appPort.Cursor, m.appPort.Offset = 0, 0
	m.fleetTablePort.Cursor, m.fleetTablePort.Offset = 0, 0
	m.fleetPort.Cursor, m.fleetPort.Offset = 0, 0
}

// matches returns the indices of the rows a filter keeps, in their original
// order.
//
// Comparisons run first and cheaply, and the fuzzy pass runs over what
// survives: typing "payei" finds "payments-7d8f  ImagePullBackOff", because
// during an incident people type what they remember, not what they can see.
func matches(columns []string, rows [][]string, filter string) []int {
	q := query.Parse(filter)
	if q.Empty() {
		return nil
	}

	kept := make([]int, 0, len(rows))
	for i, cells := range rows {
		if q.Match(columns, cells) {
			kept = append(kept, i)
		}
	}
	if q.Text == "" {
		return kept
	}

	haystack := make([]string, len(kept))
	for i, index := range kept {
		haystack[i] = strings.ToLower(strings.Join(rows[index], " "))
	}

	found := fuzzy.Find(strings.ToLower(q.Text), haystack)
	out := make([]int, 0, len(found))
	for _, match := range found {
		out = append(out, kept[match.Index])
	}
	// fuzzy.Find ranks by score; a table must stay in the order the server
	// sorted it, or a filtered list becomes a different list.
	sortInts(out)
	return out
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// visibleRows are the resource table's rows after the filter and the sort.
func (m *Model) visibleRows() []resources.Row {
	rows := m.tableRows()
	if m.filtering() {
		cells := make([][]string, len(rows))
		for i := range rows {
			cells[i] = rows[i].Cells
		}
		out := make([]resources.Row, 0, len(rows))
		for _, i := range matches(m.searchColumns(), cells, m.query()) {
			out = append(out, rows[i])
		}
		rows = out
	}
	return ordered(rows, m.searchColumns(), func(r *resources.Row) []string { return r.Cells }, m.tableSort)
}

// ordered reorders rows that have already been filtered.
//
// Sorting runs after filtering rather than before it, so the two compose the
// way they read: narrow to what matters, then put the worst of it first. The
// cells it sorts on are the ones the filter compares, so a column that answers
// `restarts>5` and a column ordered by restarts agree about what a restart is.
func ordered[T any](rows []T, columns []string, cells func(*T) []string, s tableSort) []T {
	if !s.active() || len(rows) == 0 {
		return rows
	}
	grid := make([][]string, len(rows))
	for i := range rows {
		grid[i] = cells(&rows[i])
	}
	out := make([]T, 0, len(rows))
	for _, i := range query.Order(columns, grid, s.column, s.desc) {
		out = append(out, rows[i])
	}
	return out
}

// visibleApplications are the dashboard's applications after the filter. The
// haystack is what the row shows, so what you can read is what you can search.
func (m *Model) visibleApplications() []application.Application {
	apps := m.applications()
	if m.filtering() {
		cells := make([][]string, len(apps))
		for i := range apps {
			cells[i] = applicationCells(&apps[i])
		}
		out := make([]application.Application, 0, len(apps))
		for _, i := range matches(applicationColumns, cells, m.query()) {
			out = append(out, apps[i])
		}
		apps = out
	}
	return ordered(apps, applicationColumns, applicationCells, m.appSort)
}

// visibleFleetRows are the cross-cluster table's rows after the filter.
func (m *Model) visibleFleetRows() []resources.MergedRow {
	rows := m.fleetTable.Rows
	if m.filtering() {
		cells := make([][]string, len(rows))
		for i := range rows {
			cells[i] = rows[i].Cells
		}
		out := make([]resources.MergedRow, 0, len(rows))
		for _, i := range matches(m.searchColumns(), cells, m.query()) {
			out = append(out, rows[i])
		}
		rows = out
	}
	return ordered(rows, m.searchColumns(), func(r *resources.MergedRow) []string { return r.Cells }, m.fleetSort)
}

// applicationColumns name what the dashboard shows, in the order
// applicationCells writes them. They are the headings on screen, so what
// somebody can read is what they can filter on.
var applicationColumns = []string{
	"Status", "Application", "Namespace", "Pods", "Workloads",
	"Managed by", "Restarts", "Age", "Detail",
}

// applicationCells is one dashboard row as the filter sees it. It is not the
// rendered row: the health glyph and the theme have no business in a filter,
// and an age is compared against the clock rather than against the words
// "2d4h".
func applicationCells(a *application.Application) []string {
	return []string{
		a.Health.String(),
		a.Name,
		a.Namespace,
		itoa(int(a.ReadyPods)) + "/" + itoa(int(a.DesiredPods)),
		workloadSummary(a),
		a.Manager.Label(),
		itoa(int(a.Restarts)),
		formatAge(a.CreatedAt, time.Now()),
		a.Summary + " " + a.ProblemSummary(),
	}
}

// emptyFilterMessage says why nothing is left, and it says which of the two
// nothings this is: a filter that matched none of the rows, or a filter naming
// a column this screen does not have. The second one is not an empty cluster,
// and must never be able to look like one.
func (m *Model) emptyFilterMessage(total int, noun string) string {
	if problems := m.parsedQuery().Problems(m.searchColumns()); len(problems) > 0 {
		return strings.Join(problems, "; ") + ". The columns here are " +
			strings.Join(m.searchColumns(), ", ") + "."
	}
	return "Nothing matches " + m.query() + " among " + itoa(total) + " " + noun + "."
}

// searchNote says what the filter is showing, and what it is showing it out of.
func searchNote(shown, total int, complete bool) string {
	note := itoa(shown) + " of " + itoa(total)
	if !complete {
		// The rows below have not been fetched. A filter that quietly searched
		// only the first page would be the worst kind of half-answer.
		note += " loaded"
	}
	return note + " " + rowWord(total)
}

func rowWord(n int) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}
