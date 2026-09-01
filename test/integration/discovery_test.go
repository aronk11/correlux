//go:build integration

package integration

import (
	"strings"
	"testing"

	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
)

func TestProbeReportsTheServerVersion(t *testing.T) {
	info := shared.factory.Probe(ctx(t), shared.context)

	if info.State != kubeclient.ConnOK {
		t.Fatalf("probe state = %v: %v", info.State, info.Err)
	}
	if !strings.HasPrefix(info.ServerVersion, "v1.") {
		t.Errorf("server version = %q, want something like v1.34.0", info.ServerVersion)
	}
	if info.Latency <= 0 {
		t.Error("the probe must measure its own latency")
	}
}

func TestCatalogFindsNativeResources(t *testing.T) {
	catalog, err := shared.factory.Catalog(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}

	// A real cluster serves far more than a handful of kinds; a small number
	// here would mean discovery quietly gave up.
	if catalog.Len() < 40 {
		t.Errorf("catalog has %d listable kinds, want at least 40", catalog.Len())
	}

	for _, name := range []string{"pods", "deployments", "services", "nodes", "configmaps", "customresourcedefinitions"} {
		res, ok := catalog.Lookup(name)
		if !ok {
			t.Errorf("%q is missing from the catalog", name)
			continue
		}
		if !res.Builtin {
			t.Errorf("%q must be classified as a native resource", name)
		}
	}

	pods, _ := catalog.Lookup("po")
	if pods.Kind() != "Pod" || !pods.Namespaced {
		t.Errorf("short name resolution is wrong: %+v", pods)
	}
	nodes, _ := catalog.Lookup("nodes")
	if nodes.Namespaced {
		t.Error("nodes are cluster scoped")
	}
}

func TestCatalogFindsSeededCustomResources(t *testing.T) {
	catalog, err := shared.factory.Catalog(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}

	widgets, ok := catalog.Lookup("widgets")
	if !ok {
		t.Fatal("the seeded CRD is not in the catalog; run `task kind:seed` first")
	}
	if widgets.Builtin {
		t.Error("a CRD-backed resource must not be classified as native")
	}
	if widgets.Kind() != "Widget" || widgets.GVR.Group != "load.kubeui.dev" {
		t.Errorf("resource = %+v", widgets)
	}
	if !widgets.Namespaced {
		t.Error("the seeded CRD is namespace scoped")
	}

	// The same lookups a user would type.
	for _, name := range []string{"widget", "wid", "widgets.load.kubeui.dev", "Widget"} {
		if _, ok := catalog.Lookup(name); !ok {
			t.Errorf("Lookup(%q) failed for a custom resource", name)
		}
	}

	if len(catalog.CustomResources()) == 0 {
		t.Error("CustomResources() must report the seeded CRDs")
	}
}

func TestCatalogSkipsSubresources(t *testing.T) {
	catalog, err := shared.factory.Catalog(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	for _, r := range catalog.Resources {
		if strings.Contains(r.Plural(), "/") {
			t.Errorf("%q is a subresource and must not be browsable", r.Plural())
		}
		if !r.Listable() {
			t.Errorf("%q cannot be listed and must not be in the catalog", r.Plural())
		}
	}
}

func TestNamespaceListingSeesTheSeededNamespaces(t *testing.T) {
	list, err := shared.factory.ListNamespaces(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if list.Restricted {
		t.Fatal("the test cluster's admin user may list namespaces")
	}

	var seeded int
	for _, ns := range list.Names {
		if strings.HasPrefix(ns, "kubeui-load-") {
			seeded++
		}
	}
	if seeded == 0 {
		t.Error("no seeded namespaces found; run `task kind:seed` first")
	}
	for _, want := range []string{"default", "kube-system"} {
		if !contains(list.Names, want) {
			t.Errorf("%q is missing from the namespace list", want)
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
