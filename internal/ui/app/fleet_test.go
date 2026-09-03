package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	"github.com/aronk11/correlux/internal/kube/resources"
)

// fleetModel builds a model whose fleet covers the given contexts. They exist
// in the fixture kubeconfig.
func fleetModel(t *testing.T, contexts ...string) *Model {
	t.Helper()
	return newTestModel(t, func(o *Options) { o.Config.Fleet = contexts })
}

// answer delivers one cluster's result the way the reader would.
func answer(m *Model, member fleet.Member) {
	m.Update(fleetMemberMsg{gen: m.fleetGeneration, member: member})
}

func ready(context string, production bool, apps ...application.Application) fleet.Member {
	return fleet.Member{
		Context: context, Production: production, State: fleet.Ready, Applications: apps,
	}
}

func fleetApp(name string, health application.Health, ready, desired int32) application.Application {
	return application.Application{
		Name: name, Namespace: "shop", Health: health,
		ReadyPods: ready, DesiredPods: desired, Summary: "the summary",
	}
}

func TestTheFleetIsEmptyUntilItIsConfigured(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "F")

	if m.view != viewFleet {
		t.Fatalf("F must open the fleet, got view %v", m.view)
	}
	out := plainView(m)
	if !strings.Contains(out, "No fleet configured") {
		t.Errorf("an unconfigured fleet must say so rather than showing nothing:\n%s", out)
	}
	if len(m.fleetMembers) != 0 {
		t.Error("Correlux must not reach for a cluster nobody named")
	}
}

func TestEveryConfiguredClusterAppearsWhileItIsStillBeingRead(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")

	if len(m.fleetMembers) != 2 {
		t.Fatalf("members = %+v, want both configured clusters", m.fleetMembers)
	}
	out := plainView(m)
	for _, want := range []string{"staging", "prod-eu", "connecting"} {
		if !strings.Contains(out, want) {
			t.Errorf("a cluster still being read must be on screen as such, missing %q:\n%s", want, out)
		}
	}
	// A production cluster is marked, in text, wherever it appears.
	if !strings.Contains(out, "prod-eu  PROD") {
		t.Errorf("production must be marked in the list:\n%s", out)
	}
}

func TestAnswersArriveOneClusterAtATime(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")

	answer(m, ready("staging", false, fleetApp("payments", application.Healthy, 3, 3)))
	out := plainView(m)
	if !strings.Contains(out, "nothing broken") {
		t.Errorf("the cluster that answered must be shown at once:\n%s", out)
	}
	if !strings.Contains(out, "connecting") {
		t.Errorf("the one still being read must still say so:\n%s", out)
	}
	if !strings.Contains(out, "of 2") {
		t.Errorf("a partial fleet must not present its totals as complete:\n%s", out)
	}
}

func TestWhatIsBrokenIsListedWithItsCluster(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")

	answer(m, ready("prod-eu", true,
		fleetApp("payments", application.Down, 0, 3),
		fleetApp("api", application.Healthy, 2, 2),
	))
	answer(m, ready("staging", false, fleetApp("payments", application.Degraded, 2, 3)))

	out := plainView(m)
	if !strings.Contains(out, "WHAT IS BROKEN") {
		t.Fatalf("the broken applications need their own section:\n%s", out)
	}
	if !strings.Contains(out, "payments") || !strings.Contains(out, "down") {
		t.Errorf("the failing application and its state must appear:\n%s", out)
	}
	// A healthy application is not an incident and does not belong in the list.
	if strings.Contains(out, "api") {
		t.Errorf("only what is broken belongs here:\n%s", out)
	}
}

func TestAnUnreachableClusterIsNamedNotOmitted(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")

	answer(m, ready("staging", false, fleetApp("payments", application.Healthy, 3, 3)))
	answer(m, fleet.Member{Context: "prod-eu", Production: true, State: fleet.Failed,
		Err: errors.New("dial tcp 10.0.0.1:6443: i/o timeout")})

	out := plainView(m)
	if !strings.Contains(out, "unreachable") || !strings.Contains(out, "i/o timeout") {
		t.Errorf("a cluster that could not be read must say so, and why:\n%s", out)
	}
	if !strings.Contains(out, "1 unreachable") {
		t.Errorf("the header must qualify the totals:\n%s", out)
	}
}

