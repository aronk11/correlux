//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// Budgets for the assertions below. They are deliberately loose: the point is
// to catch an order-of-magnitude regression — an accidental unbounded list, a
// quadratic render — not to police milliseconds on someone's laptop.
const (
	budgetFirstPage = 5 * time.Second
	budgetCatalog   = 10 * time.Second
	budgetRender    = 150 * time.Millisecond
)

func benchCtx(tb testing.TB) context.Context {
	tb.Helper()
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	tb.Cleanup(cancel)
	return c
}

func TestFirstPageIsFastOnALoadedCluster(t *testing.T) {
	res, ok := catalogFor(t).Lookup("pods")
	if !ok {
		t.Fatal("pods not found")
	}

	start := time.Now()
	table, err := shared.factory.ListTable(benchCtx(t), shared.context, res,
		resources.ListOptions{Limit: resources.DefaultPageSize})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}

	t.Logf("first page: %d rows in %s (%d more on the server)", len(table.Rows), elapsed.Round(time.Millisecond), table.Remaining)
	if elapsed > budgetFirstPage {
		t.Errorf("the first page took %s, budget is %s — the UI must never wait this long", elapsed, budgetFirstPage)
	}
	if int64(len(table.Rows)) > resources.DefaultPageSize {
		t.Errorf("got %d rows for a page size of %d; the list was not bounded", len(table.Rows), resources.DefaultPageSize)
	}
}

func TestDiscoveryIsFastEnoughToRunAtStartup(t *testing.T) {
	start := time.Now()
	catalog, err := shared.factory.Catalog(benchCtx(t), shared.context)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}

	t.Logf("discovered %d kinds in %s", catalog.Len(), elapsed.Round(time.Millisecond))
	if elapsed > budgetCatalog {
		t.Errorf("discovery took %s, budget is %s", elapsed, budgetCatalog)
	}
}

func TestRenderingALargeTableStaysInteractive(t *testing.T) {
	// Rendering is on the keystroke path: every cursor move redraws. A frame
	// that takes longer than a few milliseconds is felt.
	data := syntheticTable(10000)
	th := theme.New(theme.Capabilities{Color: true, Unicode: true, Attributes: true, Dark: true}, config.ThemeAuto)

	start := time.Now()
	for i := 0; i < 20; i++ {
		data.Offset = i * 17
		data.Cursor = data.Offset
		if out := screens.RenderTable(th, data, 140, 40); out == "" {
			t.Fatal("render produced nothing")
		}
	}
	perFrame := time.Since(start) / 20

	t.Logf("rendered a 10000-row table in %s per frame", perFrame.Round(time.Microsecond))
	if perFrame > budgetRender {
		t.Errorf("a frame took %s, budget is %s", perFrame, budgetRender)
	}
}

func syntheticTable(rows int) screens.TableData {
	d := screens.TableData{
		Columns: []screens.TableColumn{
			{Title: "Name"}, {Title: "Ready"}, {Title: "Status"},
			{Title: "Restarts", Right: true}, {Title: "Age"}, {Title: "Node", Wide: true},
		},
		Rows: make([]screens.TableRow, 0, rows),
	}
	for i := 0; i < rows; i++ {
		name := "app-" + strings.Repeat("x", i%12) + "-" + itoa(i)
		d.Rows = append(d.Rows, screens.TableRow{
			Cells: []string{name, "1/1", "Running", "0", "3d", "correlux-load-node"},
		})
	}
	return d
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func BenchmarkCatalog(b *testing.B) {
	for b.Loop() {
		if _, err := shared.factory.Catalog(benchCtx(b), shared.context); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPodsPage(b *testing.B) {
	catalog, err := shared.factory.Catalog(benchCtx(b), shared.context)
	if err != nil {
		b.Fatal(err)
	}
	res, ok := catalog.Lookup("pods")
	if !ok {
		b.Fatal("pods not found")
	}

	b.ReportAllocs()
	for b.Loop() {
		table, err := shared.factory.ListTable(benchCtx(b), shared.context, res,
			resources.ListOptions{Limit: resources.DefaultPageSize})
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(table.Rows)))
	}
}

func BenchmarkListCustomResourcePage(b *testing.B) {
	catalog, err := shared.factory.Catalog(benchCtx(b), shared.context)
	if err != nil {
		b.Fatal(err)
	}
	res, ok := catalog.Lookup("widgets")
	if !ok {
		b.Skip("no seeded CRD")
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := shared.factory.ListTable(benchCtx(b), shared.context, res,
			resources.ListOptions{Limit: resources.DefaultPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPageThroughEverything(b *testing.B) {
	catalog, err := shared.factory.Catalog(benchCtx(b), shared.context)
	if err != nil {
		b.Fatal(err)
	}
	res, _ := catalog.Lookup("pods")

	b.ReportAllocs()
	for b.Loop() {
		opts := resources.ListOptions{Limit: resources.DefaultPageSize}
		total := 0
		for {
			table, err := shared.factory.ListTable(benchCtx(b), shared.context, res, opts)
			if err != nil {
				b.Fatal(err)
			}
			total += len(table.Rows)
			if !table.HasMore() {
				break
			}
			opts.Continue = table.Continue
		}
		b.SetBytes(int64(total))
	}
}

func BenchmarkRenderTable10k(b *testing.B) {
	data := syntheticTable(10000)
	th := theme.New(theme.Capabilities{Color: true, Unicode: true, Attributes: true, Dark: true}, config.ThemeAuto)

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		data.Offset = i % 9000
		data.Cursor = data.Offset
		_ = screens.RenderTable(th, data, 140, 40)
	}
}

// BenchmarkFullFrame measures a complete redraw of the application against
// live data — the cost paid on every keystroke.
func BenchmarkFullFrame(b *testing.B) {
	m := newModelFor(b)
	drain(b, m, m.Init())
	drain(b, m, m.OpenResourceForTest("pods"))

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}
