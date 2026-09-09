package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
)

// appWithRestarts is a dashboard row whose restart count is the thing under
// test, so the order on screen is unambiguous.
func appWithRestarts(name string, health application.Health, restarts int32) application.Application {
	a := testApplication(name, health, 3, 3)
	a.Restarts = restarts
	return a
}

// dashboardOrder reads the application names off the rendered dashboard, in
// the order they are drawn.
func dashboardOrder(m *Model) []string {
	apps := m.visibleApplications()
	out := make([]string, 0, len(apps))
	for i := range apps {
		out = append(out, apps[i].Name)
	}
	return out
}

func TestSortingTheDashboardByAColumn(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
		appWithRestarts("payments", application.Healthy, 7),
	)

	m.sortBy("Restarts")
	// Restarts opens at the top: the reason to sort by restarts during an
	// incident is never to find the pod that has not restarted.
	if got := dashboardOrder(m); strings.Join(got, ",") != "worker,payments,api" {
		t.Errorf("first sort by restarts = %v, want worker,payments,api", got)
	}
	// The same column again reverses it.
	m.sortBy("Restarts")
	if got := dashboardOrder(m); strings.Join(got, ",") != "api,payments,worker" {
		t.Errorf("reversed = %v, want api,payments,worker", got)
	}
}

func TestTheSortedColumnSaysSoInTheHeader(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, appWithRestarts("api", application.Healthy, 2))
	m.sortBy("Restarts")

	out := plainView(m)
	if !strings.Contains(out, "RESTARTS "+m.theme.Glyphs.SortDesc) {
		t.Errorf("the header must carry the sort direction:\n%s", out)
	}
	if !strings.Contains(out, "Restarts") {
		t.Errorf("the status bar must name the column the table is in:\n%s", out)
	}
}

func TestSortingSurvivesNothingItShouldNot(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, appWithRestarts("api", application.Healthy, 2))
	m.sortBy("Restarts")

	// Each table keeps its own order: a dashboard sorted by restarts says
	// nothing about how somebody wants a list of pods.
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	if m.currentSort().active() {
		t.Error("the resource browser must open in the server's own order")
	}
	m.backToApplications()
	if !m.currentSort().active() {
		t.Error("the dashboard must still be in the order it was put in")
	}
}

func TestClearingTheSortRestoresTheDefaultOrder(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
	)
	before := strings.Join(dashboardOrder(m), ",")

	m.sortBy("Application")
	m.clearSort()
	if got := strings.Join(dashboardOrder(m), ","); got != before {
		t.Errorf("clearing the sort = %s, want the original %s", got, before)
	}
}

func TestSortAndFilterCompose(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("payments-api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
		appWithRestarts("payments-web", application.Healthy, 9),
	)

	typeFilter(t, m, "payments")
	m.sortBy("Restarts")
	// Narrow to what matters, then put the worst of it first.
	if got := dashboardOrder(m); strings.Join(got, ",") != "payments-web,payments-api" {
		t.Errorf("filtered and sorted = %v, want payments-web,payments-api", got)
	}
}

func TestSortingPutsTheCursorBackOnTheFirstRow(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
		appWithRestarts("payments", application.Healthy, 7),
	)
	m.moveAppCursor(2)

	m.sortBy("Restarts")
	// Reordering moves every row out from under the cursor, and the next key
	// pressed may act on whatever it is left sitting on.
	if m.appPort.Cursor != 0 {
		t.Errorf("cursor is on row %d, want the first row", m.appPort.Cursor)
	}
}

func TestClickingAHeadingSortsByThatColumn(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
	)

	// The heading is the first line of the body.
	m.handleClick(clickAt(0, m.screen.Body.Y))
	if !m.currentSort().active() {
		t.Fatal("clicking a heading must sort by it")
	}
	if got := m.currentSort().column; got != "Status" {
		t.Errorf("clicked the first heading, sorted by %q", got)
	}
}

func TestClickingARowSelectsItAndClickingItAgainOpensIt(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
	)

	// Second body line is the second row: one for the heading, one for the
	// first row.
	m.handleClick(clickAt(2, m.screen.Body.Y+2))
	if m.appPort.Cursor != 1 {
		t.Fatalf("clicking a row must select it, cursor is %d", m.appPort.Cursor)
	}
	if m.view != viewApplications {
		t.Fatal("one click selects, it does not navigate")
	}
	m.handleClick(clickAt(2, m.screen.Body.Y+2))
	if m.view != viewApplication {
		t.Error("clicking the selected row must open it")
	}
}

func clickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestTheSortPickerOffersTheColumnsOnScreen(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, appWithRestarts("api", application.Healthy, 2))

	press(t, m, "s")
	if m.overlay != overlaySort {
		t.Fatal("s must open the sort picker on a table")
	}
	titles := make([]string, 0)
	for _, item := range m.sortPicker.Items() {
		titles = append(titles, item.Title)
	}
	joined := strings.Join(titles, "|")
	for _, want := range []string{"Restarts", "Application", "Worst first (default)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the picker must offer %q, offered %s", want, joined)
		}
	}
}

func TestTheSortKeyDoesNothingWhereThereIsNoTable(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, appWithRestarts("api", application.Healthy, 2))
	press(t, m, "enter") // into the application detail, which is not a table
	press(t, m, "s")
	if m.overlay != overlayNone {
		t.Error("the sort picker must not open over a screen it cannot reorder")
	}
}

func TestSortingAResourceTable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	table := podTablePage("api-1", "worker-2", "payments-3")
	table.Rows[0].Cells[3] = "2"
	table.Rows[1].Cells[3] = "41"
	table.Rows[2].Cells[3] = "7"
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: table})

	m.sortBy("Restarts")
	got := make([]string, 0, 3)
	for _, r := range m.visibleRows() {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != "worker-2,payments-3,api-1" {
		t.Errorf("sorted pods = %v, want worker-2,payments-3,api-1", got)
	}
	// The column the server sorted by is still the one it returned; sorting is
	// a client-side reordering of the rows already loaded.
	m.clearSort()
	got = got[:0]
	for _, r := range m.visibleRows() {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != "api-1,worker-2,payments-3" {
		t.Errorf("cleared = %v, want the server's order", got)
	}
}

func TestSortingIsKeptAcrossAReload(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 2),
		appWithRestarts("worker", application.Healthy, 41),
	)
	m.sortBy("Restarts")

	// A timed reload must not quietly put the table back into an order the
	// user changed.
	loadApplicationsInto(m,
		appWithRestarts("api", application.Healthy, 3),
		appWithRestarts("worker", application.Healthy, 44),
	)
	if got := dashboardOrder(m); strings.Join(got, ",") != "worker,api" {
		t.Errorf("after a reload = %v, want worker,api", got)
	}
}

func TestSortingAPagedTableSaysWhatItCovers(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	table := podTablePage("api-1", "worker-2")
	table.Continue = "next-page-token" // the cluster has more than this
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: table})

	m.sortBy("Restarts")
	// The first row of a sorted page is the worst of what is loaded, which is
	// a different claim from the worst in the cluster.
	if out := plainView(m); !strings.Contains(out, "sorted among those loaded") {
		t.Errorf("a sorted paged table must say what its order covers:\n%s", out)
	}

	// A table read whole makes no such qualification: the arrow says it all.
	whole := podTablePage("api-1", "worker-2")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: whole})
	if out := plainView(m); strings.Contains(out, "sorted among those loaded") {
		t.Errorf("a complete table must not qualify its order:\n%s", out)
	}
}
