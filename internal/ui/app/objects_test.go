package app

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/aronk11/kubeui/internal/domain/application"
	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

// loadObjectInto feeds a fetched object to the model the way the runtime would.
func loadObjectInto(m *Model, obj *resources.Object) {
	m.Update(objectLoadedMsg{gen: m.object.Generation(), object: obj})
}

func podObject(name string, owner string) *resources.Object {
	obj := &resources.Object{
		Target:    resources.Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
		Kind:      "Pod",
		Name:      name,
		Namespace: "default",
		UID:       name + "-uid",
		Labels:    map[string]string{"app.kubernetes.io/name": "payments"},
		YAML:      "apiVersion: v1\nkind: Pod\nmetadata:\n  name: " + name + "\nspec:\n  containers:\n  - image: registry/payments:1.4\n",
	}
	if owner != "" {
		obj.Owners = []resources.OwnerRef{{Kind: "ReplicaSet", Name: owner, UID: owner + "-uid", Controller: true}}
	}
	return obj
}

func openDetail(t *testing.T, m *Model) {
	t.Helper()
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "enter")
	if m.view != viewApplication {
		t.Fatalf("expected the application detail, got view %v", m.view)
	}
}

func TestTheDetailViewSelectsItsObjects(t *testing.T) {
	m := newTestModel(t)
	openDetail(t, m)

	_, targets := m.applicationView()
	if len(targets) == 0 {
		t.Fatal("the workloads, pods and services must be selectable")
	}
	if targets[0].Kind != "Deployment" {
		t.Errorf("the first target is %+v, want the workload", targets[0])
	}

	// The cursor starts on the first object and the row is marked as selected.
	if !strings.Contains(plainView(m), "Deployment") {
		t.Error("the workload row must be on screen")
	}

	press(t, m, "down")
	if m.detailPort.Cursor != 1 {
		t.Errorf("down must move the selection, cursor = %d", m.detailPort.Cursor)
	}
	press(t, m, "up")
	press(t, m, "up")
	if m.detailPort.Cursor != 0 {
		t.Errorf("the selection must not run past the first row, cursor = %d", m.detailPort.Cursor)
	}
}

// managedApplication has a row in every section of the detail view: a Flux
// object that can be opened, a service and an ingress, so the order the rows
// are numbered in can be compared with the order they are drawn in.
func managedApplication() application.Application {
	a := testApplication("payments", application.Degraded, 2, 3)
	a.Manager = application.Manager{
		Tool: "Flux", Kind: "Kustomization", Name: "payments", Namespace: "flux-system",
	}
	a.Ingresses = []application.Ingress{{
		Meta:  application.Meta{Kind: "Ingress", Name: "payments", Namespace: "default"},
		Hosts: []string{"payments.example.com"},
	}}
	return a
}

// TestTheTargetsAreNumberedInTheOrderTheyAreDrawn is the rule behind ↓: the
// next target is the next row down the screen. The numbering used to follow the
// order the sections were built in, so the delivery row — built before the
// network section and drawn after it — took the cursor past everything in
// between.
func TestTheTargetsAreNumberedInTheOrderTheyAreDrawn(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, managedApplication())
	press(t, m, "enter")

	data, targets := m.applicationView()
	lines := data.TargetLines(m.screen.Body.Width)
	if len(lines) != len(targets) {
		t.Fatalf("%d rows are drawn for %d targets", len(lines), len(targets))
	}
	if !opensKind(targets, "Kustomization") {
		t.Fatal("the delivery row must be navigable, or this proves nothing")
	}

	previous := -1
	for i := range targets {
		line, drawn := lines[i]
		if !drawn {
			t.Fatalf("target %d, %s, is numbered but never drawn", i, targets[i].label())
		}
		if line <= previous {
			t.Errorf("target %d, %s, is drawn on line %d, above target %d on line %d",
				i, targets[i].label(), line, i-1, previous)
		}
		previous = line
	}
}

func opensKind(targets []objectRef, kind string) bool {
	for _, ref := range targets {
		if ref.Kind == kind {
			return true
		}
	}
	return false
}

func TestEnterOpensTheSelectedObject(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)

	press(t, m, "enter")
	if m.view != viewObject {
		t.Fatalf("Enter must open the object, got view %v", m.view)
	}
	if m.objectTarget.Kind != "Deployment" {
		t.Errorf("opened %+v, want the selected workload", m.objectTarget)
	}
	if out := plainView(m); !strings.Contains(out, "Loading Deployment/payments") {
		t.Errorf("an unfinished load must say so:\n%s", out)
	}
}

