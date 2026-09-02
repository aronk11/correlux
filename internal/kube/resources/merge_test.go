package resources

import (
	"errors"
	"strings"
	"testing"
)

func table(columns []string, rows ...[]string) *Table {
	t := &Table{Remaining: -1}
	for _, name := range columns {
		t.Columns = append(t.Columns, Column{Name: name, Type: "string"})
	}
	for _, cells := range rows {
		t.Rows = append(t.Rows, Row{Name: cells[0], Namespace: "shop", Cells: cells})
	}
	return t
}

// headings renders the merged column names, which is what the assertions are
// about.
func headings(m Merged) []string {
	out := make([]string, 0, len(m.Columns))
	for _, c := range m.Columns {
		out = append(out, c.Name)
	}
	return out
}

func TestRowsFromEveryClusterCarryTheClusterTheyCameFrom(t *testing.T) {
	merged := Merge([]Part{
		{Source: "prod-eu", Table: table([]string{"Name", "Ready"}, []string{"payments", "3/3"})},
		{Source: "staging", Table: table([]string{"Name", "Ready"}, []string{"payments", "1/3"})},
	}, true)

	if got := headings(merged); got[0] != SourceColumn || got[1] != NamespaceColumn {
		t.Fatalf("columns = %v, want the cluster and namespace first", got)
	}
	if len(merged.Rows) != 2 {
		t.Fatalf("rows = %+v, want one from each cluster", merged.Rows)
	}
	if merged.Rows[0].Cells[0] != "prod-eu" || merged.Rows[1].Cells[0] != "staging" {
		t.Errorf("rows = %v, want each tagged with its cluster",
			[]string{merged.Rows[0].Cells[0], merged.Rows[1].Cells[0]})
	}
	if merged.Rows[0].Cells[1] != "shop" {
		t.Errorf("namespace = %q, want the row's own", merged.Rows[0].Cells[1])
	}
}

func TestAColumnOneClusterDoesNotHaveIsLeftEmptyNotShifted(t *testing.T) {
	// The same CRD at two versions: the newer one prints a column the older one
	// knows nothing about. A cell landing under the wrong heading would be
	// worse than a gap.
	merged := Merge([]Part{
		{Source: "old", Table: table([]string{"Name", "Phase"}, []string{"widget-1", "Ready"})},
		{Source: "new", Table: table([]string{"Name", "Phase", "Size"}, []string{"widget-2", "Ready", "42"})},
	}, false)

	if got := headings(merged); strings.Join(got, ",") != "Cluster,Name,Phase,Size" {
		t.Fatalf("columns = %v, want the union in first-seen order", got)
	}
	old, new := merged.Rows[0], merged.Rows[1]
	if old.Cells[3] != "" {
		t.Errorf("the older cluster has no Size; got %q", old.Cells[3])
	}
	if new.Cells[3] != "42" {
		t.Errorf("the newer cluster's Size = %q, want 42", new.Cells[3])
	}
	// And the shared columns still line up.
	if old.Cells[2] != "Ready" || new.Cells[2] != "Ready" {
		t.Errorf("Phase = %q / %q, want both under the same heading", old.Cells[2], new.Cells[2])
	}
}

func TestColumnsInADifferentOrderStillLineUp(t *testing.T) {
	merged := Merge([]Part{
		{Source: "a", Table: table([]string{"Name", "Phase"}, []string{"widget-1", "Ready"})},
		{Source: "b", Table: table([]string{"Phase", "Name"}, []string{"Pending", "widget-2"})},
	}, false)

	// Row from b: Phase first in its own table, but Name is column 1 here.
	row := merged.Rows[1]
	if row.Cells[1] != "widget-2" || row.Cells[2] != "Pending" {
		t.Errorf("cells = %v, want them mapped by column name", row.Cells)
	}
}

func TestAClusterThatCouldNotBeListedIsReportedNotDropped(t *testing.T) {
	merged := Merge([]Part{
		{Source: "prod-eu", Table: table([]string{"Name"}, []string{"payments"})},
		{Source: "prod-us", Err: errors.New("the server could not find the requested resource")},
	}, false)

	if len(merged.Rows) != 1 {
		t.Fatalf("only the cluster that answered contributes rows, got %+v", merged.Rows)
	}
	if len(merged.Failures) != 1 || merged.Failures[0].Source != "prod-us" {
		t.Errorf("failures = %+v, want the cluster that could not be listed", merged.Failures)
	}
}

func TestTruncationAnywhereIsTruncationEverywhere(t *testing.T) {
	partial := table([]string{"Name"}, []string{"payments"})
	partial.Continue = "more"

	merged := Merge([]Part{
		{Source: "a", Table: table([]string{"Name"}, []string{"api"})},
		{Source: "b", Table: partial},
	}, false)

	if !merged.Truncated {
		t.Error("a table with rows left behind in any cluster is not the whole picture")
	}
}

func TestMergingNothingIsNotACrash(t *testing.T) {
	merged := Merge(nil, true)
	if len(merged.Rows) != 0 || len(merged.Columns) != 2 {
		t.Errorf("merged = %+v, want just the two leading columns", merged)
	}
}
