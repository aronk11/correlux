package discovery

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// fakeDiscovery implements just enough of the discovery interface for the
// catalog. client-go's own fake returns resources but cannot simulate the
// partial-failure case, which is the one that matters most here.
type fakeDiscovery struct {
	discovery.DiscoveryInterface
	lists []*metav1.APIResourceList
	// groups overrides the preferred versions; when empty, every version in
	// lists is treated as preferred, which is what a single-version fake means.
	groups []*metav1.APIGroup
	err    error
}

func (f *fakeDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	if f.groups != nil {
		return f.groups, f.lists, f.err
	}
	return groupsOf(f.lists), f.lists, f.err
}

// groupsOf derives one preferred version per group from the lists themselves.
func groupsOf(lists []*metav1.APIResourceList) []*metav1.APIGroup {
	seen := map[string]bool{}
	var out []*metav1.APIGroup
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil || seen[gv.Group] {
			continue
		}
		seen[gv.Group] = true
		out = append(out, &metav1.APIGroup{
			Name:             gv.Group,
			Versions:         []metav1.GroupVersionForDiscovery{{GroupVersion: list.GroupVersion, Version: gv.Version}},
			PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: list.GroupVersion, Version: gv.Version},
		})
	}
	return out
}

func coreList() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{Name: "pods", SingularName: "pod", Kind: "Pod", Namespaced: true,
				ShortNames: []string{"po"}, Categories: []string{"all"},
				Verbs: []string{"get", "list", "watch", "delete"}},
			{Name: "pods/log", Kind: "Pod", Namespaced: true, Verbs: []string{"get"}},
			{Name: "nodes", SingularName: "node", Kind: "Node", Namespaced: false,
				ShortNames: []string{"no"}, Verbs: []string{"get", "list", "watch"}},
			{Name: "bindings", Kind: "Binding", Namespaced: true, Verbs: []string{"create"}},
		},
	}
}

func appsList() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{
			{Name: "deployments", SingularName: "deployment", Kind: "Deployment", Namespaced: true,
				ShortNames: []string{"deploy"}, Verbs: []string{"get", "list", "watch"}},
			{Name: "deployments/scale", Kind: "Scale", Namespaced: true,
				Verbs: []string{"get", "patch", "update"}},
		},
	}
}

func crdList() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "acme.example.com/v1alpha1",
		APIResources: []metav1.APIResource{
			{Name: "widgets", SingularName: "widget", Kind: "Widget", Namespaced: true,
				ShortNames: []string{"wid"}, Verbs: []string{"get", "list", "watch"}},
			{Name: "clusterwidgets", SingularName: "clusterwidget", Kind: "ClusterWidget",
				Namespaced: false, Verbs: []string{"get", "list"}},
		},
	}
}

func build(t *testing.T, dc discovery.DiscoveryInterface) *Catalog {
	t.Helper()
	c, err := BuildCatalog(dc)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	return c
}

func TestCatalogIncludesNativeAndCustomResources(t *testing.T) {
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{coreList(), appsList(), crdList()}})

	pods, ok := c.Lookup("pods")
	if !ok {
		t.Fatal("pods must be in the catalog")
	}
	if !pods.Builtin {
		t.Error("pods is a native resource")
	}

	widgets, ok := c.Lookup("widgets")
	if !ok {
		t.Fatal("a CRD-backed resource must be in the catalog")
	}
	if widgets.Builtin {
		t.Error("acme.example.com is not a Kubernetes group; widgets is a custom resource")
	}
	if widgets.GroupVersion() != "acme.example.com/v1alpha1" {
		t.Errorf("group version = %q", widgets.GroupVersion())
	}
	if got := len(c.CustomResources()); got != 2 {
		t.Errorf("got %d custom resources, want 2", got)
	}
}

func TestCatalogSkipsSubresourcesAndUnlistableKinds(t *testing.T) {
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{coreList()}})

	for _, name := range []string{"pods/log", "bindings"} {
		for _, r := range c.Resources {
			if r.Plural() == name {
				t.Errorf("%q must not appear in a browsable catalog", name)
			}
		}
	}
	if _, ok := c.Lookup("pods"); !ok {
		t.Error("the parent resource must still be present")
	}
}

func TestLookupResolvesKubectlNames(t *testing.T) {
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{coreList(), appsList(), crdList()}})

	for _, name := range []string{"deployments", "deployment", "deploy", "Deployment", "DEPLOY", "deployments.apps"} {
		got, ok := c.Lookup(name)
		if !ok {
			t.Errorf("Lookup(%q) found nothing", name)
			continue
		}
		if got.Kind() != "Deployment" {
			t.Errorf("Lookup(%q) = %q", name, got.Kind())
		}
	}
	for _, name := range []string{"wid", "widget", "widgets.acme.example.com"} {
		if _, ok := c.Lookup(name); !ok {
			t.Errorf("Lookup(%q) must resolve a custom resource the same way", name)
		}
	}
	if _, ok := c.Lookup("nope"); ok {
		t.Error("an unknown name must not resolve")
	}
}

