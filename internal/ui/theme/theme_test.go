package theme

import (
	"runtime"
	"testing"

	"github.com/aronk11/correlux/internal/config"
)

func TestDetectColorHonoursNoColor(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"default", map[string]string{"TERM": "xterm-256color"}, true},
		{"NO_COLOR set to empty still disables", map[string]string{"TERM": "xterm", "NO_COLOR": ""}, false},
		{"NO_COLOR with a value", map[string]string{"NO_COLOR": "1"}, false},
		{"CLICOLOR=0", map[string]string{"CLICOLOR": "0"}, false},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectColor(MapEnv(tc.env)); got != tc.want {
				t.Errorf("DetectColor(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestDetectUnicode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX locale detection does not apply on Windows")
	}
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"utf-8 locale", map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, true},
		{"utf8 spelling", map[string]string{"TERM": "xterm", "LC_ALL": "de_DE.utf8"}, true},
		{"latin-1 locale", map[string]string{"TERM": "xterm", "LANG": "en_US.ISO-8859-1"}, false},
		{"no locale", map[string]string{"TERM": "xterm"}, false},
		{"dumb terminal", map[string]string{"TERM": "dumb", "LANG": "en_US.UTF-8"}, false},
		{"opt out", map[string]string{"TERM": "xterm", "LANG": "en_US.UTF-8", "CORRELUX_ASCII": "1"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectUnicode(MapEnv(tc.env)); got != tc.want {
				t.Errorf("DetectUnicode(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestDetectDarkUsesColorFGBG(t *testing.T) {
	if DetectDark(MapEnv(map[string]string{"COLORFGBG": "0;15"})) {
		t.Error("a light background must be detected from COLORFGBG")
	}
	if !DetectDark(MapEnv(map[string]string{"COLORFGBG": "15;0"})) {
		t.Error("a dark background must be detected from COLORFGBG")
	}
	if !DetectDark(MapEnv(nil)) {
		t.Error("dark is the safer default when nothing is known")
	}
}

func TestGlyphsFallBackToASCII(t *testing.T) {
	ascii := GlyphsFor(false)
	for name, g := range map[string]string{
		"healthy": ascii.Healthy, "warning": ascii.Warning, "critical": ascii.Critical,
		"selected": ascii.Selected, "arrow": ascii.Arrow, "prompt": ascii.Prompt,
	} {
		for _, r := range g {
			if r > 127 {
				t.Errorf("the ascii glyph set must stay ascii: %s = %q", name, g)
				break
			}
		}
	}
}

func TestBadgeAlwaysCarriesTextNotJustColour(t *testing.T) {
	// Colour-blind users, monochrome terminals and piped output must all still
	// convey the state, so the badge is glyph + word by construction.
	th := New(Capabilities{Color: false, Unicode: false, Attributes: false}, config.ThemeAuto)
	badge := th.Badge(StatusCritical, "down")
	if badge != "X down" {
		t.Errorf("badge = %q, want a glyph and a word", badge)
	}
}

func TestPlainModeEmitsNoEscapeSequences(t *testing.T) {
	th := New(Capabilities{Color: false, Attributes: false}, config.ThemeAuto)
	for _, s := range []string{
		th.Critical.Render("boom"),
		th.Emphasis.Render("bold"),
		th.SelectedRow.Render("row"),
		th.Muted.Render("hint"),
	} {
		for _, r := range s {
			if r == 0x1b {
				t.Errorf("plain mode emitted an escape sequence: %q", s)
				break
			}
		}
	}
}

func TestThemePreferenceOverridesDetection(t *testing.T) {
	light := New(Capabilities{Color: true, Attributes: true, Dark: true}, config.ThemeLight)
	if light.Caps.Dark {
		t.Error("an explicit light preference must override detection")
	}
	dark := New(Capabilities{Color: true, Attributes: true, Dark: false}, config.ThemeDark)
	if !dark.Caps.Dark {
		t.Error("an explicit dark preference must override detection")
	}
}

func TestEveryStatusHasAGlyph(t *testing.T) {
	th := New(Capabilities{Color: true, Unicode: true, Attributes: true}, config.ThemeAuto)
	for _, s := range []Status{StatusUnknown, StatusHealthy, StatusWarning, StatusCritical} {
		if th.Glyph(s) == "" {
			t.Errorf("status %d has no glyph", s)
		}
	}
}
