// Package app is Correlux's Bubble Tea application: the only place where user
// input, asynchronous Kubernetes work and rendering meet.
//
// The rules that keep it maintainable as the product grows:
//   - No package below app knows about Bubble Tea.
//   - No Kubernetes call happens outside a tea.Cmd, and every one of them is
//     cancellable and tagged with a generation so stale answers are discarded.
//   - Update never blocks. If something takes time, it becomes a command.
package app

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Action names used both for key bindings and for palette entries, so a key and
// a typed command always run exactly the same code.
const (
	ActionQuit            = "quit"
	ActionHelp            = "help"
	ActionPalette         = "palette"
	ActionContextPicker   = "context.picker"
	ActionNamespacePicker = "namespace.picker"
	ActionResourcePicker  = "resource.picker"
	ActionApplications    = "applications"
	ActionFleet           = "fleet"
	ActionSearch          = "search"
	ActionWhy             = "why"
	ActionGrouping        = "application.grouping"
	ActionYAML            = "object.yaml"
	ActionDecode          = "object.decode"
	ActionScale           = "scale"
	ActionCordon          = "node.cordon"
	ActionDelete          = "delete"
	ActionRestart         = "workload.restart"
	ActionEdit            = "edit"
	ActionExec            = "exec"
	ActionCopy            = "copy"
	ActionLogs            = "logs"
	ActionUsage           = "usage"
	ActionActivity        = "activity"
	ActionFollow          = "logs.follow"
	ActionTimestamps      = "logs.timestamps"
	ActionPrevious        = "logs.previous"
	ActionOverview        = "overview"
	ActionToggleWide      = "table.wide"
	ActionSort            = "table.sort"
	ActionAllNamespaces   = "namespace.all"
	ActionRefresh         = "refresh"
	ActionAutoRefresh     = "refresh.auto"
	ActionReloadKubeconfi = "kubeconfig.reload"
	ActionClose           = "close"
)

// KeyMap maps keystrokes to actions. Users may override any of it from the
// config file; unknown action names are reported rather than ignored.
type KeyMap struct {
	bindings map[string]string // keystroke -> action
	keys     map[string]string // action -> primary keystroke (for display)
}

// DefaultBindings is the built-in keyboard layout.
//
// The choices avoid keystrokes terminals eat (ctrl+s/ctrl+q flow control,
// ctrl+z suspend) and avoid single letters for anything destructive.
var DefaultBindings = map[string]string{
	ActionQuit:            "ctrl+c",
	ActionHelp:            "?",
	ActionPalette:         "ctrl+p",
	ActionContextPicker:   "ctrl+k",
	ActionNamespacePicker: "ctrl+o",
	ActionResourcePicker:  "ctrl+b",
	ActionApplications:    "ctrl+a",
	ActionFleet:           "F",
	ActionSearch:          "/",
	ActionWhy:             "ctrl+w",
	ActionGrouping:        "r",
	ActionYAML:            "y",
	ActionDecode:          "b",
	ActionScale:           "S",
	ActionCordon:          "C",
	ActionDelete:          "D",
	ActionRestart:         "R",
	ActionEdit:            "e",
	ActionExec:            "x",
	ActionCopy:            "c",
	ActionLogs:            "l",
	ActionUsage:           "u",
	ActionActivity:        "E",
	ActionFollow:          "f",
	ActionTimestamps:      "t",
	ActionPrevious:        "p",
	ActionToggleWide:      "w",
	ActionSort:            "s",
	ActionRefresh:         "ctrl+r",
	ActionAutoRefresh:     "ctrl+f",
	ActionClose:           "esc",
}

// secondaryBindings are additional keystrokes for the same actions. They are
// accepted but not advertised, so muscle memory from other tools works.
var secondaryBindings = map[string]string{
	"q":      ActionQuit,
	"ctrl+/": ActionHelp,
}

// NewKeyMap builds a key map from the defaults plus user overrides
// (action -> keystroke). It returns the names of unknown actions so the caller
// can tell the user their config has a typo instead of silently dropping it.
func NewKeyMap(overrides map[string]string) (KeyMap, []string) {
	km := KeyMap{
		bindings: make(map[string]string, len(DefaultBindings)+len(secondaryBindings)),
		keys:     make(map[string]string, len(DefaultBindings)),
	}
	for action, key := range DefaultBindings {
		km.bindings[key] = action
		km.keys[action] = key
	}
	for key, action := range secondaryBindings {
		km.bindings[key] = action
	}

	var unknown []string
	for action, key := range overrides {
		if _, ok := DefaultBindings[action]; !ok {
			unknown = append(unknown, action)
			continue
		}
		if old, ok := km.keys[action]; ok {
			delete(km.bindings, old)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			// An empty binding unbinds the action.
			delete(km.keys, action)
			continue
		}
		km.bindings[key] = action
		km.keys[action] = key
	}
	sort.Strings(unknown)
	return km, unknown
}

