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

// RenderStatus draws the bottom bar. Hints are dropped from the right when the
// terminal is narrow, so the most important keys survive a small window.
func RenderStatus(t *theme.Theme, d StatusData, width int) string {
	if d.Message != "" {
		return pad(t.Badge(d.MessageStatus, truncate(d.Message, max(width-2, 1))), width)
	}

	var b strings.Builder
	for _, h := range d.Hints {
		part := t.Key.Render(h.Key) + t.KeyDesc.Render(" "+h.Desc)
		sep := ""
		if b.Len() > 0 {
			sep = t.Muted.Render("   ")
		}
		if lipgloss.Width(b.String())+lipgloss.Width(sep)+lipgloss.Width(part) > width {
			break
		}
		b.WriteString(sep)
		b.WriteString(part)
	}
	return pad(b.String(), width)
}