func TestEnterGoesToTheClusterAndNothingElse(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")
	answer(m, ready("prod-eu", true, fleetApp("payments", application.Down, 0, 3)))
	answer(m, ready("staging", false))

	// The first row is the first configured cluster.
	press(t, m, "enter")
	if m.Context() != "staging" {
		t.Fatalf("Enter on a cluster row must switch to it, context = %q", m.Context())
	}
	if m.view == viewFleet {
		t.Error("acting always happens inside one cluster, never in the overview")
	}
}

func TestEnterOnABrokenApplicationOpensItInItsCluster(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")
	answer(m, ready("prod-eu", true, fleetApp("payments", application.Down, 0, 3)))
	answer(m, ready("staging", false))

	// Past the two cluster rows, onto the application.
	press(t, m, "down")
	press(t, m, "down")
	press(t, m, "enter")

	if m.Context() != "prod-eu" {
		t.Fatalf("context = %q, want the cluster the application is broken in", m.Context())
	}
	if m.pendingApplication != "payments" {
		t.Errorf("the application must be opened once its cluster answers, pending = %q",
			m.pendingApplication)
	}

	// Once that cluster's dashboard arrives, the application opens.
	loadApplicationsInto(m, testApplication("payments", application.Down, 0, 3))
	if m.view != viewApplication {
		t.Errorf("view = %v, want the application that was chosen", m.view)
	}
}

func TestLeavingTheFleetStopsReadingIt(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")
	if m.cancelFleet == nil {
		t.Fatal("opening the fleet must start a read")
	}

	press(t, m, "F")
	if m.cancelFleet != nil {
		t.Error("nine requests per cluster must not stay in flight for a screen nobody is on")
	}
}

func TestEveryContextCanBeAddedButOnlyOnPurpose(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "F")

	if len(m.fleetContexts()) != 0 {
		t.Fatal("the fleet starts empty")
	}
	m.includeEveryContext()

	if len(m.fleetContexts()) != len(m.kubeconfig.Contexts) {
		t.Errorf("covering %d contexts, want all %d", len(m.fleetContexts()), len(m.kubeconfig.Contexts))
	}
	if out := plainView(m); !strings.Contains(out, "for this session") {
		t.Errorf("adding every cluster must be stated plainly:\n%s", out)
	}
}

func TestAContextThatLeftTheKubeconfigIsDropped(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config = config.Default()
		o.Config.Fleet = []string{"staging", "a-cluster-that-was-removed"}
	})

	contexts := m.fleetContexts()
	if len(contexts) != 1 || contexts[0] != "staging" {
		t.Errorf("contexts = %v, want only the one that still exists", contexts)
	}
}

func TestNamedFleetGroupsCanBeSwitchedWithoutMixingContexts(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config = config.Default()
		o.Config.FleetGroups = []config.FleetGroup{
			{Name: "production", Contexts: []string{"prod-eu"}},
			{Name: "non-prod", Contexts: []string{"staging"}},
		}
	})
	press(t, m, "F")
	if got := m.fleetContexts(); len(got) != 1 || got[0] != "prod-eu" {
		t.Fatalf("initial group contexts = %v", got)
	}
	if out := plainView(m); !strings.Contains(out, "Fleet / production") {
		t.Errorf("active group must be visible:\n%s", out)
	}

	m.fleetExtra = append(m.fleetExtra, "staging")
	m.switchFleetGroup("non-prod")
	if got := m.fleetContexts(); len(got) != 1 || got[0] != "staging" {
		t.Fatalf("switched group contexts = %v", got)
	}
	if len(m.fleetExtra) != 0 {
		t.Error("session additions from one group must not leak into another")
	}
}

func TestTheTopLevelFleetListStaysReachableBesideNamedGroups(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config = config.Default()
		o.Config.Fleet = []string{"prod-eu"}
		o.Config.FleetGroups = []config.FleetGroup{
			{Name: "non-prod", Contexts: []string{"staging"}},
		}
	})
	press(t, m, "F")

	// A configuration that predates named groups opens on the clusters it
	// always did, not on whichever group happens to be listed first.
	if got := m.fleetContexts(); len(got) != 1 || got[0] != "prod-eu" {
		t.Fatalf("initial contexts = %v", got)
	}

	m.switchFleetGroup("non-prod")
	if got := m.fleetContexts(); len(got) != 1 || got[0] != "staging" {
		t.Fatalf("switched contexts = %v", got)
	}

	// And back: without an implicit group the top-level list would be
	// stranded, because nothing could name it again.
	m.switchFleetGroup("default")
	if got := m.fleetContexts(); len(got) != 1 || got[0] != "prod-eu" {
		t.Fatalf("contexts after returning to the default group = %v", got)
	}
}

