package app

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeclient "github.com/aronk11/correlux/internal/kube/client"
)

func podTemplate(image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "payments"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: image}}},
	}
}

func testRollbackPlan() *kubeclient.RollbackPlan {
	return &kubeclient.RollbackPlan{
		Namespace: "default", Name: "payments", ResourceVersion: "7",
		CurrentRevision: 14,
		Current:         podTemplate("registry/payments:1.9"),
		Revisions: []kubeclient.DeploymentRevision{
			{Number: 13, ReplicaSet: "payments-13", CreatedAt: time.Now().Add(-48 * time.Hour), Template: podTemplate("registry/payments:1.8")},
			{Number: 12, ReplicaSet: "payments-12", CreatedAt: time.Now().Add(-96 * time.Hour), Template: podTemplate("registry/payments:1.7")},
		},
	}
}

// startRollback presses the key and answers the look-up it starts.
func startRollback(t *testing.T, m *Model, plan *kubeclient.RollbackPlan) {
	t.Helper()
	if cmd := press(t, m, "U"); cmd == nil {
		t.Fatal("U on a Deployment must read its revisions")
	}
	ref, _ := m.targetRef()
	m.Update(rollbackPlannedMsg{gen: m.rollbackGen, ref: ref, plan: plan})
}

func TestRollingBackOffersThePreviousRevisionAndSaysWhatItChangesBack(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	startRollback(t, m, testRollbackPlan())

	if m.overlay != overlayPrompt {
		t.Fatalf("a rollback asks which revision first, overlay = %v", m.overlay)
	}
	if m.promptInput.Value() != "13" {
		t.Errorf("the previous revision is the default, got %q", m.promptInput.Value())
	}
	out := plainView(m)
	for _, want := range []string{"Now at revision 14", "Kept: 13, 12", "registry/payments:1.9 → registry/payments:1.8"} {
		if !strings.Contains(out, want) {
			t.Errorf("the prompt must say %q:\n%s", want, out)
		}
	}

	press(t, m, "enter")
	if m.overlay != overlayConfirm {
		t.Fatalf("a rollback is a change and must be confirmed, overlay = %v", m.overlay)
	}
	confirm := plainView(m)
	for _, want := range []string{"Roll back Deployment/payments", "revision 14 → 13", "image: registry/payments:1.8", "staging"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, confirm)
		}
	}
}

func TestTheRevisionRunningNowIsNotARollback(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	startRollback(t, m, testRollbackPlan())

	m.promptInput.SetValue("14")
	press(t, m, "enter")
	if m.overlay != overlayPrompt || !strings.Contains(plainView(m), "that is the revision running now") {
		t.Errorf("the prompt must stay open and say why:\n%s", plainView(m))
	}

	m.promptInput.SetValue("3")
	press(t, m, "enter")
	if !strings.Contains(plainView(m), "keeps no such revision") {
		t.Errorf("an unknown revision must be named as such:\n%s", plainView(m))
	}
}

func TestADeploymentWithoutHistoryHasNothingToRollBackTo(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	plan := testRollbackPlan()
	plan.Revisions = nil
	startRollback(t, m, plan)

	if m.overlay != overlayNone {
		t.Fatalf("nothing to choose, nothing to open, overlay = %v", m.overlay)
	}
	if !strings.Contains(plainView(m), "no earlier revision") {
		t.Errorf("the refusal must say why:\n%s", plainView(m))
	}
}

func TestOnlyADeploymentIsRolledBack(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "down") // onto a pod
	if cmd := press(t, m, "U"); cmd == nil {
		t.Fatal("the refusal is a notice that expires")
	}
	if m.rollbackGen != 0 {
		t.Error("nothing may be read for a pod")
	}
	if !strings.Contains(plainView(m), "only a Deployment does") {
		t.Errorf("the refusal must say why:\n%s", plainView(m))
	}
}

func TestAStaleAnswerNeverOpensAPrompt(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	press(t, m, "U")
	ref, _ := m.targetRef()
	m.Update(rollbackPlannedMsg{gen: m.rollbackGen - 1, ref: ref, plan: testRollbackPlan()})
	if m.overlay != overlayNone {
		t.Error("an answer to an earlier request must be dropped")
	}
}
