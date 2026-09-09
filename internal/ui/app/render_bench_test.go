package app

// Render benchmarks. The frame is drawn on every keystroke and on every tick
// of the timed reload, so its cost is felt directly (SPEC 20): what these
// guard against is an accidental quadratic pass or a filter run several times
// per frame, not milliseconds on any one machine.

import (
	"testing"

	"github.com/aronk11/correlux/internal/kube/resources"
)

func bigTable(n int) *resources.Table {
	t := &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Ready", Type: "string"},
			{Name: "Status", Type: "string"},
			{Name: "Restarts", Type: "integer"},
			{Name: "Age", Type: "string"},
			{Name: "Node", Type: "string", Priority: 1},
			{Name: "IP", Type: "string", Priority: 1},
		},
		Remaining: -1,
	}
	for i := 0; i < n; i++ {
		name := "payments-7d8f-" + itoa(i)
		t.Rows = append(t.Rows, resources.Row{
			Name: name, Namespace: "default",
			Cells: []string{name, "1/1", "Running", itoa(i % 17), itoa(i%90) + "m", "node-" + itoa(i%12), "10.0.0." + itoa(i%250)},
		})
	}
	return t
}

// benchModel is a resource browser holding n rows, loaded the way the runtime
// loads one.
func benchModel(b *testing.B, n int) *Model {
	b.Helper()
	m := newTestModel(&testing.T{})
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: bigTable(n)})
	return m
}

func BenchmarkViewLargeTable(b *testing.B) {
	m := benchModel(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewLargeTableSorted(b *testing.B) {
	m := benchModel(b, 5000)
	m.sortBy("Restarts")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewLargeTableFiltered(b *testing.B) {
	m := benchModel(b, 5000)
	m.search.SetValue("restarts>5")
	m.searching = true
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
