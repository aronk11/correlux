package app

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/resources"
)

func resource(group, version, plural, kind string, namespaced, builtin bool, short ...string) kubediscovery.Resource {
	return kubediscovery.Resource{
		GVR:          schema.GroupVersionResource{Group: group, Version: version, Resource: plural},
		GVK:          schema.GroupVersionKind{Group: group, Version: version, Kind: kind},
		SingularName: strings.ToLower(kind),
		ShortNames:   short,
		Namespaced:   namespaced,
		Verbs:        []string{"get", "list", "watch"},
		Builtin:      builtin,
	}
}

func testCatalog() *kubediscovery.Catalog {
	return &kubediscovery.Catalog{
		Failures: map[string]string{},
		Resources: []kubediscovery.Resource{
			resource("", "v1", "pods", "Pod", true, true, "po"),
			resource("", "v1", "nodes", "Node", false, true, "no"),
			resource("apps", "v1", "deployments", "Deployment", true, true, "deploy"),
			resource("acme.example.com", "v1alpha1", "widgets", "Widget", true, false, "wid"),
		},
	}
}

// loadCatalogInto feeds a catalog to the model the way the runtime would.
func loadCatalogInto(m *Model, catalog *kubediscovery.Catalog) {
	m.Update(catalogLoadedMsg{gen: m.catalog.Generation(), catalog: catalog})
}

func podTablePage(names ...string) *resources.Table {
	t := &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Ready", Type: "string"},
			{Name: "Status", Type: "string"},
			{Name: "Restarts", Type: "integer"},
			{Name: "Node", Type: "string", Priority: 1},
		},
		Remaining: -1,
	}
	for _, n := range names {
		t.Rows = append(t.Rows, resources.Row{
			Name:      n,
			Namespace: "default",
			Cells:     []string{n, "1/1", "Running", "0", "kind-worker"},
		})
	}
	return t
}

func TestResourcePickerListsNativeAndCustomKinds(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	press(t, m, "ctrl+b")
	if m.overlay != overlayResources {
		t.Fatal("ctrl+b must open the resource browser")
	}

	items := m.resPicker.Items()
	var pod, widget *struct{ badge string }
	for _, it := range items {
		switch it.Title {
		case "Pod":
			pod = &struct{ badge string }{it.Badge}
		case "Widget":
			widget = &struct{ badge string }{it.Badge}
		}
	}
	if pod == nil || widget == nil {
		t.Fatalf("both native and custom kinds must be listed, got %d items", len(items))
	}
	if pod.badge != "" {
		t.Errorf("a native kind must not be badged as a CRD, got %q", pod.badge)
	}
	if widget.badge != "CRD" {
		t.Errorf("a custom resource must be badged, got %q", widget.badge)
	}
}

func TestResourcePickerIsSearchableByShortName(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	press(t, m, "ctrl+b")

	typeInto(t, m, "deploy")
	if got, ok := m.resPicker.Selected(); !ok || got.Title != "Deployment" {
		t.Errorf("selected = %+v, want Deployment", got)
	}
}

func TestResourcePickerDistinguishesDiscoveryStates(t *testing.T) {
	m := newTestModel(t)
	m.catalog.Start()
	press(t, m, "ctrl+b")
	if !hasTitle(m.resPicker.Items(), "Discovering resource kinds…") {
		t.Error("discovery in flight must not look like an empty cluster")
	}

	m.Update(catalogLoadedMsg{gen: m.catalog.Generation(), err: errors.New("connection refused")})
	m.resPicker.Refresh()
	if !hasTitle(m.resPicker.Items(), "Could not discover resource kinds") {
		t.Error("a failed discovery must say so")
	}
}

func TestPartialDiscoveryIsUsableAndAnnounced(t *testing.T) {
	m := newTestModel(t)
	catalog := testCatalog()
	catalog.Failures["metrics.k8s.io/v1beta1"] = "the server is currently unable to handle the request"
	loadCatalogInto(m, catalog)

	if !strings.Contains(m.message, "could not be discovered") {
		t.Errorf("a partial discovery must be announced, message = %q", m.message)
	}
	press(t, m, "ctrl+b")
	if !hasTitle(m.resPicker.Items(), "1 API group(s) could not be discovered") {
		t.Error("the picker must explain what is missing")
	}

	found := false
	for _, it := range m.resPicker.Items() {
		if it.Title == "Pod" {
			found = true
		}
	}
	if !found {
		t.Error("healthy kinds must remain usable when one group is broken")
	}
}