// answerPart delivers one cluster's page of a resource.
func answerPart(m *Model, source string, table *resources.Table, err error) {
	m.Update(fleetPartMsg{gen: m.fleetGeneration, part: resources.Part{
		Source: source, Table: table, Err: err,
	}})
}

func podPage(names ...string) *resources.Table {
	t := &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Status", Type: "string"},
		},
		Remaining: -1,
	}
	for _, name := range names {
		t.Rows = append(t.Rows, resources.Row{
			Name: name, Namespace: "shop", Cells: []string{name, "Running"},
		})
	}
	return t
}

func TestAKindCanBeBrowsedAcrossTheWholeFleet(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")

	m.openFleetResourceByName("pods")
	if m.view != viewFleetResource {
		t.Fatalf("view = %v, want the fleet's resource table", m.view)
	}
	if out := plainView(m); !strings.Contains(out, "Reading pods from 2 clusters") {
		t.Errorf("the read must be visible while it runs:\n%s", out)
	}

	answerPart(m, "prod-eu", podPage("payments-1"), nil)
	answerPart(m, "staging", podPage("payments-2"), nil)

	out := plainView(m)
	for _, want := range []string{"CLUSTER", "NAMESPACE", "prod-eu", "staging", "payments-1", "payments-2"} {
		if !strings.Contains(out, want) {
			t.Errorf("the merged table must contain %q:\n%s", want, out)
		}
	}
}

func TestAClusterThatCannotListTheKindIsNamed(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")
	m.openFleetResourceByName("widgets")

	answerPart(m, "prod-eu", podPage("widget-1"), nil)
	answerPart(m, "staging", nil, errors.New("the server could not find the requested resource"))

	out := plainView(m)
	if !strings.Contains(out, "widget-1") {
		t.Errorf("the cluster that answered must still be shown:\n%s", out)
	}
	if !strings.Contains(out, "not listed in staging") {
		t.Errorf("a cluster that does not serve the kind must be named:\n%s", out)
	}
}

func TestEnterOnAFleetRowGoesToThatClusterAndObject(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")
	m.openFleetResourceByName("pods")
	answerPart(m, "prod-eu", podPage("payments-1"), nil)

	press(t, m, "enter")
	if m.Context() != "prod-eu" {
		t.Fatalf("context = %q, want the cluster the row came from", m.Context())
	}
	if m.pendingObject.Name != "payments-1" || m.pendingObject.Kind != "Pod" {
		t.Errorf("pending = %+v, want the object under the cursor", m.pendingObject)
	}

	// Once that cluster's dashboard answers, the object opens.
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	if m.view != viewObject || m.objectTarget.Name != "payments-1" {
		t.Errorf("view = %v target = %+v, want the object open", m.view, m.objectTarget)
	}
}

func TestEscapeReturnsFromTheMergedTableToTheFleet(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")
	m.openFleetResourceByName("pods")

	press(t, m, "esc")
	if m.view != viewFleet {
		t.Errorf("view = %v, want the fleet overview", m.view)
	}
}

func nodeMember(context string, nodes ...application.Node) fleet.Member {
	m := ready(context, false)
	m.Nodes = nodes
	return m
}

func brokenNode(name string) application.Node {
	return application.Node{
		Meta:    application.Meta{Kind: "Node", Name: name},
		Ready:   false,
		Reason:  "KubeletNotReady",
		Message: "container runtime is down",
	}
}

