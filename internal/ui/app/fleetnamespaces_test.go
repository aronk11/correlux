package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
)

// scopedFleetModel builds a fleet whose overview covers two namespaces.
func scopedFleetModel(t *testing.T, namespaces ...string) *Model {
	t.Helper()
	return newTestModel(t, func(o *Options) {
		o.Config.Fleet = []string{"staging", "prod-eu"}
		o.Config.FleetNamespaces = namespaces
	})
}

// TestTheFleetIsScopedToNamespacesOnScreenAndSaved is the whole feature:
// somebody picks the namespaces their team owns, every cluster in the fleet is
// read for those and nothing else, and it is still true tomorrow.
func TestTheFleetIsScopedToNamespacesOnScreenAndSaved(t *testing.T) {
	m := newTestModel(t, withConfigFile(t), func(o *Options) {
		o.Config.Fleet = []string{"staging", "prod-eu"}
	})
	press(t, m, "F")
	press(t, m, "ctrl+o")

	if m.overlay != overlayFleetNamespaces {
		t.Fatalf("the namespace key must scope the fleet, got overlay %v", m.overlay)
	}
	if title := m.fleetNSPicker.Title; !strings.Contains(title, defaultFleetGroup) {
		t.Errorf("the picker must name the group it scopes, got %q", title)
	}

	// Nothing has been read yet, so nothing can be offered — and a namespace
	// can still be named.
	typeInto(t, m, "payments")
	if out := plainView(m); !strings.Contains(out, "Use namespace") {
		t.Fatalf("a namespace nobody has listed must still be reachable:\n%s", out)
	}
	press(t, m, "tab")
	if !m.fleetNSDraft["payments"] {
		t.Fatal("Tab must pick the row under the cursor")
	}
	press(t, m, "enter")

	if m.overlay != overlayNone {
		t.Fatalf("saving must close the picker, got overlay %v", m.overlay)
	}
	if got := m.fleetNamespaces(); len(got) != 1 || got[0] != "payments" {
		t.Fatalf("fleet namespaces = %v, want the one that was picked", got)
	}

	saved, err := config.Load(m.cfg.SourcePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(saved.FleetNamespaces) != 1 || saved.FleetNamespaces[0] != "payments" {
		t.Fatalf("saved namespaces = %v, want the picked one on disk", saved.FleetNamespaces)
	}
	// The clusters are not collateral damage of scoping.
	if len(saved.Fleet) != 2 {
		t.Errorf("saved fleet = %v, want the clusters untouched", saved.Fleet)
	}
}

// TestAScopedFleetSaysWhatItCovers: every number on the screen is a number
// about those namespaces, and the screen has to say so.
func TestAScopedFleetSaysWhatItCovers(t *testing.T) {
	m := scopedFleetModel(t, "payments", "checkout")
	press(t, m, "F")

	// The scope travels with the read, so a member always says what it covers.
	for _, member := range m.fleetMembers {
		if member.Scope != "payments, checkout" {
			t.Errorf("member %s scope = %q, want the namespaces it is read with",
				member.Context, member.Scope)
		}
	}

	answer(m, ready("staging", false))
	answer(m, ready("prod-eu", true))

	out := plainView(m)
	if !strings.Contains(out, "in payments, checkout") {
		t.Errorf("the overview must name the namespaces it covers:\n%s", out)
	}
	if !strings.Contains(out, "nothing is broken in payments, checkout") {
		t.Errorf("an empty scoped fleet must not claim nothing is broken anywhere:\n%s", out)
	}
}

// TestAnUnscopedFleetStillSpeaksOfEverywhere: the wording only narrows when the
// fleet does.
func TestAnUnscopedFleetStillSpeaksOfEverywhere(t *testing.T) {
	m := scopedFleetModel(t)
	press(t, m, "F")
	answer(m, ready("staging", false))
	answer(m, ready("prod-eu", true))

	out := plainView(m)
	if !strings.Contains(out, "nothing is broken anywhere") {
		t.Errorf("an unscoped fleet covers everywhere and says so:\n%s", out)
	}
	if strings.Contains(out, " in ,") {
		t.Errorf("an empty scope must not leak into the subtitle:\n%s", out)
	}
}

// TestTheScopeCanBeGivenBack: narrowing the fleet must never be a one-way door,
// least of all in the middle of an incident.
func TestTheScopeCanBeGivenBack(t *testing.T) {
	m := newTestModel(t, withConfigFile(t), func(o *Options) {
		o.Config.Fleet = []string{"staging"}
		o.Config.FleetNamespaces = []string{"payments"}
	})
	press(t, m, "F")
	press(t, m, "ctrl+o")
	press(t, m, "ctrl+t")

	if len(m.fleetNSDraft) != 0 {
		for name, on := range m.fleetNSDraft {
			if on {
				t.Fatalf("Ctrl+T must clear the scope, %s is still picked", name)
			}
		}
	}
	press(t, m, "enter")

	if got := m.fleetNamespaces(); len(got) != 0 {
		t.Fatalf("fleet namespaces = %v, want every namespace again", got)
	}
	saved, err := config.Load(m.cfg.SourcePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(saved.FleetNamespaces) != 0 {
		t.Errorf("saved namespaces = %v, want the key gone", saved.FleetNamespaces)
	}
}

// TestOneGroupsScopeIsNotAnothers: groups exist to keep environments apart, and
// a scope that leaked between them would be the same mistake as a cluster
// leaking between them.
func TestOneGroupsScopeIsNotAnothers(t *testing.T) {
	m := newTestModel(t, withConfigFile(t), func(o *Options) {
		o.Config.FleetGroups = []config.FleetGroup{
			{Name: "production", Contexts: []string{"prod-eu"}, Namespaces: []string{"payments"}},
			{Name: "non-prod", Contexts: []string{"staging"}},
		}
	})
	press(t, m, "F")

	if got := m.fleetNamespaces(); len(got) != 1 || got[0] != "payments" {
		t.Fatalf("the opened group's scope = %v, want its own", got)
	}
	m.switchFleetGroup("non-prod")
	if got := m.fleetNamespaces(); len(got) != 0 {
		t.Fatalf("the other group's scope = %v, want every namespace", got)
	}

	// Scoping the group that is open must leave the other one alone.
	m.activeFleetGroup = "non-prod"
	m.fleetNSDraft = map[string]bool{"shop": true}
	m.saveFleetNamespaces()

	saved, err := config.Load(m.cfg.SourcePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, g := range saved.FleetGroups {
		switch g.Name {
		case "production":
			if len(g.Namespaces) != 1 || g.Namespaces[0] != "payments" {
				t.Errorf("production scope = %v, want it untouched", g.Namespaces)
			}
			if len(g.Contexts) != 1 {
				t.Errorf("production contexts = %v, want them untouched", g.Contexts)
			}
		case "non-prod":
			if len(g.Namespaces) != 1 || g.Namespaces[0] != "shop" {
				t.Errorf("non-prod scope = %v, want what was just chosen", g.Namespaces)
			}
			if len(g.Contexts) != 1 || g.Contexts[0] != "staging" {
				t.Errorf("non-prod contexts = %v, want them untouched", g.Contexts)
			}
		}
	}
}

// TestTheNamespacesTheFleetAnsweredWithAreOffered: the list is built from what
// is already known rather than from one more fan-out.
func TestTheNamespacesTheFleetAnsweredWithAreOffered(t *testing.T) {
	m := scopedFleetModel(t)
	press(t, m, "F")
	answer(m, fleet.Member{
		Context: "staging", State: fleet.Ready,
		Applications: []application.Application{
			{Name: "payments", Namespace: "shop"},
			{Name: "billing", Namespace: "finance"},
		},
	})
	press(t, m, "ctrl+o")

	out := plainView(m)
	for _, want := range []string{"shop", "finance"} {
		if !strings.Contains(out, want) {
			t.Errorf("a namespace the fleet answered with must be offered, %q missing:\n%s",
				want, out)
		}
	}
}

// TestADeniedNamespaceIsNamedRatherThanCounted: "1 kind(s) unreadable" is a
// fraction of the truth when what could not be read is a whole namespace.
func TestADeniedNamespaceIsNamedRatherThanCounted(t *testing.T) {
	member := ready("staging", false)
	member.Gaps = []application.Gap{
		{Kind: application.WholeScopeKind, Reason: "not permitted for this user", Scope: "payments"},
		{Kind: "Ingress", Reason: "not permitted for this user", Scope: "checkout"},
	}
	if got := memberDetail(member); !strings.Contains(got, "payments not readable") {
		t.Errorf("detail = %q, want the namespace named", got)
	}
	if got := memberDetail(member); !strings.Contains(got, "1 kind(s) unreadable") {
		t.Errorf("detail = %q, want the denied kind counted once", got)
	}
}

// TestBrowsingAKindAcrossAScopedFleetAsksEachNamespace: the scope is not a
// filter over what came back, it is what each cluster is asked.
func TestBrowsingAKindAcrossAScopedFleetAsksEachNamespace(t *testing.T) {
	m := scopedFleetModel(t, "shop", "finance")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")

	reads := m.fleetReads(m.fleetContexts(), pods(t, m))
	if len(reads) != 4 {
		t.Fatalf("reads = %+v, want one per cluster per namespace", reads)
	}
	for _, read := range reads {
		if read.namespace != "shop" && read.namespace != "finance" {
			t.Errorf("read %+v is outside the scope", read)
		}
	}

	// A cluster-scoped kind has no namespace to be narrowed to, and a fleet
	// that stopped mentioning its nodes would have lost the most common thing
	// that is wrong with a cluster.
	nodes := m.fleetReads(m.fleetContexts(), node(t, m))
	if len(nodes) != 2 {
		t.Fatalf("cluster-scoped reads = %+v, want one per cluster", nodes)
	}
	if nodes[0].namespace != "" {
		t.Errorf("a cluster-scoped kind must be read whole, got %+v", nodes[0])
	}
}

// TestTheMergedTableCountsClustersNotRequests: three namespaces of one cluster
// are three answers and one cluster.
func TestTheMergedTableCountsClustersNotRequests(t *testing.T) {
	m := scopedFleetModel(t, "shop", "finance")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")
	m.openFleetResourceByName("pods")

	if out := plainView(m); !strings.Contains(out, "Reading pods from 2 clusters") {
		t.Errorf("the wait must be counted in clusters, not in requests:\n%s", out)
	}

	answerPart(m, "staging", podPage("payments-1"), nil)
	answerPart(m, "staging", podPage("billing-1"), nil)

	out := plainView(m)
	if !strings.Contains(out, "in shop, finance") {
		t.Errorf("the table must say which namespaces it covers:\n%s", out)
	}
	if !strings.Contains(out, "from 1 of 2 clusters") {
		t.Errorf("a cluster that answered twice is still one cluster:\n%s", out)
	}
}

// pods and node resolve two kinds from the test catalog: one namespaced, one
// not.
func pods(t *testing.T, m *Model) kubediscovery.Resource {
	t.Helper()
	return lookup(t, m, "pods")
}

func node(t *testing.T, m *Model) kubediscovery.Resource {
	t.Helper()
	return lookup(t, m, "nodes")
}

func lookup(t *testing.T, m *Model, name string) kubediscovery.Resource {
	t.Helper()
	res, ok := m.catalog.Get().Lookup(name)
	if !ok {
		t.Fatalf("the test catalog must serve %s", name)
	}
	return res
}