func TestTheObjectViewShowsIdentityRelationsAndTheDocument(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)
	press(t, m, "enter")
	loadObjectInto(m, podObject("payments-7d8f-0", "payments-7d8f"))

	out := plainView(m)
	for _, want := range []string{"IDENTITY", "RELATED", "controller", "payments-7d8f", "RECENT EVENTS"} {
		if !strings.Contains(out, want) {
			t.Errorf("the object view must show %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "registry/payments:1.4") {
		t.Error("the document belongs behind the YAML key, not on the details screen")
	}

	press(t, m, "y")
	yaml := plainView(m)
	if !strings.Contains(yaml, "registry/payments:1.4") {
		t.Errorf("y must show the document the server holds:\n%s", yaml)
	}
	press(t, m, "y")
	if strings.Contains(plainView(m), "registry/payments:1.4") {
		t.Error("y must toggle back to the details")
	}
}

func TestFollowingARelationAndWalkingBackOut(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)

	press(t, m, "down")  // past the workload, onto the first pod
	press(t, m, "enter") // open the pod
	if m.objectTarget.Kind != "Pod" {
		t.Fatalf("opened %+v, want the pod", m.objectTarget)
	}
	loadObjectInto(m, podObject(m.objectTarget.Name, "payments-7d8f"))
	press(t, m, "enter") // follow its controller

	if m.objectTarget.Name != "payments-7d8f" {
		t.Fatalf("Enter on a relation must follow it, target = %+v", m.objectTarget)
	}
	if len(m.objectTrail) != 1 {
		t.Errorf("the way back must be remembered, trail = %v", m.objectTrail)
	}

	press(t, m, "esc")
	if !strings.HasPrefix(m.objectTarget.Name, "payments-7d8f-") || m.view != viewObject {
		t.Errorf("Esc must retrace the path, now at %+v in view %v", m.objectTarget, m.view)
	}
	press(t, m, "esc")
	if m.view != viewApplication {
		t.Errorf("Esc from the first object returns to the application, got %v", m.view)
	}
}

func TestChildrenComeFromTheLoadedSnapshot(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	app := brokenApplication()
	// The pods are owned by a ReplicaSet, which is owned by the Deployment.
	rs := application.Meta{Kind: "ReplicaSet", Name: "payments-7d8f", Namespace: "default", UID: "rs-uid"}
	for i := range app.Pods {
		app.Pods[i].Owners = []application.OwnerRef{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: true}}
	}
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{Apps: []application.Application{app}, Snapshot: application.Snapshot{
			Owners: []application.Meta{rs},
			Pods:   app.Pods,
		}},
	})
	press(t, m, "enter")
	press(t, m, "enter")

	// Looking at the ReplicaSet must offer its pods.
	loadObjectInto(m, &resources.Object{
		Target: resources.Target{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true},
		Kind:   "ReplicaSet", Name: "payments-7d8f", Namespace: "default", UID: "rs-uid",
	})

	_, targets := m.objectView()
	pods := 0
	for _, ref := range targets {
		if ref.Kind == "Pod" {
			pods++
		}
	}
	if pods != len(app.Pods) {
		t.Errorf("the ReplicaSet must offer its %d pods, got %d: %+v", len(app.Pods), pods, targets)
	}
}

func TestAnUnknownKindIsReportedRatherThanGuessed(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)

	m.openObject(objectRef{Kind: "Sprocket", Name: "widget-1", Namespace: "default"})
	if out := plainView(m); !strings.Contains(out, "does not serve Sprocket") {
		t.Errorf("a kind the cluster does not serve must be named:\n%s", out)
	}
}

func TestAFailedObjectLoadSaysWhy(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)
	press(t, m, "enter")
	m.Update(objectLoadedMsg{gen: m.object.Generation(), err: errors.New("pods \"payments\" is forbidden")})

	if out := plainView(m); !strings.Contains(out, "forbidden") {
		t.Errorf("a denied read must be stated:\n%s", out)
	}
}

func TestSwitchingScopeClosesTheInspector(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)
	press(t, m, "enter")

	m.switchNamespace("other")
	if m.view != viewApplications || m.objectTarget.Kind != "" {
		t.Errorf("an object from the previous scope must not linger: view %v, target %+v",
			m.view, m.objectTarget)
	}
}

