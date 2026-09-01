package components

import "testing"

func typeText(in *Input, s string) {
	for _, r := range s {
		in.HandleKey("", string(r))
	}
}

func TestInputTyping(t *testing.T) {
	var in Input
	typeText(&in, "payments")
	if in.Value() != "payments" {
		t.Errorf("value = %q", in.Value())
	}
	if in.CursorPos() != len("payments") {
		t.Errorf("cursor = %d, want at the end", in.CursorPos())
	}
}

func TestInputBackspaceAndDelete(t *testing.T) {
	var in Input
	typeText(&in, "abc")
	in.HandleKey("backspace", "")
	if in.Value() != "ab" {
		t.Errorf("value = %q, want ab", in.Value())
	}
	in.HandleKey("home", "")
	in.HandleKey("delete", "")
	if in.Value() != "b" {
		t.Errorf("value = %q, want b", in.Value())
	}
}

func TestInputInsertsAtCursor(t *testing.T) {
	var in Input
	typeText(&in, "prod")
	in.HandleKey("home", "")
	typeText(&in, "eu-")
	if in.Value() != "eu-prod" {
		t.Errorf("value = %q, want eu-prod", in.Value())
	}
}

func TestInputWordDelete(t *testing.T) {
	var in Input
	typeText(&in, "kube system pods")
	in.HandleKey("ctrl+w", "")
	if in.Value() != "kube system " {
		t.Errorf("value = %q", in.Value())
	}
}

func TestInputKillLine(t *testing.T) {
	var in Input
	typeText(&in, "abcdef")
	in.HandleKey("home", "")
	in.HandleKey("ctrl+u", "")
	if in.Value() != "abcdef" {
		t.Errorf("ctrl+u at the start must delete nothing, got %q", in.Value())
	}
	in.HandleKey("end", "")
	in.HandleKey("ctrl+u", "")
	if in.Value() != "" {
		t.Errorf("value = %q, want empty", in.Value())
	}
}

func TestCtrlKFallsThroughWhenNothingToKill(t *testing.T) {
	// ctrl+k is the global cluster switcher; inside an input it only kills text
	// when there is text to the right of the cursor, so the shortcut keeps
	// working in the common case.
	var in Input
	typeText(&in, "abc")
	if _, handled := in.HandleKey("ctrl+k", ""); handled {
		t.Error("ctrl+k at the end of the line must fall through to the application")
	}

	in.HandleKey("home", "")
	changed, handled := in.HandleKey("ctrl+k", "")
	if !handled || !changed || in.Value() != "" {
		t.Errorf("ctrl+k with text to the right must kill it, value = %q", in.Value())
	}
}

func TestInputIgnoresControlCharacters(t *testing.T) {
	var in Input
	if changed, _ := in.HandleKey("enter", "\r"); changed {
		t.Error("a control character must not be inserted as text")
	}
	if in.Value() != "" {
		t.Errorf("value = %q, want empty", in.Value())
	}
}

func TestInputHandlesMultiByteRunes(t *testing.T) {
	var in Input
	typeText(&in, "störung")
	in.HandleKey("backspace", "")
	if in.Value() != "störun" {
		t.Errorf("value = %q — backspace must remove a rune, not a byte", in.Value())
	}
}

func TestInputCursorClampedAtBoundaries(t *testing.T) {
	var in Input
	in.HandleKey("left", "")
	in.HandleKey("backspace", "")
	typeText(&in, "x")
	in.HandleKey("right", "")
	in.HandleKey("right", "")
	if in.CursorPos() != 1 {
		t.Errorf("cursor = %d, want clamped to 1", in.CursorPos())
	}
}
