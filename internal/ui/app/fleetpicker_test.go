package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/config"
)

// withConfigFile points the model at a writable configuration file, which is
// what the picker saves into.
func withConfigFile(t *testing.T) func(*Options) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	return func(o *Options) {
		o.Config = config.Default()
		o.Config.SourcePath = path
	}
}

// TestTheFleetIsAssembledOnScreenAndSaved is the whole feature: somebody who
// has never opened the configuration file picks clusters, and they are still
// there tomorrow.
func TestTheFleetIsAssembledOnScreenAndSaved(t *testing.T) {
	m := newTestModel(t, withConfigFile(t))
	press(t, m, "F")
	press(t, m, "e")

	if m.overlay != overlayFleetPicker {
		t.Fatalf("the edit key must open the picker, got overlay %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{"prod-eu", "staging", "sandbox", "[ ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("the picker must offer every context with a box: %q missing\n%s", want, out)
		}
	}

	// Tick the row under the cursor, move on, tick another.
	press(t, m, "tab")
	press(t, m, "down")
	press(t, m, "tab")
	if out := plainView(m); !strings.Contains(out, "[x]") {
		t.Errorf("a picked cluster must look picked:\n%s", out)
	}

	press(t, m, "enter")
	if m.overlay != overlayNone {
		t.Fatalf("saving must close the picker, got overlay %v", m.overlay)
	}

	// It reached the file, not just the session.
	saved, err := config.Load(m.cfg.SourcePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(saved.Fleet) != 2 {
		t.Fatalf("saved fleet = %v, want the two that were picked", saved.Fleet)
	}
	if len(m.fleetContexts()) != 2 {
		t.Errorf("the fleet on screen = %v, want the two that were picked", m.fleetContexts())
	}
}

// TestOneKeyTakesEveryClusterAndGivesThemBack covers the "all of them" case,
// and the way out of it: an operator with four clusters wants all four, and
// the same key undoes it the moment they decide otherwise.
func TestOneKeyTakesEveryClusterAndGivesThemBack(t *testing.T) {
	m := newTestModel(t, withConfigFile(t))
	press(t, m, "F")
	press(t, m, "e")

	press(t, m, "ctrl+t")
	for _, c := range m.kubeconfig.Contexts {
		if !m.fleetDraft[c.Name] {
			t.Fatalf("%s was left out of an all-clusters selection", c.Name)
		}
	}

	press(t, m, "ctrl+t")
	for _, c := range m.kubeconfig.Contexts {
		if m.fleetDraft[c.Name] {
			t.Fatalf("%s survived clearing the selection", c.Name)
		}
	}
}

// TestEscapingThePickerChangesNothing. The picker writes to a file the user may
// have written by hand; it must only do so when they say so.
func TestEscapingThePickerChangesNothing(t *testing.T) {
	m := newTestModel(t, withConfigFile(t))
	press(t, m, "F")
	press(t, m, "e")
	press(t, m, "ctrl+t")
	press(t, m, "esc")

	if m.overlay != overlayNone {
		t.Fatalf("Esc must close the picker, got overlay %v", m.overlay)
	}
	if len(m.fleetContexts()) != 0 {
		t.Errorf("a cancelled selection was applied anyway: %v", m.fleetContexts())
	}
	if cfg, err := config.Load(m.cfg.SourcePath); err != nil || len(cfg.Fleet) != 0 {
		t.Errorf("a cancelled selection was written to the file: %v (%v)", cfg.Fleet, err)
	}
}

// TestANamedGroupIsCreatedAndDeletedWithoutTouchingAFile covers grouping: the
// second half of the request, and the half a flat list of ticks cannot answer.
func TestANamedGroupIsCreatedAndDeletedWithoutTouchingAFile(t *testing.T) {
	m := newTestModel(t, withConfigFile(t))

	m.promptNewFleetGroup()
	m.promptInput.SetValue("production")
	m.Update(keyMsg("enter"))
	if m.overlay != overlayFleetPicker {
		t.Fatalf("naming a group must lead straight to picking its clusters, got %v", m.overlay)
	}
	if m.fleetDraftGroup != "production" {
		t.Fatalf("editing group %q, want production", m.fleetDraftGroup)
	}

	press(t, m, "ctrl+t")
	press(t, m, "enter")

	saved, err := config.Load(m.cfg.SourcePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(saved.FleetGroups) != 1 || saved.FleetGroups[0].Name != "production" ||
		len(saved.FleetGroups[0].Contexts) != len(m.kubeconfig.Contexts) {
		t.Fatalf("saved groups = %#v", saved.FleetGroups)
	}
	if m.activeFleetGroup != "production" {
		t.Errorf("the group just built should be the one on screen, got %q", m.activeFleetGroup)
	}

	m.deleteFleetGroup("production")
	if saved, err := config.Load(m.cfg.SourcePath); err != nil || len(saved.FleetGroups) != 0 {
		t.Errorf("groups after delete = %#v (%v)", saved.FleetGroups, err)
	}
}

// TestAGroupCannotShadowAnotherOne. Two groups with the same name make the
// switcher ambiguous and the saved file lossy.
func TestAGroupCannotShadowAnotherOne(t *testing.T) {
	m := newTestModel(t, withConfigFile(t))
	m.cfg.FleetGroups = []config.FleetGroup{{Name: "production", Contexts: []string{"prod-eu"}}}

	m.promptNewFleetGroup()
	m.promptInput.SetValue("Production")
	m.Update(keyMsg("enter"))

	if m.overlay != overlayPrompt {
		t.Fatalf("a duplicate name must not be accepted, overlay = %v", m.overlay)
	}
	if !strings.Contains(m.promptError, "already a group") {
		t.Errorf("promptError = %q, want it to say why", m.promptError)
	}
}
