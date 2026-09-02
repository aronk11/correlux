// Package theme owns every colour and symbol Correlux draws.
//
// Two rules drive the design:
//   - Information is never encoded in colour alone. Every state has a glyph and
//     a word as well, so Correlux stays readable on monochrome terminals, for
//     colour-blind users, and in a `script`/CI capture.
//   - Terminals that cannot render a symbol get a plain-ASCII substitute rather
//     than a replacement box.
package theme

import (
	"os"
	"runtime"
	"strings"
)

// Glyphs is the symbol set used for status and chrome.
type Glyphs struct {
	Healthy  string
	Warning  string
	Critical string
	Unknown  string
	Pending  string
	Bullet   string
	Selected string
	Arrow    string
	Ellipsis string
	Prompt   string
	Prod     string
	// BarFull and BarEmpty draw a proportion. They are a shape, never a
	// meaning: the number they illustrate is always printed beside them.
	BarFull  string
	BarEmpty string
}

var unicodeGlyphs = Glyphs{
	Healthy:  "✓",
	Warning:  "⚠",
	Critical: "✖",
	Unknown:  "?",
	Pending:  "…",
	Bullet:   "•",
	Selected: "▸",
	Arrow:    "→",
	Ellipsis: "…",
	Prompt:   "❯",
	Prod:     "⬤",
	BarFull:  "█",
	BarEmpty: "░",
}

var asciiGlyphs = Glyphs{
	Healthy:  "OK",
	Warning:  "!",
	Critical: "X",
	Unknown:  "?",
	Pending:  "..",
	Bullet:   "*",
	Selected: ">",
	Arrow:    "->",
	Ellipsis: "...",
	Prompt:   ">",
	Prod:     "#",
	BarFull:  "#",
	BarEmpty: ".",
}

// Env abstracts environment lookup so detection can be tested without
// mutating the process environment.
type Env func(key string) (string, bool)

// OSEnv reads the real process environment.
func OSEnv(key string) (string, bool) { return os.LookupEnv(key) }

// MapEnv builds an Env from a map, for tests.
func MapEnv(m map[string]string) Env {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

func get(env Env, key string) string {
	v, _ := env(key)
	return v
}

// GlyphsFor returns the symbol set appropriate for the environment.
func GlyphsFor(unicodeOK bool) Glyphs {
	if unicodeOK {
		return unicodeGlyphs
	}
	return asciiGlyphs
}

// DetectUnicode reports whether the terminal can be expected to render the
// Unicode glyph set. It errs towards ASCII, because a wrong guess in that
// direction is merely plain, while the other direction is unreadable.
func DetectUnicode(env Env) bool {
	if truthy(get(env, "CORRELUX_ASCII")) {
		return false
	}
	term := get(env, "TERM")
	if term == "dumb" {
		return false
	}
	if runtime.GOOS == "windows" {
		// Windows Terminal and modern hosts render UTF-8 happily; the legacy
		// conhost does not advertise itself, so require a positive marker.
		if _, ok := env("WT_SESSION"); ok {
			return true
		}
		return strings.Contains(strings.ToLower(get(env, "TERM_PROGRAM")), "vscode")
	}
	if term == "" {
		return false
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v, ok := env(key)
		if !ok || v == "" {
			continue
		}
		v = strings.ToUpper(v)
		return strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8")
	}
	return false
}

// DetectColor reports whether colour may be used at all. It honours the
// NO_COLOR convention (https://no-color.org): the variable being present, with
// any value, disables colour.
func DetectColor(env Env) bool {
	if _, ok := env("NO_COLOR"); ok {
		return false
	}
	if get(env, "CLICOLOR") == "0" {
		return false
	}
	if get(env, "TERM") == "dumb" {
		return false
	}
	return true
}

// DetectDark reports whether a dark background should be assumed before the
// terminal has answered a background-colour query. COLORFGBG, when present, is
// authoritative; otherwise dark is the safer default for terminals.
func DetectDark(env Env) bool {
	if v, ok := env("COLORFGBG"); ok {
		parts := strings.Split(v, ";")
		if len(parts) >= 2 {
			switch parts[len(parts)-1] {
			case "7", "15":
				return false
			}
		}
	}
	return true
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
