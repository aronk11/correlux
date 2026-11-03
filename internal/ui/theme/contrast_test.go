package theme

import (
	"fmt"
	"image/color"
	"math"
	"testing"
)

// The floor is WCAG 2.1 AA for body text. Correlux is read for minutes at a
// time by somebody who is already having a bad day, on whatever terminal their
// employer gave them; a ratio that is "probably fine on my screen" is not a
// standard anybody can check.
const minContrast = 4.5

// TestEverySelectionPairIsLegible checks the colours drawn *inside* a selected
// row against the background that row is drawn on.
//
// This is the pair that goes wrong quietly. Each colour is chosen against the
// terminal's own background, where it reads well; the selection then puts a
// different background behind it, and a muted grey picked for a dark terminal
// lands within a shade of a dark selection.
func TestEverySelectionPairIsLegible(t *testing.T) {
	for _, p := range []struct {
		name string
		p    palette
	}{{"dark", darkPalette()}, {"light", lightPalette()}} {
		for _, pair := range []struct {
			role string
			fg   color.Color
		}{
			{"selectedFG", p.p.selectedFG},
			{"selMuted", p.p.selMuted},
			{"selAccent", p.p.selAccent},
			{"selHealthy", p.p.selHealthy},
			{"selWarn", p.p.selWarn},
			{"selCrit", p.p.selCrit},
		} {
			if got := contrast(pair.fg, p.p.selectedBG); got < minContrast {
				t.Errorf("%s: %s on the selection is %.2f:1, the floor is %.1f:1",
					p.name, pair.role, got, minContrast)
			}
		}
	}
}

// TestTheProductionBadgeIsLegible guards the one other pair Correlux paints
// both halves of. Mistaking the production cluster for a safe one is the most
// expensive misreading the program can cause.
func TestTheProductionBadgeIsLegible(t *testing.T) {
	for _, p := range []struct {
		name string
		p    palette
	}{{"dark", darkPalette()}, {"light", lightPalette()}} {
		if got := contrast(p.p.prodFG, p.p.prodBG); got < minContrast {
			t.Errorf("%s: the production badge is %.2f:1, the floor is %.1f:1",
				p.name, got, minContrast)
		}
	}
}

// contrast is the WCAG 2.1 contrast ratio between two colours.
func contrast(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// relativeLuminance is the WCAG definition: sRGB channels linearised, then
// weighted for how much each contributes to perceived brightness.
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// TestEveryRoleIsLegibleOnCommonBackgrounds checks the colours drawn straight
// onto the terminal's own background. Correlux paints no background of its
// own, so each palette has to hold on the backgrounds people actually run:
// plain white and black, and the popular light and dark schemes.
func TestEveryRoleIsLegibleOnCommonBackgrounds(t *testing.T) {
	hex := func(s string) color.Color {
		var r, g, b uint8
		_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
		return color.RGBA{R: r, G: g, B: b, A: 0xff}
	}
	for _, c := range []struct {
		name        string
		p           palette
		backgrounds []string
	}{
		// white, macOS Terminal, Solarized Light, Catppuccin Latte, GitHub Light
		{"light", lightPalette(), []string{"#ffffff", "#f5f5f5", "#fdf6e3", "#eff1f5", "#f6f8fa"}},
		// black, One Dark, Tokyo Night Storm, Catppuccin Mocha, Dracula
		{"dark", darkPalette(), []string{"#000000", "#282c34", "#24283b", "#1e1e2e", "#282a36"}},
	} {
		for _, bg := range c.backgrounds {
			for _, pair := range []struct {
				role string
				fg   color.Color
			}{
				{"fg", c.p.fg}, {"muted", c.p.muted}, {"accent", c.p.accent},
				{"healthy", c.p.healthy}, {"warn", c.p.warn}, {"crit", c.p.crit},
			} {
				if got := contrast(pair.fg, hex(bg)); got < minContrast {
					t.Errorf("%s: %s on %s is %.2f:1, the floor is %.1f:1", c.name, pair.role, bg, got, minContrast)
				}
			}
		}
	}
}
