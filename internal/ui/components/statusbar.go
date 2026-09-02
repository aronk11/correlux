package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// KeyHint is one "key — action" pair in the status bar.
type KeyHint struct {
	Key  string
	Desc string
	// Priority decides which hints survive a narrow terminal. Higher stays;
	// zero is the least important. Without it the bar drops whatever happens
	// to be furthest right, which is how the help key disappears from an
	// eighty-column window and takes discoverability with it.
	Priority int
}

// StatusData is the status bar's input.
type StatusData struct {
	// Message is a transient notice; it replaces the hints when set.
	Message string
	// MessageStatus colours the message.
	MessageStatus theme.Status
	// Hints are the keys available in the current view.
	Hints []KeyHint
}

// RenderStatus draws the bottom bar.
//
// When the hints do not fit, the least important ones are dropped rather than
// the rightmost: a narrow terminal should lose "Quit" before it loses "Help",
// whatever order the caller listed them in. The surviving hints keep that
// order, so the bar does not reshuffle as the window is resized.
func RenderStatus(t *theme.Theme, d StatusData, width int) string {
	if d.Message != "" {
		return pad(t.Badge(d.MessageStatus, truncate(d.Message, max(width-2, 1))), width)
	}
	return pad(renderHints(t, fit(d.Hints, width), width), width)
}

// separatorWidth is the gap rendered between two hints.
const separatorWidth = 3

// fit drops the lowest-priority hints until the rest fit the width. Ties are
// broken towards the front, which is where the view-specific keys live.
func fit(hints []KeyHint, width int) []KeyHint {
	keep := make([]bool, len(hints))
	total := 0
	for i, h := range hints {
		keep[i] = true
		total += hintWidth(h)
		if i > 0 {
			total += separatorWidth
		}
	}

	for total > width {
		victim := -1
		for i, h := range hints {
			if !keep[i] {
				continue
			}
			if victim < 0 || h.Priority < hints[victim].Priority {
				victim = i
			}
		}
		if victim < 0 {
			break
		}
		keep[victim] = false
		total -= hintWidth(hints[victim]) + separatorWidth
	}

	out := make([]KeyHint, 0, len(hints))
	for i, h := range hints {
		if keep[i] {
			out = append(out, h)
		}
	}
	return out
}

func hintWidth(h KeyHint) int { return lipgloss.Width(h.Key) + 1 + lipgloss.Width(h.Desc) }

func renderHints(t *theme.Theme, hints []KeyHint, width int) string {
	var b strings.Builder
	for _, h := range hints {
		part := t.Key.Render(h.Key) + t.KeyDesc.Render(" "+h.Desc)
		sep := ""
		if b.Len() > 0 {
			sep = t.Muted.Render("   ")
		}
		// A single hint too wide for the terminal is still dropped rather than
		// wrapped: the bar is one line.
		if lipgloss.Width(b.String())+lipgloss.Width(sep)+lipgloss.Width(part) > width {
			break
		}
		b.WriteString(sep)
		b.WriteString(part)
	}
	return b.String()
}
