package app

import (
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/palette"
)

// nodeObject is a node document as the server would return it.
func nodeObject(name string, unschedulable bool) *resources.Object {
	spec := "{}"
	if unschedulable {
		spec = `{"unschedulable":true}`
	}
	return &resources.Object{
		Target: resources.Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}},
		Kind:   "Node",
		Name:   name,
		Raw:    []byte(`{"apiVersion":"v1","kind":"Node","metadata":{"name":"` + name + `"},"spec":` + spec + `}`),
		YAML:   "apiVersion: v1\nkind: Node\nmetadata:\n  name: " + name + "\n",
	}
}

// openNode opens a machine in the inspector, the way the fleet view does.
func openNode(t *testing.T, m *Model, name string, unschedulable bool) {
	t.Helper()
	loadCatalogInto(m, testCatalog())
	m.openObject(objectRef{Kind: "Node", Name: name, Resource: "nodes"})
	loadObjectInto(m, nodeObject(name, unschedulable))
	if m.view != viewObject {
		t.Fatalf("expected the object view, got %v", m.view)
	}
}

// nodeTablePage is what the server's own printer returns for nodes.
func nodeTablePage(names ...string) *resources.Table {
	table := &resources.Table{
		Columns: []resources.Column{
			{Name: "Name", Type: "string"},
			{Name: "Status", Type: "string"},
		},
		Remaining: -1,
	}
	for _, name := range names {
		table.Rows = append(table.Rows, resources.Row{Name: name, Cells: []string{name, "Ready"}})
	}
	return table
}

func paletteEntry(m *Model, id string) (palette.Command, bool) {
	for _, c := range m.registry.Commands() {
		if c.ID == id {
			return c, true
		}
	}
	return palette.Command{}, false
}

func TestCordoningANodeStatesWhatItStopsAndWhere(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", false)

	press(t, m, "C")
	if m.overlay != overlayConfirm {
		t.Fatalf("a change must be confirmed, overlay = %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{
		"Cordon Node/node-1",
		"This stops new pods from being scheduled onto node-1.",
		"already running there",
		"staging",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, out)
		}
	}
}

func TestACordonedNodeIsOfferedTheOtherHalfOfTheAction(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", true)

	if got := m.cordonTitle(objectRef{Kind: "Node", Name: "node-1", Resource: "nodes"}); got != "Uncordon node-1" {
		t.Errorf("title = %q; a cordoned node must not be offered a cordon", got)
	}

	press(t, m, "C")
	out := plainView(m)
	if !strings.Contains(out, "Uncordon Node/node-1") {
		t.Errorf("the action must be the one that is left to do:\n%s", out)
	}
	if !strings.Contains(out, "place pods on node-1 again") {
		t.Errorf("the confirmation must say what it lets happen:\n%s", out)
	}
}

func TestTheCountOfPodsThatStayIsGivenOnlyWhenItIsTheWholeTruth(t *testing.T) {
	m := newTestModel(t)
	// Cluster-wide, so the snapshot sees every pod on the machine rather than
	// one namespace's share of them.
	m.setAllNamespaces(true)
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{Snapshot: application.Snapshot{Pods: []application.Pod{
			{Meta: application.Meta{Kind: "Pod", Name: "payments-0"}, Node: "node-1"},
			{Meta: application.Meta{Kind: "Pod", Name: "payments-1"}, Node: "node-1"},
			{Meta: application.Meta{Kind: "Pod", Name: "worker-0"}, Node: "node-2"},
		}}},
	})
	openNode(t, m, "node-1", false)

	press(t, m, "C")
	if out := plainView(m); !strings.Contains(out, "The 2 pods already running there keep running.") {
		t.Errorf("a known count must be stated:\n%s", out)
	}
}

func TestAStateCorreluxHasNotReadIsNotClaimed(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("nodes")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: nodeTablePage("node-1", "node-2")})

	press(t, m, "C")
	if m.overlay != overlayConfirm {
		t.Fatalf("a node under the cursor in a table can be cordoned, overlay = %v", m.overlay)
	}
	out := plainView(m)
	if !strings.Contains(out, "Cordon Node/node-1") {
		t.Errorf("the row under the cursor is what is acted on:\n%s", out)
	}
	if !strings.Contains(out, "cordoned already has not been read") {
		t.Errorf("an unread state must be admitted rather than guessed:\n%s", out)
	}
	if !strings.Contains(out, "Pods already running there are not touched.") {
		t.Errorf("without a count, the promise must still be made:\n%s", out)
	}
}

