package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// truncate shortens s to width display cells, appending an ellipsis when it had
// to cut. It is ANSI- and grapheme-aware, so styled text and CJK/emoji columns
// stay aligned.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
}

// pad right-pads s with spaces to exactly width display cells, truncating when
// it is too long. Used to build fixed-width columns.
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncate(s, width)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// padLeft left-pads s to width display cells (right alignment).
func padLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncate(s, width)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s = strings.Repeat(" ", gap) + s
	}
	return s
}

// fill returns a line of exactly width spaces.
func fill(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", width)
}
