//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

func catalogFor(t *testing.T) *discovery.Catalog {
	t.Helper()
	catalog, err := shared.factory.Catalog(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	return catalog
}

func lookup(t *testing.T, name string) discovery.Resource {
	t.Helper()
	res, ok := catalogFor(t).Lookup(name)
	if !ok {
		t.Fatalf("resource %q not found", name)
	}
	return res
}

func TestListTableRendersNativeResources(t *testing.T) {
	table, err := shared.factory.ListTable(ctx(t), shared.context, lookup(t, "pods"), resources.ListOptions{Limit: 20})
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}

	// These are the API server's own column names for pods.
	want := map[string]bool{"Name": false, "Ready": false, "Status": false, "Restarts": false, "Age": false}
	for _, c := range table.Columns {
		if _, ok := want[c.Name]; ok {
			want[c.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("the pod table is missing the %q column", name)
		}
	}
	if len(table.Rows) == 0 {
		t.Fatal("no pods returned; run `task kind:seed` first")
	}
	for _, row := range table.Rows {
		if row.Name == "" {
			t.Error("every row must carry its object name")
		}
		if len(row.Cells) != len(table.Columns) {
			t.Errorf("row %q has %d cells for %d columns", row.Name, len(row.Cells), len(table.Columns))
		}
	}
}

func TestListTableRendersCRDPrinterColumns(t *testing.T) {
	// This is what "CRD support" means in practice: the columns the CRD author
	// declared arrive from the server, and kubeui needs no code for them.
	table, err := shared.factory.ListTable(ctx(t), shared.context, lookup(t, "widgets"), resources.ListOptions{Limit: 20})
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}

	var names []string
	for _, c := range table.Columns {
		names = append(names, c.Name)
	}
	for _, want := range []string{"Name", "Phase", "Size", "Owner", "Age"} {
		if !contains(names, want) {
			t.Errorf("the widget table is missing the %q column declared by the CRD; got %v", want, names)
		}
	}

	var owner resources.Column
	for _, c := range table.Columns {
		if c.Name == "Owner" {
			owner = c
		}
	}
	if !owner.Wide() {
		t.Error("a printer column with priority 1 must be classified as wide")
	}

	if len(table.Rows) == 0 {
		t.Fatal("no widgets returned; run `task kind:seed` first")
	}
	var sawPhase bool
	for _, row := range table.Rows {
		for _, cell := range row.Cells {
			if cell == "Ready" || cell == "Failed" || cell == "Progressing" {
				sawPhase = true
			}
		}
	}
	if !sawPhase {
		t.Error("the status printer column must carry the seeded phase")
	}
}

func TestListTableScopesToANamespace(t *testing.T) {
	pods := lookup(t, "pods")

	all, err := shared.factory.ListTable(ctx(t), shared.context, pods, resources.ListOptions{Limit: 500})
	if err != nil {
		t.Fatalf("ListTable(all): %v", err)
	}
	scoped, err := shared.factory.ListTable(ctx(t), shared.context, pods,
		resources.ListOptions{Namespace: "kube-system", Limit: 500})
	if err != nil {
		t.Fatalf("ListTable(kube-system): %v", err)
	}

	if len(scoped.Rows) == 0 {
		t.Fatal("kube-system always has pods")
	}
	for _, row := range scoped.Rows {
		if row.Namespace != "kube-system" {
			t.Errorf("row %q is in namespace %q, want kube-system", row.Name, row.Namespace)
		}
	}
	if len(scoped.Rows) >= len(all.Rows) {
		t.Errorf("scoping returned %d rows and the cluster-wide list %d; the scope had no effect",
			len(scoped.Rows), len(all.Rows))
	}
}

func TestPagingTerminatesAndDoesNotRepeatObjects(t *testing.T) {
	// The property that matters on a large cluster: paging ends, and every
	// object appears exactly once.
	pods := lookup(t, "pods")
	seen := map[string]int{}

	opts := resources.ListOptions{Limit: 25}
	pages := 0
	for {
		table, err := shared.factory.ListTable(ctx(t), shared.context, pods, opts)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		for _, row := range table.Rows {
			seen[row.Namespace+"/"+row.Name]++
		}
		if !table.HasMore() {
			break
		}
		if pages > 500 {
			t.Fatal("paging did not terminate")
		}
		opts.Continue = table.Continue
	}

	if pages < 2 {
		t.Skip("the cluster is too small to page; seed more pods to exercise this")
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("%s appeared %d times across pages", key, count)
		}
	}
	t.Logf("paged through %d pods in %d pages", len(seen), pages)
}

func TestListTableRespectsLabelSelectors(t *testing.T) {
	table, err := shared.factory.ListTable(ctx(t), shared.context, lookup(t, "pods"), resources.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=kubeui-seed",
		Limit:         200,
	})
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}
	if len(table.Rows) == 0 {
		t.Fatal("no seeded pods matched the selector")
	}
	for _, row := range table.Rows {
		if !strings.HasPrefix(row.Namespace, "kubeui-load-") {
			t.Errorf("row %s/%s does not belong to the seeded load", row.Namespace, row.Name)
		}
	}
}

func TestListTableOnAClusterScopedResource(t *testing.T) {
	table, err := shared.factory.ListTable(ctx(t), shared.context, lookup(t, "nodes"), resources.ListOptions{})
	if err != nil {
		t.Fatalf("ListTable(nodes): %v", err)
	}
	if len(table.Rows) == 0 {
		t.Fatal("a cluster has nodes")
	}
	for _, row := range table.Rows {
		if row.Namespace != "" {
			t.Errorf("a cluster-scoped object must have no namespace, got %q", row.Namespace)
		}
	}
}

func TestSeededHealthMixIsVisible(t *testing.T) {
	// kubeui exists to show what is broken; the test cluster must therefore
	// contain something broken, and it must be visible in the table.
	table, err := shared.factory.ListTable(ctx(t), shared.context, lookup(t, "pods"), resources.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=kubeui-seed",
		Limit:         500,
	})
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}

	states := map[string]int{}
	for _, row := range table.Rows {
		for _, cell := range row.Cells {
			switch cell {
			case "Running", "CrashLoopBackOff", "ImagePullBackOff", "OOMKilled", "Error":
				states[cell]++
			}
		}
	}
	if states["Running"] == 0 {
		t.Error("the seeded cluster must contain healthy pods")
	}
	if states["CrashLoopBackOff"]+states["ImagePullBackOff"]+states["OOMKilled"]+states["Error"] == 0 {
		t.Errorf("the seeded cluster must contain unhealthy pods, saw %v", states)
	}
	t.Logf("seeded pod states: %v", states)
}
