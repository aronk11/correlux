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
