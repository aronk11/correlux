package components

import (
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// HintGroup orders the status bar. The bar reads left to right as the eye
// travels out from the screen in front of you: what these arrow keys do here,
// then what else this screen offers, then how to go somewhere else, then the
// session itself.
//
// It is separate from Priority because the two questions are different. A
// group says where a hint belongs in the sentence; Priority says what to give
// up when the sentence does not fit. "Quit" is last in the reading order and
// also the first thing worth dropping, but "↑↓ Rows" is first in the reading
// order and among the first to go — the arrow keys are the one thing nobody
// has to be told.
type HintGroup int

const (
	// HintNavigate is what the cursor keys, Enter and Esc do on this screen.
	HintNavigate HintGroup = iota
	// HintView is what else can be done to what is on screen.
	HintView
	// HintScope is how to look at something else: cluster, namespace, kind,
	// filter.
	HintScope
	// HintSession is the program rather than the cluster: refresh, commands,
	// help, quit.
	HintSession
)

// KeyHint is one "key — action" pair in the status bar.
type KeyHint struct {
	Key  string
	Desc string
	// Group decides where the hint reads in the bar. The zero value is
	// HintNavigate, which is where an unlabelled hint about this screen
	// belongs anyway.
	Group HintGroup
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
	// Filter is the text a list is being narrowed by, shown while it is in
	// force. A list showing a fraction of its rows must say so somewhere the
	// eye lands.
	Filter string
	// FilterNote says how much is shown of how much: "12 of 4213 loaded rows".
	FilterNote string
	// FilterFocused draws the cursor in the filter, because the next keystroke
	// will land there.
	FilterFocused bool
	// Hints are the keys available in the current view.
	Hints []KeyHint
}

// RenderStatus draws the bottom bar.
//
// The hints are grouped before anything else, so the bar always reads in the
// same order however the caller assembled it: this screen's keys, then what
// else it offers, then how to leave it, then the session. Callers append their
// view-specific hints to a shared list and do not have to interleave them by
// hand — which is how "Filter" ended up printed after "Quit".
//
// When they do not fit, the least important are dropped rather than the
// rightmost: a narrow terminal should lose "Quit" before it loses "Help". The
// survivors keep the grouped order, so the bar does not reshuffle as the
// window is resized.
func RenderStatus(t *theme.Theme, d StatusData, width int) string {
	if d.Message != "" {
		return pad(t.Badge(d.MessageStatus, truncate(d.Message, max(width-2, 1))), width)
	}

	lead := ""
	if d.Filter != "" || d.FilterFocused {
		lead = renderFilter(t, d)
	}
	remaining := max(width-lipgloss.Width(lead), 0)
	return pad(lead+renderHints(t, fit(order(d.Hints), remaining), remaining), width)
}

// order drops duplicate keys and sorts what is left into reading order.
//
// A key appears once. Views prepend their own wording for a shared key — the
// refresh key is "Measure again" on the usage screen and "Reload" on the fleet
// — and the general list still carries the plain "Refresh" behind it; printing
// both put the same keystroke on the bar twice, describing two things. The
// first wins, which is the view's, because a hint that names what the key does
// on this screen beats one that names what it does in general.
//
// The sort is stable, so hints in the same group keep the order the view
// listed them in: within a group that order is the view's own judgement.
func order(hints []KeyHint) []KeyHint {
	out := make([]KeyHint, 0, len(hints))
	seen := make(map[string]bool, len(hints))
	for _, h := range hints {
		if seen[h.Key] {
			continue
		}
		seen[h.Key] = true
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Group < out[j].Group })
	return out
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

// renderFilter draws the filter and what it is showing, leading the bar.
func renderFilter(t *theme.Theme, d StatusData) string {
	text := d.Filter
	if d.FilterFocused {
		text += t.Glyphs.Selected
	}
	out := t.InputPrompt.Render("/") + t.Emphasis.Render(text)
	if d.FilterNote != "" {
		out += t.Muted.Render("  " + d.FilterNote)
	}
	return out + t.Muted.Render("   ")
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
