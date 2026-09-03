package app

import (
	"strings"
	"testing"
)

func TestExecWithNothingSelectedSaysSo(t *testing.T) {
	m := newTestModel(t)

	cmd := press(t, m, "x")
	if cmd == nil {
		t.Fatal("pressing x with nothing selected must still return a command, to show the notice")
	}
	if out := plainView(m); !strings.Contains(out, "Select a pod or a workload") {
		t.Errorf("the refusal must say what to do instead:\n%s", out)
	}
	if m.overlay != overlayNone {
		t.Errorf("a refusal must not open an overlay, got %v", m.overlay)
	}
}

func TestExecOpensDirectlyOutsideProduction(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	press(t, m, "down") // onto the first pod

	cmd := press(t, m, "x")
	if m.overlay != overlayNone {
		t.Errorf("a non-production shell must open without a confirmation, overlay = %v", m.overlay)
	}
	if cmd == nil {
		t.Fatal("x must hand off to tea.Exec, which is a command")
	}
}

func TestExecInProductionStatesTheTargetBeforeOpening(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	press(t, m, "down") // onto the first pod

	press(t, m, "x")
	if m.overlay != overlayConfirm {
		t.Fatalf("a production shell must be confirmed first, overlay = %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{"Shell in Pod/payments-7d8f-0", "interactive shell", "production", "prod-eu"} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, out)
		}
	}

	// Enter alone does nothing while the challenge is unanswered.
	press(t, m, "enter")
	if m.overlay != overlayConfirm {
		t.Error("the confirmation must stay open until the cluster name is typed")
	}

	m.confirmInput.SetValue("prod-eu")
	cmd := press(t, m, "enter")
	if m.overlay == overlayConfirm {
		t.Error("with the challenge answered, the shell must be allowed to open")
	}
	if cmd == nil {
		t.Fatal("the confirmed action must hand off to tea.Exec")
	}
}

func TestExecTargetsTheObjectInHandFromTheInspector(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.OpenObjectForTest("Pod", "payments-7d8f-0", "default")
	loadObjectInto(m, podObject("payments-7d8f-0", "payments-7d8f"))

	target, title, ok := m.execTarget()
	if !ok {
		t.Fatal("an open Pod must be a valid exec target")
	}
	if target.Namespace != "default" || target.Pod != "payments-7d8f-0" {
		t.Errorf("target = %+v, want the open pod", target)
	}
	if !strings.Contains(title, "payments-7d8f-0") {
		t.Errorf("title = %q, want it to name the pod", title)
	}
}

func TestExecOffersNoTargetOnAKindThatIsNotAPodOrAWorkload(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.OpenObjectForTest("ConfigMap", "flags", "default")

	if _, _, ok := m.execTarget(); ok {
		t.Error("a ConfigMap owns no pods; there is nothing to open a shell in")
	}
}