func TestOpeningAResourceShowsItsTable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	press(t, m, "ctrl+b")
	typeInto(t, m, "pods")
	press(t, m, "enter")

	if m.view != viewTable {
		t.Fatal("choosing a kind must switch to the table view")
	}
	if m.resource.Kind() != "Pod" {
		t.Fatalf("resource = %q, want Pod", m.resource.Kind())
	}
	if out := view(m); !strings.Contains(out, "Loading pods…") {
		t.Errorf("the pending load must be visible:\n%s", out)
	}

	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("payments-7d8f", "worker-8a91")})
	out := view(m)
	for _, want := range []string{"NAME", "READY", "payments-7d8f", "worker-8a91"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "kind-worker") {
		t.Error("a priority column must be hidden until the wide view is asked for")
	}
}

func TestCustomResourcesOpenLikeNativeOnes(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	press(t, m, "ctrl+b")
	typeInto(t, m, "widget")
	press(t, m, "enter")

	if m.resource.Kind() != "Widget" {
		t.Fatalf("resource = %q, want Widget", m.resource.Kind())
	}
	if got := m.resource.GVR.Group; got != "acme.example.com" {
		t.Errorf("group = %q", got)
	}

	// Printer columns declared by the CRD arrive from the server and are
	// rendered without any resource-specific code.
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Phase", Type: "string"},
			{Name: "Size", Type: "integer"},
		},
		Rows:      []resources.Row{{Name: "widget-1", Cells: []string{"widget-1", "Ready", "42"}}},
		Remaining: -1,
	}})

	out := view(m)
	for _, want := range []string{"PHASE", "SIZE", "widget-1", "Ready", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("custom printer columns must render, missing %q:\n%s", want, out)
		}
	}
}

func TestWideTogglesTheHiddenColumns(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("payments-7d8f")})

	press(t, m, "w")
	if !strings.Contains(view(m), "kind-worker") {
		t.Error("the wide view must show priority columns")
	}
	press(t, m, "w")
	if strings.Contains(view(m), "kind-worker") {
		t.Error("toggling back must hide them again")
	}
}

func TestEscapeLeavesTheTableView(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")

	press(t, m, "esc")
	if m.view != viewApplications {
		t.Error("esc must return to the application dashboard, which is home")
	}
}

func TestEmptyResultIsNotConfusedWithLoading(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{Remaining: -1}})

	out := view(m)
	if !strings.Contains(out, "No pods in") {
		t.Errorf("an empty result must name the scope it applies to:\n%s", out)
	}
	if strings.Contains(out, "Loading") {
		t.Error("an empty result must not still claim to be loading")
	}
}

func TestFailedListReportsTheReason(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), err: errors.New("pods is forbidden: User cannot list resource")})

	if out := view(m); !strings.Contains(out, "Could not list pods") {
		t.Errorf("a failed list must explain itself:\n%s", out)
	}
}

func TestRowCountIsHonestAboutPaging(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")

	page := podTablePage("a", "b", "c")
	page.Continue = "token"
	page.Remaining = 4997
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: page})

	if out := view(m); !strings.Contains(out, "3 of 5000") {
		t.Errorf("a paged table must not imply it shows everything:\n%s", out)
	}
}

func TestNextPageIsAppendedNotReplaced(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")

	first := podTablePage("a", "b")
	first.Continue = "token"
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: first})

	second := podTablePage("c", "d")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: second, append: true})

	if got := len(m.tableRows()); got != 4 {
		t.Fatalf("got %d rows, want the pages merged into 4", got)
	}
	if m.tableRows()[0].Name != "a" || m.tableRows()[3].Name != "d" {
		t.Errorf("page order was not preserved: %v", m.tableRows())
	}
}

func TestStaleTablePageIsDiscarded(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	stale := m.table.Generation()

	m.openResource("deployments")
	m.Update(tableLoadedMsg{gen: stale, table: podTablePage("payments-7d8f")})

	if strings.Contains(view(m), "payments-7d8f") {
		t.Error("rows for the previously opened kind must not appear under the new one")
	}
}

func TestScopeChangeReloadsTheTable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("payments-7d8f")})

	m.switchNamespace("other")
	if got := len(m.tableRows()); got != 0 {
		t.Errorf("got %d rows after a scope change, want the table cleared", got)
	}
	out := view(m)
	if strings.Contains(out, "payments-7d8f") {
		t.Errorf("stale rows are still on screen after switching namespace:\n%s", out)
	}
	if !strings.Contains(out, "Loading pods…") {
		t.Errorf("the reload must be visible as such:\n%s", out)
	}
}

func TestClusterScopedResourceIgnoresNamespaceScope(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("nodes")
	if m.resource.Namespaced {
		t.Fatal("nodes are cluster scoped")
	}
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("kind-control-plane")})

	// Switching namespace reloads the dashboard, which is always scoped, but
	// must not refetch a table whose objects have no namespace.
	before := m.table.Generation()
	m.reloadScopedViews()
	if m.table.Generation() != before {
		t.Error("a cluster-scoped table must not be reloaded when the namespace changes")
	}
	if len(m.tableRows()) == 0 {
		t.Error("its rows must stay on screen")
	}
}