func TestTheObjectViewDescribesWhatTheDocumentSays(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)
	press(t, m, "enter")

	obj := podObject("payments-7d8f-0", "payments-7d8f")
	obj.Raw = []byte(`{
	  "kind": "Pod",
	  "spec": {"nodeName": "node-1", "containers": [
	    {"name": "payments", "image": "registry/payments:1.4",
	     "resources": {"limits": {"memory": "256Mi"}}}]},
	  "status": {"phase": "Running", "qosClass": "Burstable",
	    "containerStatuses": [{"name": "payments", "ready": false, "restartCount": 12,
	      "state": {"waiting": {"reason": "CrashLoopBackOff"}}}],
	    "conditions": [{"type": "Ready", "status": "False", "reason": "ContainersNotReady"}]}
	}`)
	loadObjectInto(m, obj)

	out := plainView(m)
	for _, want := range []string{
		"CONTAINERS",            // the section that answers "what is running"
		"registry/payments:1.4", // and with what
		"waiting: CrashLoopBackOff",
		"memory=256Mi", // the limit that explains an OOM kill
		"CONDITIONS",
		"ContainersNotReady",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the description must contain %q:\n%s", want, out)
		}
	}
}

// smallScreen shrinks the terminal so a list actually overflows it. A resize
// message would be debounced and not yet applied, so the geometry is set the
// way the debouncer eventually would.
func smallScreen(m *Model, width, height int) {
	m.width, m.height = width, height
	m.applyLayout()
}

// openLongApplication opens an application with more pods than fit on screen.
func openLongApplication(t *testing.T, m *Model) {
	t.Helper()
	smallScreen(m, 100, 20)
	loadApplicationsInto(m, testApplication("payments", application.Degraded, 1, 12))
	press(t, m, "enter")
}

func TestScrollingTheApplicationDoesNotSnapBack(t *testing.T) {
	m := newTestModel(t)
	openLongApplication(t, m)

	for i := 0; i < 5; i++ {
		m.Update(wheel(false))
	}
	scrolled := m.detailPort.Offset
	if scrolled == 0 {
		t.Fatal("the wheel must scroll an application that overflows the screen")
	}

	// The bug this guards: the page jumped back to the selection the moment an
	// arrow key was pressed, which reads as "scrolling does not work here".
	press(t, m, "down")
	if m.detailPort.Offset < scrolled {
		t.Errorf("the page jumped back from %d to %d", scrolled, m.detailPort.Offset)
	}
}

func TestScrollingDragsTheSelectionOntoTheScreen(t *testing.T) {
	m := newTestModel(t)
	openLongApplication(t, m)

	data, _ := m.applicationView()
	for i := 0; i < 5; i++ {
		m.Update(wheel(false))
	}

	lines := data.TargetLines(m.screen.Body.Width)
	line, known := lines[m.detailPort.Cursor]
	if !known {
		t.Fatalf("the selection %d is not rendered at all", m.detailPort.Cursor)
	}
	if line < m.detailPort.Offset || line >= m.detailPort.Offset+m.screen.Body.Height {
		t.Errorf("the selection sits on line %d, outside the visible %d..%d",
			line, m.detailPort.Offset, m.detailPort.Offset+m.screen.Body.Height)
	}
}

func TestPageKeysMoveTheApplicationView(t *testing.T) {
	m := newTestModel(t)
	openLongApplication(t, m)

	press(t, m, "pgdown")
	if m.detailPort.Offset == 0 {
		t.Error("PgDn must move the page")
	}
	press(t, m, "pgup")
	if m.detailPort.Offset != 0 {
		t.Errorf("PgUp must come back to the top, offset = %d", m.detailPort.Offset)
	}

	press(t, m, "end")
	atEnd := m.detailPort.Offset
	if atEnd == 0 {
		t.Error("End must reach the bottom")
	}
	press(t, m, "home")
	if m.detailPort.Offset != 0 || m.detailPort.Cursor != 0 {
		t.Errorf("Home must return to the first row, offset = %d cursor = %d", m.detailPort.Offset, m.detailPort.Cursor)
	}
}

