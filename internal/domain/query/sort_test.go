package query

import (
	"strings"
	"testing"
)

func names(columns []string, rows [][]string, order []int, col int) string {
	out := make([]string, 0, len(order))
	for _, i := range order {
		out = append(out, rows[i][col])
	}
	return strings.Join(out, ",")
}

func TestOrderComparesNumbersAsNumbers(t *testing.T) {
	columns := []string{"Name", "Restarts"}
	rows := [][]string{
		{"a", "9"},
		{"b", "12"},
		{"c", "2"},
	}
	// Lexically "12" sorts before "2"; the point of a typed sort is that it
	// does not here.
	if got := names(columns, rows, Order(columns, rows, "restarts", false), 0); got != "c,a,b" {
		t.Errorf("ascending by restarts = %s, want c,a,b", got)
	}
	if got := names(columns, rows, Order(columns, rows, "restarts", true), 0); got != "b,a,c" {
		t.Errorf("descending by restarts = %s, want b,a,c", got)
	}
}

func TestOrderReadsAnAgeColumnAsADuration(t *testing.T) {
	columns := []string{"Name", "Age"}
	rows := [][]string{
		{"a", "2d"},
		{"b", "45s"},
		{"c", "1h30m"},
	}
	if got := names(columns, rows, Order(columns, rows, "age", false), 0); got != "b,c,a" {
		t.Errorf("ascending by age = %s, want b,c,a", got)
	}
}

func TestOrderUsesTheSameSuffixesTheFilterDoes(t *testing.T) {
	columns := []string{"Name", "CPU"}
	rows := [][]string{
		{"a", "2"},
		{"b", "500m"},
		{"c", "1500m"},
	}
	// 500m is half a CPU here, not five hundred of anything.
	if got := names(columns, rows, Order(columns, rows, "cpu", false), 0); got != "b,c,a" {
		t.Errorf("ascending by cpu = %s, want b,c,a", got)
	}
}

func TestCellsWithNothingInThemSortLastEitherWay(t *testing.T) {
	columns := []string{"Name", "Restarts"}
	rows := [][]string{
		{"a", "—"},
		{"b", "3"},
		{"c", ""},
		{"d", "1"},
	}
	// An unset value is not a small one: a column sorted worst-first must not
	// open on a screen of blanks.
	for _, desc := range []bool{false, true} {
		order := Order(columns, rows, "restarts", desc)
		got := names(columns, rows, order, 0)
		if last := strings.Split(got, ",")[2:]; last[0] != "a" && last[0] != "c" {
			t.Errorf("desc=%v: empty cells must trail, got %s", desc, got)
		}
	}
}

func TestOrderIsStable(t *testing.T) {
	columns := []string{"Name", "Status"}
	rows := [][]string{
		{"a", "Running"},
		{"b", "Running"},
		{"c", "Running"},
	}
	// Rows the column cannot tell apart keep the order the server gave them,
	// or a table reshuffles itself on every reload.
	if got := names(columns, rows, Order(columns, rows, "status", false), 0); got != "a,b,c" {
		t.Errorf("equal rows must keep their order, got %s", got)
	}
}

func TestOrderLeavesRowsAloneWhenTheColumnIsNotThere(t *testing.T) {
	columns := []string{"Name", "Status"}
	rows := [][]string{{"a", "Running"}, {"b", "Pending"}}
	if got := names(columns, rows, Order(columns, rows, "restarts", false), 0); got != "a,b" {
		t.Errorf("an unknown column must not reorder anything, got %s", got)
	}
}

func TestOrderAcceptsThePrefixesAndAliasesTheFilterDoes(t *testing.T) {
	columns := []string{"Namespace", "Restarts"}
	rows := [][]string{{"shop", "9"}, {"api", "1"}}
	// "ns" is the alias, "rest" the prefix: sorting must answer to the same
	// names the filter does or the two read as different features.
	if got := names(columns, rows, Order(columns, rows, "ns", false), 0); got != "api,shop" {
		t.Errorf("alias ns = %s, want api,shop", got)
	}
	if got := names(columns, rows, Order(columns, rows, "rest", false), 0); got != "api,shop" {
		t.Errorf("prefix rest = %s, want api,shop", got)
	}
}
