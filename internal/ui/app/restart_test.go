package app

import (
	"errors"
	"strings"
	"testing"
)

func TestRestartingStatesWhatItReplaces(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)

	press(t, m, "R")
	if m.overlay != overlayConfirm {
		t.Fatalf("a restart must be confirmed before it is sent, overlay = %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{
		"Restart Deployment/payments",
		"This replaces every pod of payments",
		"3 pods are running now.",
		"Deployment/payments in default",
		"staging",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, out)
		}
	}
}

func TestRestartingSomethingWithNoPodTemplateIsRefusedWithTheReason(t *testing.T) {
	m := newTestModel(t)
	openSecret(t, m)

	press(t, m, "R")
	if m.overlay == overlayConfirm {
		t.Fatal("a Secret has no pods; there is nothing to roll and nothing to confirm")
	}
	if out := plainView(m); !strings.Contains(out, "has no pod template") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
}

func TestRestartingFromARowReadsTheDocumentFirst(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)

	// The row is on screen; the document behind it is not, and whether a
	// rollout means anything is written in the document.
	press(t, m, "R")
	if m.overlay == overlayConfirm {
		t.Fatal("the gate must not open before Correlux knows the object carries a pod template")
	}
	if !strings.Contains(m.message, "Reading Deployment/payments") {
		t.Errorf("message = %q, want the look-up under way", m.message)
	}

	m.Update(restartProbedMsg{gen: m.restartGen, ref: m.targetOnScreen(t), object: deploymentObject()})
	if m.overlay != overlayConfirm {
		t.Fatalf("with the document read, the gate must open, overlay = %v", m.overlay)
	}
	if out := plainView(m); !strings.Contains(out, "This replaces every pod of payments") {
		t.Errorf("the confirmation must state the consequence:\n%s", out)
	}
}

func TestAnAnswerForAnObjectNobodyIsPointingAtIsDropped(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)
	press(t, m, "R")

	// The user moved on: the answer belongs to a question that has been
	// superseded, and it must not open a gate on its own.
	m.Update(restartProbedMsg{gen: m.restartGen - 1, ref: m.targetOnScreen(t), object: deploymentObject()})

	if m.overlay == overlayConfirm {
		t.Error("a stale look-up must never open a confirmation")
	}
}

func TestAFailedLookUpForARestartIsReported(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)
	press(t, m, "R")

	m.Update(restartProbedMsg{
		gen: m.restartGen,
		ref: m.targetOnScreen(t),
		err: errors.New("deployments.apps \"payments\" is forbidden"),
	})

	if m.overlay == overlayConfirm {
		t.Fatal("nothing was read; there is nothing to confirm")
	}
	if out := plainView(m); !strings.Contains(out, "Could not read Deployment/payments") {
		t.Errorf("the failure must be reported:\n%s", out)
	}
}

func TestRestartingInProductionDemandsTheClusterName(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	openDeploymentObject(t, m)

	press(t, m, "R")
	if out := plainView(m); !strings.Contains(out, "Type prod-eu") {
		t.Fatalf("rolling production is guarded like every other change:\n%s", out)
	}

	press(t, m, "enter")
	if m.overlay != overlayConfirm || m.pending == nil {
		t.Fatal("Enter alone must not roll a production workload")
	}

	m.confirmInput.SetValue("prod-eu")
	press(t, m, "enter")
	if !strings.Contains(m.message, "Restarting Deployment/payments") {
		t.Errorf("message = %q, want the rollout under way", m.message)
	}
}

func TestARestartIsReportedWhenItLandsAndWhenItDoesNot(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)
	ref := m.objectTarget

	m.Update(restartedMsg{ref: ref})
	if !strings.Contains(m.message, "pods are being replaced") {
		t.Errorf("message = %q, want what the rollout is doing", m.message)
	}

	m.Update(restartedMsg{ref: ref, err: errors.New("etcdserver: request timed out")})
	if !strings.Contains(m.message, "Could not restart Deployment/payments") {
		t.Errorf("message = %q, want the failure named", m.message)
	}
	if m.view != viewObject {
		t.Error("a failed restart changes nothing; the screen must stay where it was")
	}
}

func TestTheRestartCommandIsOfferedOnlyWhereItWouldDoSomething(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)
	press(t, m, "ctrl+p")
	typeInto(t, m, "restart")
	if !hasTitle(m.cmdPal.Items(), "Restart Deployment/payments") {
		t.Error("the palette must offer the rollout for a workload with a pod template")
	}

	secret := newTestModel(t)
	openSecret(t, secret)
	press(t, secret, "ctrl+p")
	typeInto(t, secret, "restart")
	if hasTitle(secret.cmdPal.Items(), "Restart Secret/database") {
		t.Error("the document says this object has no pods; the command must not be offered")
	}
}

// targetOnScreen is what the model would act on right now, so a test can send
// the answer to a look-up the model itself started.
func (m *Model) targetOnScreen(t *testing.T) objectRef {
	t.Helper()
	ref, ok := m.targetRef()
	if !ok {
		t.Fatal("nothing is selected")
	}
	return ref
}