// Action returns the action bound to a keystroke, if any.
func (k KeyMap) Action(keystroke string) (string, bool) {
	a, ok := k.bindings[keystroke]
	if !ok {
		a, ok = k.bindings[normalizeShiftedLetter(keystroke)]
	}
	return a, ok
}

// normalizeShiftedLetter turns "shift+e" into "E".
//
// A terminal that cannot report modifiers separately from the character
// typed sends a shifted letter as the letter itself — "E" — which is what
// DefaultBindings is written in. One that can (the Kitty keyboard protocol,
// the Windows Console API) sends the base key and the modifier apart, and
// [Key.Keystroke] renders that as "shift+e": correct, but a different string
// from the same keystroke on a plainer terminal, and one DefaultBindings
// never matches. Shift+E and Shift+F doing nothing on such a terminal was
// this, not a missing binding.
func normalizeShiftedLetter(keystroke string) string {
	rest, ok := strings.CutPrefix(keystroke, "shift+")
	if !ok {
		return keystroke
	}
	r, size := utf8.DecodeRuneInString(rest)
	if size != len(rest) || !unicode.IsLower(r) {
		return keystroke
	}
	return string(unicode.ToUpper(r))
}

// Key returns the primary keystroke for an action, for display in help.
func (k KeyMap) Key(action string) string {
	if key, ok := k.keys[action]; ok {
		return prettyKey(key)
	}
	return ""
}

// keyPress builds the event a terminal would send for a keystroke like
// "ctrl+p", "enter" or "a".
//
// It lives here rather than in a test file because the integration suite drives
// the real application through it: a test that reaches past the keyboard proves
// less than one that presses the key.
func keyPress(keystroke string) tea.KeyPressMsg {
	key := tea.Key{}
	rest := keystroke
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+"):
			key.Mod |= tea.ModCtrl
			rest = strings.TrimPrefix(rest, "ctrl+")
			continue
		case strings.HasPrefix(rest, "alt+"):
			key.Mod |= tea.ModAlt
			rest = strings.TrimPrefix(rest, "alt+")
			continue
		case strings.HasPrefix(rest, "shift+"):
			key.Mod |= tea.ModShift
			rest = strings.TrimPrefix(rest, "shift+")
			continue
		}
		break
	}

	if code, ok := namedKeys[rest]; ok {
		key.Code = code
		return tea.KeyPressMsg(key)
	}
	r, _ := utf8.DecodeRuneInString(rest)
	key.Code = r
	if key.Mod == 0 {
		key.Text = rest
	}
	return tea.KeyPressMsg(key)
}

// namedKeys are the keys that are not a character.
var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"space":     tea.KeySpace,
}

// displayKeys spells the keys that are not a character the way the keyboard
// does. A key printed as "esc" in the middle of a bar of "Ctrl+K" and "Enter"
// reads as a word somebody forgot to finish.
var displayKeys = map[string]string{
	"esc":       "Esc",
	"enter":     "Enter",
	"tab":       "Tab",
	"space":     "Space",
	"backspace": "Backspace",
	"delete":    "Delete",
	"home":      "Home",
	"end":       "End",
	"pgup":      "PgUp",
	"pgdown":    "PgDn",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
}

var displayModifiers = map[string]string{"ctrl": "Ctrl", "alt": "Alt", "shift": "Shift"}

// prettyKey renders a keystroke the way it is printed on a keyboard. A single
// character is left exactly as it is bound: "u" and "F" are different keys.
func prettyKey(key string) string {
	parts := strings.Split(key, "+")
	last := len(parts) - 1
	for i, part := range parts[:last] {
		if name, ok := displayModifiers[part]; ok {
			parts[i] = name
		}
	}
	switch name, named := displayKeys[parts[last]]; {
	case named:
		parts[last] = name
	case last > 0:
		// The final key of a chord: "ctrl+p" -> "Ctrl+P".
		parts[last] = strings.ToUpper(parts[last])
	}
	return strings.Join(parts, "+")
}
