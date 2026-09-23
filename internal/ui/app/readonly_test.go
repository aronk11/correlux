package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/palette"
)

func readOnlyModel(t *testing.T, safety func(*config.Safety), contextName string) *Model {
	t.Helper()
	m := newTestModel(t, func(o *Options) {
		o.ContextName = contextName
		safety(&o.Config.Safety)
	})
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	return m
}

func TestAReadOnlySessionRefusesChangesAndSaysWhy(t *testing.T) {
	m := readOnlyModel(t, func(s *config.Safety) { s.ReadOnly = true }, "staging")

	for _, key := range []string{"S", "R", "D", "x", "U"} {
		press(t, m, key)
		if m.overlay != overlayNone || m.pending != nil {
			t.Fatalf("%s must not open anything in a read-only session, overlay = %v", key, m.overlay)
		}
		if out := plainView(m); !strings.Contains(out, "Read-only: this session is read-only") {
			t.Errorf("%s must be refused with the reason:\n%s", key, out)
		}
	}
}

func TestReadOnlyIsMarkedInTheHeaderAndTheKeysAreNotAdvertised(t *testing.T) {
	m := readOnlyModel(t, func(s *config.Safety) { s.ReadOnly = true }, "staging")
	m.message = ""

	status := m.statusData()
	for _, h := range status.Hints {
		if h.Key == "S" || h.Key == "D" || h.Key == "R" || h.Key == "x" {
			t.Errorf("a read-only session must not advertise %s %s", h.Key, h.Desc)
		}
	}
	if !m.headerData().ReadOnly {
		t.Error("the header must say the session is read-only")
	}
	if out := plainView(m); !strings.Contains(out, "read-only") {
		t.Errorf("the frame must say read-only in words:\n%s", out)
	}
}

func TestReadOnlyPaletteExplainsInsteadOfHiding(t *testing.T) {
	m := readOnlyModel(t, func(s *config.Safety) { s.ReadOnlyProduction = true }, "prod-eu")

	var scale *palette.Command
	for _, c := range m.registry.Commands() {
		if c.Action == paletteScale {
			scale = &c
			break
		}
	}
	if scale == nil {
		t.Fatal("Scale must stay listed, so the palette can say why it is off")
	}
	if scale.Enabled || !strings.Contains(scale.DisabledReason, "production contexts are read-only") {
		t.Errorf("Scale must be disabled with the reason, got %+v", scale)
	}
	for _, c := range m.registry.Commands() {
		if c.Action == paletteLogs && !c.Enabled {
			t.Error("reading logs changes nothing and must stay available")
		}
	}
}

func TestReadOnlyProductionLeavesOtherContextsAlone(t *testing.T) {
	m := readOnlyModel(t, func(s *config.Safety) { s.ReadOnlyProduction = true }, "staging")

	press(t, m, "S")
	if m.overlay != overlayPrompt {
		t.Fatalf("staging is not production and must still scale, overlay = %v", m.overlay)
	}
}

func TestTheConfirmationGateRefusesEvenWhatForgotToAsk(t *testing.T) {
	m := readOnlyModel(t, func(s *config.Safety) { s.ReadOnly = true }, "staging")
	ran := false
	m.confirm(pendingAction{Title: "anything", Run: func(*Model) tea.Cmd { ran = true; return nil }})
	if m.overlay == overlayConfirm || m.pending != nil {
		t.Fatal("the gate itself must refuse in a read-only context")
	}
	if ran {
		t.Fatal("nothing may run")
	}
}