func TestEverythingBelowTheLastSelectableRowIsReachable(t *testing.T) {
	m := newTestModel(t)
	openLongApplication(t, m)
	loadEvidenceInto(m, application.Context{})

	// The events sit below the last row that can be selected; pressing down
	// past it must keep moving the page.
	for i := 0; i < 40; i++ {
		press(t, m, "down")
	}
	if out := plainView(m); !strings.Contains(out, "RECENT EVENTS") {
		t.Errorf("the sections below the last selectable row must be reachable:\n%s", out)
	}
}

// secretYAML is what the API server hands back for a Secret: values nobody can
// read without reaching for a second tool.
const secretYAML = `apiVersion: v1
data:
  keystore.jks: G0kgYW0gbm90IHRleHQgYXQgYWxsLCBJIGFtIGJ5dGVzAAAAAAA=
  password: aHVudGVyMg==
  username: cGF5bWVudHM=
kind: Secret
metadata:
  name: database
  namespace: default
type: Opaque
`

const secretJSON = `{"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
  "metadata": {"name": "database", "namespace": "default"},
  "data": {
    "keystore.jks": "G0kgYW0gbm90IHRleHQgYXQgYWxsLCBJIGFtIGJ5dGVzAAAAAAA=",
    "password": "aHVudGVyMg==",
    "username": "cGF5bWVudHM="
  }}`

func secretCatalog() *kubediscovery.Catalog {
	catalog := testCatalog()
	catalog.Resources = append(catalog.Resources, resource("", "v1", "secrets", "Secret", true, true))
	return catalog
}

// openSecret puts a Secret on the object screen, exactly as the server holds it.
func openSecret(t *testing.T, m *Model) {
	t.Helper()
	loadCatalogInto(m, secretCatalog())
	m.openObject(objectRef{Kind: "Secret", Name: "database", Namespace: "default", Resource: "secrets"})
	loadObjectInto(m, secretObject())
}

func secretObject() *resources.Object {
	return &resources.Object{
		Target:          resources.Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Namespaced: true},
		Kind:            "Secret",
		Name:            "database",
		Namespace:       "default",
		UID:             "secret-uid",
		ResourceVersion: "7781",
		Raw:             []byte(secretJSON),
		YAML:            secretYAML,
	}
}

func TestTheEncodedValuesOfASecretAreDecodedOnRequest(t *testing.T) {
	m := newTestModel(t)
	openSecret(t, m)

	press(t, m, "y")
	if out := plainView(m); !strings.Contains(out, "password: aHVudGVyMg==") {
		t.Fatalf("the document must first be shown as the server holds it:\n%s", out)
	}

	press(t, m, "b")
	out := plainView(m)
	for _, want := range []string{"password: hunter2", "username: payments"} {
		if !strings.Contains(out, want) {
			t.Errorf("the decoded document must show %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "aHVudGVyMg==") {
		t.Errorf("a value shown decoded must not also be shown encoded:\n%s", out)
	}
	if !strings.Contains(out, "3 values decoded from base64") {
		t.Errorf("the reader must be told that what they see is decoded:\n%s", out)
	}

	press(t, m, "b")
	if out := plainView(m); !strings.Contains(out, "password: aHVudGVyMg==") {
		t.Errorf("the key must toggle back to what the server stores:\n%s", out)
	}
}

func TestADecodedValueThatIsNotTextIsSummarisedRatherThanDumped(t *testing.T) {
	m := newTestModel(t)
	openSecret(t, m)

	press(t, m, "b")
	out := plainView(m)
	if !strings.Contains(out, "keystore.jks: <binary, 38 bytes>") {
		t.Errorf("bytes nobody can read must be summarised:\n%s", out)
	}
	if strings.Contains(out, "I am not text") {
		// The value starts with an escape byte: written out as it is, it would
		// redraw the screen around itself.
		t.Errorf("the bytes themselves must not reach the terminal:\n%s", out)
	}
}

func TestDecodingReachesForTheDocumentItDecodes(t *testing.T) {
	m := newTestModel(t)
	openSecret(t, m)

	// The details are on screen, and the encoded values are not part of them.
	press(t, m, "b")
	if !m.objectYAML {
		t.Error("asking for the values decoded must show the document that holds them")
	}
}

func TestAnObjectWithNothingToDecodeSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	openDetail(t, m)
	press(t, m, "enter")
	loadObjectInto(m, podObject("payments-7d8f-0", "payments-7d8f"))

	press(t, m, "b")
	if out := plainView(m); !strings.Contains(out, "nothing here is base64") {
		t.Errorf("a key that found nothing to do must say so rather than look broken:\n%s", out)
	}
}
