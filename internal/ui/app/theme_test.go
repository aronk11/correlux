package app

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestThePaletteFollowsTheTerminalsBackground(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.caps.Dark || m.theme.Caps.Dark {
		t.Fatal("a white background gets the light palette")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if !m.caps.Dark || !m.theme.Caps.Dark {
		t.Error("and a black one the dark palette again")
	}
}

func TestASwitchBetweenLightAndDarkAsksTheTerminalAgain(t *testing.T) {
	// The terminal's background need not follow the system's scheme, so a
	// switch is a reason to ask, not an answer.
	for _, msg := range []tea.Msg{uv.LightColorSchemeEvent{}, uv.DarkColorSchemeEvent{}, tea.FocusMsg{}} {
		m := newTestModel(t)
		dark := m.caps.Dark
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Errorf("%T must ask for the background colour", msg)
		}
		if m.caps.Dark != dark {
			t.Errorf("%T alone must not change the palette", msg)
		}
	}
}
