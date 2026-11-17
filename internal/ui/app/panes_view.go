package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aronk11/correlux/internal/ui/components"
)

// renderPanes draws the body: one view, or several next to each other, each
// under a title that says what it is and whether the keyboard is in it.
func (m *Model) renderPanes() string {
	if !m.splitScreen() {
		return m.renderBody()
	}
	body := m.fullBody
	focused := m.focused()

	if m.solo() {
		// One pane of several still wears its title: it is the only thing on
		// screen saying that the others exist.
		return m.renderPaneTitle(focused, body.Width) + "\n" + m.renderBody()
	}

	layers := make([]*lipgloss.Layer, 0, len(m.panes)+len(m.rules))
	for _, p := range m.panes {
		var block string
		m.withPane(p, func() {
			if p == focused {
				block = m.renderPaneTitle(p, p.frame.Width) + "\n" + m.renderBody()
				return
			}
			// Each pane filters and sorts its own table; the frame cache is
			// the pane's, and so is clearing it.
			defer m.beginFrame()()
			block = m.renderPaneTitle(p, p.frame.Width) + "\n" + m.renderBody()
		})
		layers = append(layers, lipgloss.NewLayer(block).X(p.frame.X-body.X).Y(p.frame.Y-body.Y))
	}
	for _, r := range m.rules {
		line := m.theme.Muted.Render(m.theme.Glyphs.PaneRule)
		rule := strings.TrimSuffix(strings.Repeat(line+"\n", max(r.Height, 0)), "\n")
		layers = append(layers, lipgloss.NewLayer(rule).X(r.X-body.X).Y(r.Y-body.Y))
	}
	canvas := lipgloss.NewCanvas(body.Width, body.Height)
	canvas.Compose(lipgloss.NewCompositor(layers...))
	return canvas.Render()
}

// renderPaneTitle is the one row a pane spends on itself. The focused pane's
// name is in brackets as well as in colour, the way the active navigation
// item is: which pane the next key goes to must be readable without either.
func (m *Model) renderPaneTitle(p *pane, width int) string {
	if width <= 0 {
		return ""
	}
	focused := p == m.focused()
	name := m.paneName(p)
	state := m.paneState(p)

	rule := m.theme.Glyphs.Rule
	left := rule + " "
	label := name
	if focused {
		label = "[" + name + "]"
	}
	// The state gives way before the name does: on a narrow pane, what it
	// shows matters more than how it got there.
	room := width - xansi.StringWidth(left) - 2
	if state != "" && xansi.StringWidth(label)+xansi.StringWidth(state)+3 > room {
		state = ""
	}
	if state != "" {
		room -= xansi.StringWidth(state) + 3
	}
	label = xansi.Truncate(label, max(room, 1), m.theme.Glyphs.Ellipsis)

	var b strings.Builder
	b.WriteString(m.theme.Muted.Render(left))
	if focused {
		b.WriteString(m.theme.SelectedRow.Render(label))
	} else {
		b.WriteString(m.theme.Emphasis.Render(label))
	}
	used := xansi.StringWidth(left) + xansi.StringWidth(label)
	if state != "" {
		b.WriteString(m.theme.Muted.Render(" " + m.theme.Glyphs.Bullet + " " + state))
		used += xansi.StringWidth(state) + 3
	}
	if rest := width - used - 1; rest > 0 {
		b.WriteString(m.theme.Muted.Render(" " + strings.Repeat(rule, rest)))
	} else if width > used {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}

// paneName says what a pane shows.
func (m *Model) paneName(p *pane) string {
	var name string
	m.withPane(p, func() {
		switch {
		case m.follow == followEvents:
			name = "Events " + m.theme.Glyphs.Bullet + " " + m.scopeLabel()
		case m.follow != followNone && m.atFollowRoot():
			name = m.follow.label()
			if m.subjectLabel != "" {
				name += " " + m.theme.Glyphs.Bullet + " " + m.subjectLabel
			}
		default:
			crumbs := m.breadcrumb()
			name = crumbs[len(crumbs)-1]
			if m.view == viewLogs && m.logTitle != "" {
				name = m.logTitle
			}
		}
		if m.filtering() {
			name += " " + m.theme.Glyphs.Bullet + " filter: " + m.query()
		}
	})
	return name
}

// paneState says how a pane relates to the others: following, held, or the
// only one of several being shown.
func (m *Model) paneState(p *pane) string {
	var parts []string
	switch {
	case p.follow == followEvents || p.follow == followNone:
		// Events are the scope's, not the selection's: nothing to follow.
	case p.pinned:
		parts = append(parts, "pinned")
	default:
		parts = append(parts, "follows selection")
	}
	if m.solo() {
		word := "zoomed"
		if m.crowded {
			word = "pane"
		}
		parts = append(parts, word+" "+itoa(m.focus+1)+" of "+itoa(len(m.panes)))
	}
	return strings.Join(parts, ", ")
}

// renderBlank is what a companion draws when the selection gives it nothing
// to show.
func (m *Model) renderBlank(width int) string {
	lines := []string{"", "  " + m.theme.Muted.Render(xansi.Truncate(m.blank, max(width-4, 1), m.theme.Glyphs.Ellipsis))}
	return strings.Join(lines, "\n")
}

// paneHints are the keys a divided screen adds to the status bar.
func (m *Model) paneHints() []components.KeyHint {
	if !m.splitScreen() {
		return []components.KeyHint{
			{Group: components.HintSession, Key: m.keys.Key(ActionSplitRight), Desc: "Split", Priority: 58},
		}
	}
	hints := []components.KeyHint{
		{Group: components.HintNavigate, Key: m.keys.Key(ActionPaneNext), Desc: "Next pane", Priority: 88},
		{Group: components.HintSession, Key: m.keys.Key(ActionPaneZoom), Desc: zoomHint(m.zoomed), Priority: 56},
		{Group: components.HintSession, Key: m.keys.Key(ActionPaneClose), Desc: "Close pane", Priority: 54},
	}
	if f := m.focused().follow; f != followNone && f != followEvents {
		hints = append(hints, components.KeyHint{
			Group: components.HintView, Key: m.keys.Key(ActionPanePin), Desc: pinHint(m.focused().pinned), Priority: 62,
		})
	}
	return hints
}

func zoomHint(zoomed bool) string {
	if zoomed {
		return "Unzoom"
	}
	return "Zoom"
}

func pinHint(pinned bool) string {
	if pinned {
		return "Follow"
	}
	return "Pin"
}
