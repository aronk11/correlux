package app

import (
	"strings"
	"testing"
)

func TestNormalizeShiftedLetter(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"shift+e", "E"},
		{"shift+f", "F"},
		{"shift+s", "S"},
		{"E", "E"},             // already the form DefaultBindings uses
		{"shift+E", "shift+E"}, // not a base-key report; leave alone
		{"ctrl+e", "ctrl+e"},
		{"shift+enter", "shift+enter"}, // a named key, not a single letter
		{"shift+", "shift+"},
	}
	for _, tc := range cases {
		if got := normalizeShiftedLetter(tc.in); got != tc.want {
			t.Errorf("normalizeShiftedLetter(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestShiftedLetterKeystrokesResolveTheSameAction covers every capital-letter
// binding: a terminal that reports modifiers apart from the base key (the
// Kitty keyboard protocol, the Windows Console API) sends "shift+e" for the
// same press a plainer terminal sends as "E", and DefaultBindings is written
// in the latter form.
func TestShiftedLetterKeystrokesResolveTheSameAction(t *testing.T) {
	km, _ := NewKeyMap(nil)
	for _, upper := range []string{"E", "F", "S", "C", "D", "R"} {
		plain, ok := km.Action(upper)
		if !ok {
			t.Fatalf("no action bound to %q in DefaultBindings", upper)
		}
		lower := strings.ToLower(upper)
		shifted, ok := km.Action("shift+" + lower)
		if !ok || shifted != plain {
			t.Errorf("shift+%s = (%q, %v), want (%q, true) to match the plain %q binding",
				lower, shifted, ok, plain, upper)
		}
	}
}

func TestPressingShiftEAndShiftFOpensEventsAndFleet(t *testing.T) {
	m := newTestModel(t)

	press(t, m, "shift+e")
	if m.view != viewActivity {
		t.Errorf("shift+e must open Events the way E does, got view %v", m.view)
	}

	press(t, m, "esc") // back to the dashboard
	press(t, m, "shift+f")
	if m.view != viewFleet {
		t.Errorf("shift+f must open Fleet the way F does, got view %v", m.view)
	}
}

// The destructive actions are bound the same shifted-letter way (ADR: "avoid
// single letters for anything destructive"), so they carried the identical
// bug — Shift+S/C/D/R doing nothing on a terminal that reports modifiers
// apart from the base key. One end-to-end case per action, each reaching the
// confirmation it must ask for before anything is sent.

func TestPressingShiftSAsksToScale(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	press(t, m, "shift+s")
	if m.overlay != overlayPrompt {
		t.Fatalf("shift+s must ask for the count the way S does, overlay = %v", m.overlay)
	}
}

func TestPressingShiftCAsksToCordon(t *testing.T) {
	m := newTestModel(t)
	openNode(t, m, "node-1", false)

	press(t, m, "shift+c")
	if m.overlay != overlayConfirm {
		t.Fatalf("shift+c must confirm the cordon the way C does, overlay = %v", m.overlay)
	}
}

func TestPressingShiftDAsksToDelete(t *testing.T) {
	m := newTestModel(t)
	openCountedWorkload(t, m)

	press(t, m, "shift+d")
	if m.overlay != overlayConfirm {
		t.Fatalf("shift+d must confirm the delete the way D does, overlay = %v", m.overlay)
	}
}

func TestPressingShiftRAsksToRestart(t *testing.T) {
	m := newTestModel(t)
	openDeploymentObject(t, m)

	press(t, m, "shift+r")
	if m.overlay != overlayConfirm {
		t.Fatalf("shift+r must confirm the restart the way R does, overlay = %v", m.overlay)
	}
}
