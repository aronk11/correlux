// Package components holds kubeui's reusable terminal widgets.
//
// Components are pure view state: they know how to render themselves and how to
// react to key presses, and nothing about Kubernetes. Anything that needs the
// cluster is passed in as already-computed data.
package components

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// Input is a single-line text field with the editing keys people expect from a
// shell prompt. It is intentionally small: kubeui uses it for filtering, not
// for authoring text.
type Input struct {
	value  []rune
	cursor int
	// Placeholder is shown when the value is empty.
	Placeholder string
}

// Value returns the current text.
func (i *Input) Value() string { return string(i.value) }

// SetValue replaces the text and moves the cursor to the end.
func (i *Input) SetValue(s string) {
	i.value = []rune(s)
	i.cursor = len(i.value)
}

// Reset clears the field.
func (i *Input) Reset() {
	i.value = i.value[:0]
	i.cursor = 0
}

// CursorPos returns the cursor offset in runes.
func (i *Input) CursorPos() int { return i.cursor }

// HandleKey applies a keystroke and reports whether the value changed. Keys the
// input does not own (enter, escape, arrows up/down) are left for the caller.
func (i *Input) HandleKey(keystroke string, text string) (changed bool, handled bool) {
	switch keystroke {
	case "backspace", "ctrl+h":
		if i.cursor > 0 {
			i.value = append(i.value[:i.cursor-1], i.value[i.cursor:]...)
			i.cursor--
			return true, true
		}
		return false, true
	case "delete", "ctrl+d":
		if i.cursor < len(i.value) {
			i.value = append(i.value[:i.cursor], i.value[i.cursor+1:]...)
			return true, true
		}
		return false, true
	case "left", "ctrl+b":
		if i.cursor > 0 {
			i.cursor--
		}
		return false, true
	case "right", "ctrl+f":
		if i.cursor < len(i.value) {
			i.cursor++
		}
		return false, true
	case "home", "ctrl+a":
		i.cursor = 0
		return false, true
	case "end", "ctrl+e":
		i.cursor = len(i.value)
		return false, true
	case "ctrl+u":
		if i.cursor == 0 {
			return false, true
		}
		i.value = i.value[i.cursor:]
		i.cursor = 0
		return true, true
	case "ctrl+k":
		// Only meaningful with text to the right; otherwise let the app use
		// ctrl+k as a global shortcut.
		if i.cursor < len(i.value) {
			i.value = i.value[:i.cursor]
			return true, true
		}
		return false, false
	case "ctrl+w", "alt+backspace":
		start := wordStart(i.value, i.cursor)
		if start == i.cursor {
			return false, true
		}
		i.value = append(i.value[:start], i.value[i.cursor:]...)
		i.cursor = start
		return true, true
	}

	if text != "" && !strings.ContainsFunc(text, isControl) {
		runes := []rune(text)
		rest := append([]rune(nil), i.value[i.cursor:]...)
		i.value = append(append(i.value[:i.cursor], runes...), rest...)
		i.cursor += len(runes)
		return true, true
	}
	return false, false
}

func isControl(r rune) bool { return unicode.IsControl(r) }

func wordStart(value []rune, cursor int) int {
	i := cursor
	for i > 0 && unicode.IsSpace(value[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(value[i-1]) {
		i--
	}
	return i
}

// Render draws the field, truncating from the left so the cursor stays visible.
func (i *Input) Render(t *theme.Theme, width int, focused bool) string {
	prompt := t.InputPrompt.Render(t.Glyphs.Prompt + " ")
	inner := width - lipgloss.Width(prompt)
	if inner < 4 {
		inner = 4
	}

	if len(i.value) == 0 && !focused {
		return prompt + t.Muted.Render(truncate(i.Placeholder, inner))
	}
	if len(i.value) == 0 {
		return prompt + t.Muted.Render(truncate(i.Placeholder, inner-1)) + cursorCell(t, " ")
	}

	// Keep the cursor in view by scrolling the window over the text.
	start := 0
	if i.cursor >= inner {
		start = i.cursor - inner + 1
	}
	end := min(len(i.value), start+inner)
	visible := i.value[start:end]

	var b strings.Builder
	b.WriteString(prompt)
	for idx, r := range visible {
		if start+idx == i.cursor {
			b.WriteString(cursorCell(t, string(r)))
			continue
		}
		b.WriteString(string(r))
	}
	if i.cursor >= end {
		b.WriteString(cursorCell(t, " "))
	}
	return b.String()
}

func cursorCell(t *theme.Theme, s string) string {
	return t.SelectedRow.Render(s)
}
