package app

import (
	"strings"
	"testing"

	"github.com/aronk11/kubeui/internal/config"
	"github.com/aronk11/kubeui/internal/domain/application"
	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
)

// scalableCatalog is a catalog where Deployments declare a scale subresource
// and Pods, correctly, do not.
func scalableCatalog() *kubediscovery.Catalog {
	catalog := testCatalog()
	for i := range catalog.Resources {
		if catalog.Resources[i].Kind() == "Deployment" {
			catalog.Resources[i].Scalable = true
		}
	}
	return catalog
}

// openWorkload opens an application and puts the cursor on its Deployment.
func openWorkload(t *testing.T, m *Model) {
	t.Helper()
	app := testApplication("payments", application.Healthy, 3, 3)
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{
			Apps:     []application.Application{app},
			Snapshot: application.Snapshot{Workloads: app.Workloads},
		},
	})
	press(t, m, "enter")
}

func TestScalingAsksForACountAndStatesWhatItWillDo(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	if m.overlay != overlayPrompt {
		t.Fatalf("S must ask for the count first, overlay = %v", m.overlay)
	}
	out := plainView(m)
	if !strings.Contains(out, "Scale Deployment/payments") || !strings.Contains(out, "Currently 3 replicas") {
		t.Errorf("the prompt must say what it has now:\n%s", out)
	}

	// The consequence appears while typing, before Enter.
	m.promptInput.SetValue("1")
	m.refreshPrompt()
	if !strings.Contains(plainView(m), "removes 2 replicas") {
		t.Errorf("the blast radius must be visible before Enter:\n%s", plainView(m))
	}

	press(t, m, "enter")
	if m.overlay != overlayConfirm {
		t.Fatalf("a change must be confirmed, overlay = %v", m.overlay)
	}
	confirm := plainView(m)
	for _, want := range []string{"Scale Deployment/payments", "removes 2 replicas", "3 replicas → 1 replica", "staging"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("the confirmation must contain %q:\n%s", want, confirm)
		}
	}
}

func TestScalingToZeroIsCalledWhatItIs(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	m.promptInput.SetValue("0")
	press(t, m, "enter")

	if out := plainView(m); !strings.Contains(out, "serve nothing") {
		t.Errorf("scaling to zero must say what it means:\n%s", out)
	}
}

func TestANonsenseCountNeverReachesTheCluster(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	m.promptInput.SetValue("lots")
	press(t, m, "enter")

	if m.overlay != overlayPrompt {
		t.Fatalf("the prompt must stay open, overlay = %v", m.overlay)
	}
	if out := plainView(m); !strings.Contains(out, "not a number") {
		t.Errorf("the prompt must say what is wrong:\n%s", out)
	}
}

func TestAKindWithoutAScaleSubresourceIsNotOffered(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "down") // onto a pod
	press(t, m, "S")
	if m.overlay == overlayPrompt {
		t.Fatal("a pod has no scale subresource and must not be scalable")
	}
	if out := plainView(m); !strings.Contains(out, "cannot be scaled") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
}

func TestProductionDemandsTheClusterNameBeTyped(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	m.promptInput.SetValue("5")
	press(t, m, "enter")

	out := plainView(m)
	if !strings.Contains(out, "production") || !strings.Contains(out, "prod-eu") {
		t.Errorf("a production change must demand more than Enter:\n%s", out)
	}

	// Enter alone does nothing while the challenge is unanswered.
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

func TestProductionConfirmationCanBeTurnedOff(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.ContextName = "prod-eu"
		o.Config.Safety = config.Safety{ProductionConfirmation: false}
	})
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	m.promptInput.SetValue("5")
	press(t, m, "enter")

	if strings.Contains(plainView(m), "Type prod-eu") {
		t.Error("the safety system is configurable, and this configuration turned it off")
	}
}

func TestEscapeAbandonsTheChangeAtEveryStep(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "S")
	press(t, m, "esc")
	if m.overlay != overlayNone {
		t.Fatalf("esc must close the prompt, overlay = %v", m.overlay)
	}

	press(t, m, "S")
	m.promptInput.SetValue("9")
	press(t, m, "enter")
	press(t, m, "esc")
	if m.overlay != overlayNone || m.pending != nil {
		t.Errorf("esc must abandon the pending change, overlay = %v pending = %+v", m.overlay, m.pending)
	}
}
