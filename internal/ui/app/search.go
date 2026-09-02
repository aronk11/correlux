package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sahilm/fuzzy"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// Searching filters what is already on screen.
//
// It is a filter, not a query: kubeui narrows the rows it has rather than
// asking the API server a different question. That distinction is not a
// shortcut, it is the honest one — a server-side selector cannot match on the
// cells a printer produced, and a table that silently changed what it was
// listing would be worse than one that says how much it is showing.

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

// matches returns the indices of the rows a query keeps, in their original
// order.
//
// The match is fuzzy over the whole row: typing "payei" finds
// "payments-7d8f  ImagePullBackOff", because during an incident people type
// what they remember, not what they can see.
func matches(rows [][]string, query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	haystack := make([]string, len(rows))
	for i, cells := range rows {
		haystack[i] = strings.ToLower(strings.Join(cells, " "))
	}

	found := fuzzy.Find(strings.ToLower(query), haystack)
	out := make([]int, 0, len(found))
	for _, match := range found {
		out = append(out, match.Index)
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

// visibleRows are the resource table's rows after the filter.
func (m *Model) visibleRows() []resources.Row {
	rows := m.tableRows()
	if !m.filtering() {
		return rows
	}
	cells := make([][]string, len(rows))
	for i := range rows {
		cells[i] = rows[i].Cells
	}
	out := make([]resources.Row, 0, len(rows))
	for _, i := range matches(cells, m.query()) {
		out = append(out, rows[i])
	}
	return out
}

// visibleApplications are the dashboard's applications after the filter. The
// haystack is what the row shows, so what you can read is what you can search.
func (m *Model) visibleApplications() []application.Application {
	apps := m.applications()
	if !m.filtering() {
		return apps
	}
	cells := make([][]string, len(apps))
	for i := range apps {
		a := &apps[i]
		cells[i] = []string{
			a.Name, a.Namespace, a.Health.String(), a.Summary,
			a.ProblemSummary(), a.Manager.Label(), workloadSummary(a),
		}
	}
	out := make([]application.Application, 0, len(apps))
	for _, i := range matches(cells, m.query()) {
		out = append(out, apps[i])
	}
	return out
}

// visibleFleetRows are the cross-cluster table's rows after the filter.
func (m *Model) visibleFleetRows() []resources.MergedRow {
	rows := m.fleetTable.Rows
	if !m.filtering() {
		return rows
	}
	cells := make([][]string, len(rows))
	for i := range rows {
		cells[i] = rows[i].Cells
	}
	out := make([]resources.MergedRow, 0, len(rows))
	for _, i := range matches(cells, m.query()) {
		out = append(out, rows[i])
	}
	return out
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