func TestPaletteOffersEveryDiscoveredKind(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	press(t, m, "ctrl+p")
	typeInto(t, m, "widget")

	for _, it := range m.cmdPal.Items() {
		if it.Title == "Open Widget" {
			return
		}
	}
	t.Error("custom resources must be reachable straight from the command palette")
}

func TestOverviewReportsDiscoveredKinds(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.backToOverview()

	if out := view(m); !strings.Contains(out, "4 listable, 1 custom") {
		t.Errorf("the overview must report what discovery found:\n%s", out)
	}
}

func TestTheWheelScrollsTheResourceTable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")

	names := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		names = append(names, "payments-"+itoa(i))
	}
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage(names...)})

	m.Update(wheel(false))
	if m.tablePort.Offset == 0 {
		t.Fatal("the wheel must scroll a resource table")
	}
	if m.tablePort.Cursor < m.tablePort.Offset {
		t.Errorf("cursor %d scrolled off the top of the viewport at offset %d", m.tablePort.Cursor, m.tablePort.Offset)
	}

	for i := 0; i < 60; i++ {
		m.Update(wheel(false))
	}
	if last := len(m.tableRows()) - 1; m.tablePort.Cursor > last {
		t.Errorf("cursor %d is past the last row %d", m.tablePort.Cursor, last)
	}
	if m.tablePort.Offset > len(m.tableRows()) {
		t.Errorf("the viewport scrolled past the end of the table, offset %d", m.tablePort.Offset)
	}
}

func TestEnterOpensTheObjectUnderTheCursor(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("widgets")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns: []resources.Column{{Name: "Name", Type: "string"}, {Name: "Phase", Type: "string"}},
		Rows: []resources.Row{
			{Name: "widget-1", Namespace: "default", Cells: []string{"widget-1", "Ready"}},
			{Name: "widget-2", Namespace: "default", Cells: []string{"widget-2", "Pending"}},
		},
		Remaining: -1,
	}})

	press(t, m, "down")
	press(t, m, "enter")

	if m.view != viewObject {
		t.Fatalf("Enter must open the object, got view %v", m.view)
	}
	if m.objectTarget.Name != "widget-2" || m.objectTarget.Kind != "Widget" {
		t.Errorf("opened %+v, want the row under the cursor", m.objectTarget)
	}
	// The exact resource travels with the reference: two groups may serve the
	// same kind, and the browser listed this one.
	if m.objectTarget.Resource != "widgets.acme.example.com" {
		t.Errorf("resource = %q, want the fully qualified name", m.objectTarget.Resource)
	}
	if out := plainView(m); !strings.Contains(out, "Loading Widget/widget-2") {
		t.Errorf("the fetch must be visible:\n%s", out)
	}
}

func TestEscapeFromAnObjectReturnsToTheTableItCameFrom(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("widgets")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns:   []resources.Column{{Name: "Name", Type: "string"}},
		Rows:      []resources.Row{{Name: "widget-1", Namespace: "default", Cells: []string{"widget-1"}}},
		Remaining: -1,
	}})

	press(t, m, "enter")
	press(t, m, "esc")

	if m.view != viewTable {
		t.Fatalf("Esc must return to the table, got view %v", m.view)
	}
	if len(m.tableRows()) != 1 {
		t.Error("the rows must still be there; going back is not a reload")
	}
}

func TestAnObjectWithoutANameIsNotOpened(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("widgets")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns:   []resources.Column{{Name: "Name", Type: "string"}},
		Rows:      []resources.Row{{Cells: []string{""}}},
		Remaining: -1,
	}})

	press(t, m, "enter")
	if m.view == viewObject {
		t.Error("a row the server gave no name for is not something to open")
	}
}

func TestAClusterScopedRowOpensWithoutANamespace(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("nodes")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns:   []resources.Column{{Name: "Name", Type: "string"}},
		Rows:      []resources.Row{{Name: "node-1", Cells: []string{"node-1"}}},
		Remaining: -1,
	}})

	press(t, m, "enter")
	if m.objectTarget.Namespace != "" {
		t.Errorf("a node has no namespace, got %q", m.objectTarget.Namespace)
	}
}

func TestTheBreadcrumbSaysWhereAnObjectWasOpenedFrom(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("widgets")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: &resources.Table{
		Columns:   []resources.Column{{Name: "Name", Type: "string"}},
		Rows:      []resources.Row{{Name: "widget-1", Namespace: "default", Cells: []string{"widget-1"}}},
		Remaining: -1,
	}})
	press(t, m, "enter")

	out := plainView(m)
	if !strings.Contains(out, "Widget") {
		t.Errorf("the breadcrumb must name the kind it was browsed from:\n%s", out)
	}
	if strings.Contains(out, "Applications → Widget") {
		t.Errorf("this object did not come through an application:\n%s", out)
	}
}