func TestABrokenNodeIsVisibleEvenWhenEveryApplicationIsFine(t *testing.T) {
	// The gap this closes: a cluster whose applications are all healthy while
	// one of its machines is not.
	m := fleetModel(t, "staging", "prod-eu")
	press(t, m, "F")

	healthy := nodeMember("staging",
		application.Node{Meta: application.Meta{Kind: "Node", Name: "node-1"}, Ready: true})
	healthy.Applications = []application.Application{fleetApp("api", application.Healthy, 2, 2)}
	answer(m, healthy)

	broken := nodeMember("prod-eu", brokenNode("node-2"))
	broken.Production = true
	broken.Applications = []application.Application{fleetApp("api", application.Healthy, 2, 2)}
	answer(m, broken)

	out := plainView(m)
	if !strings.Contains(out, "NODES") {
		t.Fatalf("the machines need their own section:\n%s", out)
	}
	for _, want := range []string{"node-2", "not ready", "container runtime is down", "1 of 1 nodes not ready"} {
		if !strings.Contains(out, want) {
			t.Errorf("the broken node must be named, with the reason, missing %q:\n%s", want, out)
		}
	}
	// Nothing is wrong with any application; the fleet must still say so.
	if !strings.Contains(out, "nodes not ready") {
		t.Errorf("the header must carry the machines too:\n%s", out)
	}
}

func TestNodesThatCannotBeListedAreNotSilence(t *testing.T) {
	m := fleetModel(t, "staging")
	press(t, m, "F")

	member := ready("staging", false, fleetApp("api", application.Healthy, 1, 1))
	member.NodesErr = errors.New("nodes is forbidden")
	answer(m, member)

	if out := plainView(m); !strings.Contains(out, "nodes not readable") {
		t.Errorf("a permission Correlux does not have must be said, not omitted:\n%s", out)
	}
}

func TestEnterOnANodeOpensItInItsCluster(t *testing.T) {
	m := fleetModel(t, "staging", "prod-eu")
	loadCatalogInto(m, testCatalog())
	press(t, m, "F")
	answer(m, ready("staging", false))
	answer(m, nodeMember("prod-eu", brokenNode("node-2")))

	// Two cluster rows, then the node.
	press(t, m, "down")
	press(t, m, "down")
	press(t, m, "enter")

	if m.Context() != "prod-eu" {
		t.Fatalf("context = %q, want the cluster the node belongs to", m.Context())
	}
	if m.pendingObject.Kind != "Node" || m.pendingObject.Name != "node-2" {
		t.Errorf("pending = %+v, want the node under the cursor", m.pendingObject)
	}
}

func TestEverythingOddAboutAClusterIsOnTheDefaultView(t *testing.T) {
	// One node down, one cordoned, one kind unreadable: three separate facts,
	// and showing only the worst is how the other two are found too late.
	m := fleetModel(t, "prod-eu")
	press(t, m, "F")

	cordoned := application.Node{Meta: application.Meta{Kind: "Node", Name: "node-3"}, Ready: true}
	cordoned.Unschedulable = true

	member := ready("prod-eu", true, fleetApp("api", application.Healthy, 2, 2))
	member.Nodes = []application.Node{
		{Meta: application.Meta{Kind: "Node", Name: "node-1"}, Ready: true},
		brokenNode("node-2"),
		cordoned,
	}
	member.Gaps = []application.Gap{{Kind: "Ingress", Reason: "not permitted for this user"}}
	answer(m, member)

	out := plainView(m)
	for _, want := range []string{
		"1 of 3 nodes not ready", // the failure
		"1 cordoned",             // and the thing that is merely unusual
		"kind(s) unreadable",     // and what could not be read at all
		"node-3",                 // the cordoned machine is named
		"cordoned",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the default view must show %q:\n%s", want, out)
		}
	}
}

func TestACordonedNodeAloneStillMarksTheCluster(t *testing.T) {
	m := fleetModel(t, "sandbox")
	press(t, m, "F")

	cordoned := application.Node{Meta: application.Meta{Kind: "Node", Name: "node-1"}, Ready: true}
	cordoned.Unschedulable = true
	member := ready("sandbox", false, fleetApp("api", application.Healthy, 1, 1))
	member.Nodes = []application.Node{cordoned}
	answer(m, member)

	out := plainView(m)
	if strings.Contains(out, "nothing broken") {
		t.Errorf("a cordoned node is not nothing:\n%s", out)
	}
	if !strings.Contains(out, "cordoned") {
		t.Errorf("it must be named on the cluster row:\n%s", out)
	}
}