func TestPartialDiscoveryFailureKeepsTheRestOfTheCatalog(t *testing.T) {
	// The classic real-world case: an aggregated API server is down. Refusing
	// to show anything because of it is the behaviour we are avoiding.
	failed := &discovery.ErrGroupDiscoveryFailed{
		Groups: map[schema.GroupVersion]error{
			{Group: "metrics.k8s.io", Version: "v1beta1"}: errors.New("the server is currently unable to handle the request"),
		},
	}
	c := build(t, &fakeDiscovery{
		lists: []*metav1.APIResourceList{coreList(), crdList()},
		err:   failed,
	})

	if !c.Partial() {
		t.Error("the catalog must report that discovery was partial")
	}
	if got := c.Failures["metrics.k8s.io/v1beta1"]; got == "" {
		t.Errorf("the failing group must be named, failures = %v", c.Failures)
	}
	if _, ok := c.Lookup("pods"); !ok {
		t.Error("healthy groups must still be usable")
	}
	if _, ok := c.Lookup("widgets"); !ok {
		t.Error("custom resources must survive an unrelated group failure")
	}
}

func TestTotalDiscoveryFailureIsAnError(t *testing.T) {
	_, err := BuildCatalog(&fakeDiscovery{err: errors.New("connection refused")})
	if err == nil {
		t.Fatal("a non-partial discovery failure must be reported")
	}
}

func TestCatalogIsSortedDeterministically(t *testing.T) {
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{crdList(), appsList(), coreList()}})

	var last string
	for _, r := range c.Resources {
		key := r.Group() + "/" + r.Plural()
		if last != "" && key < last {
			t.Fatalf("catalog is not sorted: %q came after %q", key, last)
		}
		last = key
	}
}

func TestUnparseableGroupVersionIsRecordedNotFatal(t *testing.T) {
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{
		{GroupVersion: "a/b/c", APIResources: []metav1.APIResource{{Name: "things", Verbs: []string{"list"}}}},
		coreList(),
	}})

	if !c.Partial() {
		t.Error("an unparseable group version must be reported as a failure")
	}
	if _, ok := c.Lookup("pods"); !ok {
		t.Error("the rest of the catalog must survive")
	}
}

func TestScaleSubresourcesMarkTheirParentScalable(t *testing.T) {
	// The catalog learns what can be scaled from the server rather than from a
	// list of kinds kubeui was compiled with, so a custom resource that
	// declares a scale subresource is scalable too.
	crd := crdList()
	crd.APIResources = append(crd.APIResources, metav1.APIResource{
		Name: "widgets/scale", Kind: "Scale", Namespaced: true,
		Verbs: []string{"get", "patch", "update"},
	})
	c := build(t, &fakeDiscovery{lists: []*metav1.APIResourceList{coreList(), appsList(), crd}})

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"deployments.apps", true},
		{"widgets.acme.example.com", true},
		{"pods", false},
	} {
		r, ok := c.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the catalog", tc.name)
		}
		if r.Scalable != tc.want {
			t.Errorf("%s scalable = %v, want %v", tc.name, r.Scalable, tc.want)
		}
	}

	// The subresource itself is never offered as something to browse.
	if _, ok := c.Lookup("deployments/scale"); ok {
		t.Error("a subresource must not appear in the catalog as a kind of its own")
	}
}

func TestOnlyThePreferredVersionOfAGroupIsListed(t *testing.T) {
	// The same kind served by two versions must appear once, as the version the
	// server prefers: that is what a user means when they type "widgets".
	v1 := &metav1.APIResourceList{
		GroupVersion: "acme.example.com/v1",
		APIResources: []metav1.APIResource{
			{Name: "widgets", SingularName: "widget", Kind: "Widget", Namespaced: true,
				Verbs: []string{"get", "list"}},
		},
	}
	v1alpha1 := &metav1.APIResourceList{
		GroupVersion: "acme.example.com/v1alpha1",
		APIResources: []metav1.APIResource{
			{Name: "widgets", SingularName: "widget", Kind: "Widget", Namespaced: true,
				Verbs: []string{"get", "list"}},
		},
	}
	dc := &fakeDiscovery{
		lists: []*metav1.APIResourceList{v1alpha1, v1},
		groups: []*metav1.APIGroup{{
			Name: "acme.example.com",
			Versions: []metav1.GroupVersionForDiscovery{
				{GroupVersion: "acme.example.com/v1", Version: "v1"},
				{GroupVersion: "acme.example.com/v1alpha1", Version: "v1alpha1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{
				GroupVersion: "acme.example.com/v1", Version: "v1",
			},
		}},
	}

	c := build(t, dc)
	widgets := 0
	for _, r := range c.Resources {
		if r.Plural() == "widgets" {
			widgets++
			if r.GVR.Version != "v1" {
				t.Errorf("listed version %q, want the preferred v1", r.GVR.Version)
			}
		}
	}
	if widgets != 1 {
		t.Errorf("widgets appears %d times, want once", widgets)
	}
}
