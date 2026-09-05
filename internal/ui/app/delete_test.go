package app

import (
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/resources"
)

// countedWorkload is an application whose pods Correlux can actually match to
// its workload: a selector, and pods carrying the labels it selects.
func countedWorkload() application.Application {
	app := testApplication("payments", application.Healthy, 3, 3)
	app.Workloads[0].Selector = map[string]string{"app": "payments"}
	for i := range app.Pods {
		app.Pods[i].Labels = map[string]string{"app": "payments"}
	}
	return app
}

// openCountedWorkload opens an application whose pod count is known, with the
// cursor on its Deployment.
func openCountedWorkload(t *testing.T, m *Model) {
	t.Helper()
	loadCatalogInto(m, scalableCatalog())
	app := countedWorkload()
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{
			Apps:     []application.Application{app},
			Snapshot: application.Snapshot{Workloads: app.Workloads, Pods: app.Pods},
		},
	})
	press(t, m, "enter")
}

// deploymentObject is a Deployment as the server holds it, pod template and
// all.
func deploymentObject() *resources.Object {
	raw := `{"kind":"Deployment","apiVersion":"apps/v1",` +
		`"metadata":{"name":"payments","namespace":"default","uid":"dep-uid","resourceVersion":"40213"},` +
		`"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"payments"}]}}}}`
	return &resources.Object{
		Target: resources.Target{
			GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Namespaced: true,
		},
		Kind:            "Deployment",
		Name:            "payments",
		Namespace:       "default",
		UID:             "dep-uid",
		ResourceVersion: "40213",
		Raw:             []byte(raw),
		YAML:            editableYAML,
	}
}

// openDeploymentObject puts that Deployment on the object screen.
func openDeploymentObject(t *testing.T, m *Model) {
	t.Helper()
	openCountedWorkload(t, m)
	press(t, m, "enter")
	loadObjectInto(m, deploymentObject())
}

func TestDeletingStatesWhatGoesWithTheObject(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)

	press(t, m, "D")
	if m.overlay != overlayConfirm {
		t.Fatalf("a delete must be confirmed before it is sent, overlay = %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{
		"Delete Deployment/payments",
		"This deletes Deployment/payments. Its 3 pods go with it.",
		"Deployment/payments in default",
		"cannot be undone",
		"staging",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, out)
		}
	}
}

func TestDeletingNeverInventsAPodCount(t *testing.T) {
	m := newTestModel(t)
	// The dashboard here knows the workload but nothing that ties pods to it.
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "D")
	out := plainView(m)
	if !strings.Contains(out, "This deletes Deployment/payments.") {
		t.Fatalf("the blast radius must name the object:\n%s", out)
	}
	if strings.Contains(out, "go with it") {
		t.Errorf("Correlux does not know how many pods this owns and must not say:\n%s", out)
	}
}

func TestDeletingWithNothingSelectedSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())

	press(t, m, "D")
	if m.overlay == overlayConfirm {
		t.Fatal("there is no object on the dashboard to delete")
	}
	if out := plainView(m); !strings.Contains(out, "Select an object to delete") {
		t.Errorf("the refusal must say what is missing:\n%s", out)
	}
}

func TestDeletingInProductionDemandsTheClusterName(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	openCountedWorkload(t, m)

	press(t, m, "D")
	if out := plainView(m); !strings.Contains(out, "Type prod-eu") {
		t.Fatalf("a production delete must demand more than Enter:\n%s", out)
	}

	press(t, m, "enter")
	if m.overlay != overlayConfirm || m.pending == nil {
		t.Fatal("Enter alone must not delete anything in production")
	}
	if strings.Contains(m.message, "Deleting") {
		t.Errorf("the delete ran with the challenge unanswered, message = %q", m.message)
	}

	m.confirmInput.SetValue("prod-eu")
	press(t, m, "enter")
	if m.overlay == overlayConfirm {
		t.Error("with the cluster named, the delete must be allowed to run")
	}
	if !strings.Contains(m.message, "Deleting Deployment/payments") {
		t.Errorf("message = %q, want the delete under way", m.message)
	}
}

func TestEscapeAbandonsADelete(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)

	press(t, m, "D")
	press(t, m, "esc")

	if m.overlay != overlayNone || m.pending != nil {
		t.Errorf("esc must abandon the delete, overlay = %v pending = %+v", m.overlay, m.pending)
	}
	if strings.Contains(m.message, "Deleting") {
		t.Errorf("nothing may have been sent, message = %q", m.message)
	}
}

func TestADeletedObjectIsNotLeftOnScreen(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)
	ref := m.objectTarget

	m.Update(deletedMsg{ref: ref})

	if m.view == viewObject {
		t.Error("the object is gone; its document must not stay on screen")
	}
	if !strings.Contains(m.message, "Deleted Deployment/payments") {
		t.Errorf("message = %q, want what was deleted", m.message)
	}
}

func TestARefusedDeleteIsReportedInTermsTheUserCanAct(t *testing.T) {
	deployments := schema.GroupResource{Group: "apps", Resource: "deployments"}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			"not permitted",
			apierrors.NewForbidden(deployments, "payments", errors.New("user cannot delete")),
			"Not permitted to delete Deployment/payments",
		},
		{
			"already gone",
			apierrors.NewNotFound(deployments, "payments"),
			"Deployment/payments is already gone",
		},
		{
			"replaced since it was read",
			apierrors.NewConflict(deployments, "payments", errors.New("uid mismatch")),
			"has been replaced since it was read",
		},
		{
			"anything else",
			errors.New("net/http: TLS handshake timeout"),
			"Could not delete Deployment/payments",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			openDeploymentObject(t, m)

			m.Update(deletedMsg{ref: m.objectTarget, err: tc.err})

			if m.view != viewObject {
				t.Error("nothing was deleted; the object must stay on screen")
			}
			if out := plainView(m); !strings.Contains(out, tc.want) {
				t.Errorf("the failure must read as %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestADeleteIsPinnedToTheObjectThatWasRead(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)

	opts := m.deletePreconditions(m.objectTarget)
	if opts.UID != "dep-uid" {
		t.Errorf("uid precondition = %q, want the uid on screen: a namesake recreated since must survive", opts.UID)
	}
	// A Deployment's status changes on its own; a delete refused because the
	// ready count moved would be a guard nobody could satisfy.
	if opts.ResourceVersion != "" {
		t.Errorf("resourceVersion precondition = %q, want none", opts.ResourceVersion)
	}

	// From a row, with no document read, there is no identity to pin to.
	other := newTestModel(t)
	openCountedWorkload(t, other)
	if opts := other.deletePreconditions(other.objectTarget); opts.UID != "" {
		t.Errorf("uid precondition = %q, want none when no document has been read", opts.UID)
	}
}

func TestTheDeleteCommandIsOfferedForWhatIsOnScreen(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)

	press(t, m, "ctrl+p")
	typeInto(t, m, "delete")

	if !hasTitle(m.cmdPal.Items(), "Delete Deployment/payments") {
		t.Error("the palette must offer the delete for the object the screen points at")
	}
}

// A Status returned by the server carries the reason a delete failed; nothing
// in the classification may depend on the string it happens to use.
func TestDeleteFailureReadsTheReasonNotTheMessage(t *testing.T) {
	err := &apierrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonForbidden,
		Code:    403,
		Message: "deployments.apps \"payments\" is forbidden",
	}}
	ref := objectRef{Kind: "Deployment", Name: "payments", Namespace: "default"}

	text, _ := deleteFailure(ref, err)
	if !strings.Contains(text, "Not permitted") {
		t.Errorf("text = %q, want a refusal a user can act on", text)
	}
}
