package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/resources"
)

// typeFilter opens the filter and types into it.
func typeFilter(t *testing.T, m *Model, query string) {
	t.Helper()
	press(t, m, "/")
	typeInto(t, m, query)
}

func manyPods(names ...string) *resources.Table {
	table := &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Status", Type: "string"},
		},
		Remaining: -1,
	}
	for _, name := range names {
		status := "Running"
		if strings.HasPrefix(name, "worker") {
			status = "CrashLoopBackOff"
		}
		table.Rows = append(table.Rows, resources.Row{
			Name: name, Namespace: "default", Cells: []string{name, status},
		})
	}
	return table
}

func TestFilteringAResourceTable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(),
		table: manyPods("payments-1", "payments-2", "worker-1", "frontend-1")})

	typeFilter(t, m, "pay")

	out := plainView(m)
	if !strings.Contains(out, "payments-1") || !strings.Contains(out, "payments-2") {
		t.Errorf("the matching rows must stay:\n%s", out)
	}
	if strings.Contains(out, "worker-1") || strings.Contains(out, "frontend-1") {
		t.Errorf("the rest must go:\n%s", out)
	}
	if !strings.Contains(out, "2 of 4 rows") {
		t.Errorf("the bar must say how much of the list is shown:\n%s", out)
	}
}

func TestAFilterMatchesAnythingOnTheRow(t *testing.T) {
	// The status is on the row, so it is searchable: during an incident people
	// type what is wrong, not what a thing is called.
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1", "worker-1")})

	typeFilter(t, m, "crashloop")

	out := plainView(m)
	if !strings.Contains(out, "worker-1") || strings.Contains(out, "payments-1") {
		t.Errorf("the row must match on its state as well as its name:\n%s", out)
	}
}

func TestAFilterKeepsTheServersOrder(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(),
		table: manyPods("aaa-payments", "mmm-payments", "zzz-payments")})

	typeFilter(t, m, "payments")

	rows := m.visibleRows()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want all three", len(rows))
	}
	if rows[0].Name != "aaa-payments" || rows[2].Name != "zzz-payments" {
		t.Errorf("a filtered list must keep the order the server sorted, got %s..%s",
			rows[0].Name, rows[2].Name)
	}
}

func TestActingOnAFilteredListActsOnWhatIsVisible(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(),
		table: manyPods("payments-1", "worker-1", "worker-2")})

	typeFilter(t, m, "worker")
	press(t, m, "down") // leaves the input, moves onto the second match
	press(t, m, "enter")

	if m.objectTarget.Name != "worker-2" {
		t.Errorf("opened %q, want the second row of the filtered list", m.objectTarget.Name)
	}
}

func TestAFilterThatMatchesNothingSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1")})

	typeFilter(t, m, "nothing-like-this")

	out := plainView(m)
	if !strings.Contains(out, "Nothing matches") {
		t.Errorf("an empty result must be named, not left blank:\n%s", out)
	}
	if !strings.Contains(out, "1 loaded pods") {
		t.Errorf("it must say what it searched through:\n%s", out)
	}
}

func TestAPagedTableSaysThatItFilteredOnlyWhatIsLoaded(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	partial := manyPods("payments-1", "worker-1")
	partial.Continue = "more"
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: partial})

	typeFilter(t, m, "pay")

	if out := plainView(m); !strings.Contains(out, "loaded rows") {
		t.Errorf("a filter over a partly loaded table must admit as much:\n%s", out)
	}
}

func TestEscapeClearsTheFilterBeforeLeavingTheView(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1", "worker-1")})

	typeFilter(t, m, "pay")
	press(t, m, "esc")
	if m.filtering() {
		t.Fatal("esc must clear the filter")
	}
	if m.view != viewTable {
		t.Errorf("and not leave the view in the same press, got %v", m.view)
	}

	press(t, m, "esc")
	if m.view != viewApplications {
		t.Errorf("the next press leaves, got %v", m.view)
	}
}

func TestTheDashboardFiltersToo(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		testApplication("payments", application.Down, 0, 3),
		testApplication("frontend", application.Healthy, 2, 2),
	)

	typeFilter(t, m, "down")

	out := plainView(m)
	if !strings.Contains(out, "payments") || strings.Contains(out, "frontend") {
		t.Errorf("applications are filtered on what their row shows:\n%s", out)
	}
}

func TestTypingIntoTheFilterIsNotAShortcut(t *testing.T) {
	// A resource called "w" must be typeable without toggling wide columns.
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1")})

	wide := m.tableWide
	typeFilter(t, m, "w")
	if m.tableWide != wide {
		t.Error("the filter must own every key while it has focus")
	}
	if m.search.Value() != "w" {
		t.Errorf("the character must land in the filter, got %q", m.search.Value())
	}
}