func TestProductionDemandsTheClusterNameBeforeANodeIsCordoned(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	openNode(t, m, "node-1", false)

	press(t, m, "C")
	out := plainView(m)
	if !strings.Contains(out, "production") || !strings.Contains(out, "prod-eu") {
		t.Errorf("a production change must demand more than Enter:\n%s", out)
	}

	press(t, m, "enter")
	if m.overlay != overlayConfirm {
		t.Error("the confirmation must stay open until the cluster name is typed")
	}

	m.confirmInput.SetValue("prod-eu")
	press(t, m, "enter")
	if m.overlay == overlayConfirm {
		t.Error("with the challenge answered, the change must be allowed to run")
	}
}

func TestEscapeAbandonsACordon(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", false)

	press(t, m, "C")
	press(t, m, "esc")
	if m.overlay != overlayNone || m.pending != nil {
		t.Errorf("esc must abandon the change, overlay = %v pending = %+v", m.overlay, m.pending)
	}
}

func TestOnlyANodeCanBeCordoned(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openObject(objectRef{Kind: "Pod", Name: "payments-7d8f-0", Namespace: "default"})
	loadObjectInto(m, podObject("payments-7d8f-0", "payments-7d8f"))

	press(t, m, "C")
	if m.overlay != overlayNone || m.pending != nil {
		t.Fatalf("a pod is not a machine and nothing may be prepared for it, overlay = %v", m.overlay)
	}
	if out := plainView(m); !strings.Contains(out, "is not one") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
}

func TestThePaletteSaysWhyCordonIsUnavailable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openObject(objectRef{Kind: "Pod", Name: "payments-7d8f-0", Namespace: "default"})

	entry, ok := paletteEntry(m, "cmd.cordon")
	if !ok {
		t.Fatal("the entry must be listed where an object is in hand")
	}
	if entry.Enabled {
		t.Error("cordon must not be runnable on a pod")
	}
	if entry.DisabledReason == "" {
		t.Error("a disabled entry that does not say why is a dead end")
	}

	openNode(t, m, "node-1", false)
	entry, ok = paletteEntry(m, "cmd.cordon")
	if !ok || !entry.Enabled {
		t.Fatalf("with a node in hand the entry must be runnable: %+v", entry)
	}
	if entry.Title != "Cordon node-1" {
		t.Errorf("title = %q, want the action it will perform", entry.Title)
	}
}

func TestNothingIsOfferedWhereNoObjectIsInHand(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	if _, ok := paletteEntry(m, "cmd.cordon"); ok {
		t.Error("the dashboard has no node in hand; the entry would be noise")
	}
	press(t, m, "C")
	if m.overlay != overlayNone {
		t.Fatalf("nothing may be prepared, overlay = %v", m.overlay)
	}
	if !strings.Contains(plainView(m), "Select a node to cordon") {
		t.Errorf("the key must say what it needs:\n%s", plainView(m))
	}
}

func TestARefusedCordonReadsAsAPermissionProblem(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", false)

	m.Update(cordonedMsg{
		ref:           objectRef{Kind: "Node", Name: "node-1", Resource: "nodes"},
		unschedulable: true,
		err: apierrors.NewForbidden(
			schema.GroupResource{Resource: "nodes"}, "node-1",
			errors.New("User \"reader\" cannot patch resource \"nodes\" at the cluster scope")),
	})

	out := plainView(m)
	if !strings.Contains(out, "not permitted to change nodes") {
		t.Errorf("a denied change must read as one:\n%s", out)
	}
	if strings.Contains(out, "cannot patch resource") {
		t.Errorf("the client-go sentence is not an error message:\n%s", out)
	}
}

func TestACordonThatWorkedIsReportedAndTheScreenReloaded(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", false)
	ref := objectRef{Kind: "Node", Name: "node-1", Resource: "nodes"}

	if cmd := m.applyCordoned(cordonedMsg{ref: ref, unschedulable: true}); cmd == nil {
		t.Error("the node on screen must be read again so the new state is visible")
	}
	if !strings.Contains(m.message, "Cordoned node-1") {
		t.Errorf("message = %q, want what happened", m.message)
	}
	if !m.objectLoading {
		t.Error("the open node must be reloaded")
	}

	m.applyCordoned(cordonedMsg{ref: ref, unschedulable: false})
	if !strings.Contains(m.message, "Uncordoned node-1") {
		t.Errorf("message = %q, want the other half named as itself", m.message)
	}
}
