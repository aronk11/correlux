// Package app is kubeui's Bubble Tea application: the only place where user
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
)

// Action names used both for key bindings and for palette entries, so a key and
// a typed command always run exactly the same code.
const (
	ActionQuit            = "quit"
	ActionHelp            = "help"
	ActionPalette         = "palette"
	ActionContextPicker   = "context.picker"
	ActionNamespacePicker = "namespace.picker"
	ActionAllNamespaces   = "namespace.all"
	ActionRefresh         = "refresh"
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
	ActionRefresh:         "ctrl+r",
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
	return a, ok
}

// Key returns the primary keystroke for an action, for display in help.
func (k KeyMap) Key(action string) string {
	if key, ok := k.keys[action]; ok {
		return prettyKey(key)
	}
	return ""
}

// prettyKey renders a keystroke the way it is printed on a keyboard.
func prettyKey(key string) string {
	replacements := map[string]string{
		"ctrl+":  "Ctrl+",
		"alt+":   "Alt+",
		"shift+": "Shift+",
	}
	out := key
	for from, to := range replacements {
		out = strings.ReplaceAll(out, from, to)
	}
	if len(out) == 1 {
		return out
	}
	// Upper-case the final key of a chord: "Ctrl+p" -> "Ctrl+P".
	if idx := strings.LastIndex(out, "+"); idx >= 0 && idx < len(out)-1 {
		return out[:idx+1] + strings.ToUpper(out[idx+1:])
	}
	return out
}
