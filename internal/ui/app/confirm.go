package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/diff"

	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// pendingAction is a change to the cluster that is waiting for consent.
//
// Nothing that modifies anything runs without passing through here first, and
// what the user is shown is the *consequence*, not the operation: "this removes
// 2 of 3 replicas" rather than "PATCH /scale" (SPEC 17).
type pendingAction struct {
	Title string
	// Lines spell out what will happen, blast radius first.
	Lines []string
	// Challenge, when set, must be typed exactly before the action can run. It
	// is the context name, because the mistake this guards against is acting on
	// the right object in the wrong cluster.
	Challenge string
	// Diff shows what a change does to a document, when the change is one.
	Diff []diff.Line
	// Danger marks an action worth colouring as such.
	Danger bool
	// Run performs the change once the user has agreed to it.
	Run func(*Model) tea.Cmd
}

// confirm opens the confirmation overlay for an action.
func (m *Model) confirm(action pendingAction) tea.Cmd {
	m.pending = &action
	m.confirmInput.Reset()
	m.overlay = overlayConfirm
	return nil
}

// productionChallenge returns the phrase a user must type before a change is
// applied to a context classified as production, or "" when none is needed.
func (m *Model) productionChallenge() string {
	if !m.cfg.Safety.ProductionConfirmation {
		return ""
	}
	if !m.currentContext().Production {
		return ""
	}
	return m.contextName
}

// runPending applies the pending action, if the user has satisfied its
// challenge.
func (m *Model) runPending() tea.Cmd {
	action := m.pending
	if action == nil {
		m.closeOverlay()
		return nil
	}
	if action.Challenge != "" && strings.TrimSpace(m.confirmInput.Value()) != action.Challenge {
		m.notice("Type "+action.Challenge+" to confirm, or press Esc", theme.StatusWarning)
		return m.expireNotice()
	}

	m.pending = nil
	m.closeOverlay()
	return action.Run(m)
}

// cancelPending abandons the action without running it.
func (m *Model) cancelPending() {
	m.pending = nil
	m.confirmInput.Reset()
	m.closeOverlay()
}

// renderConfirm draws the confirmation overlay.
func (m *Model) renderConfirm(width, height int) string {
	action := m.pending
	if action == nil {
		return ""
	}

	title := m.theme.OverlayTitle
	if action.Danger {
		title = m.theme.Critical.Bold(true)
	}
	var b strings.Builder
	b.WriteString(title.Render(clipTo(action.Title, width, 1)))

	for _, line := range action.Lines {
		b.WriteString("\n" + m.theme.Base.Width(width).Render(line))
	}

	if len(action.Diff) > 0 {
		b.WriteString("\n")
		for _, line := range m.diffLines(action.Diff, width, height) {
			b.WriteString("\n" + line)
		}
	}

	b.WriteString("\n\n" + m.theme.Muted.Render("Cluster") + "  " + m.contextBadge())

	if action.Challenge != "" {
		b.WriteString("\n\n" + m.theme.Warning.Width(width).Render(
			"This context is production. Type "+action.Challenge+" to confirm."))
		b.WriteString("\n" + m.confirmInput.Render(m.theme, width, true))
	}

	b.WriteString("\n\n" + m.theme.Muted.Render("Enter apply   Esc cancel"))
	return clipTo(b.String(), width, height)
}

// diffLines renders the change itself, bounded to what the overlay can show.
// A preview that scrolls off the bottom of a confirmation is a preview nobody
// read.
func (m *Model) diffLines(lines []diff.Line, width, height int) []string {
	budget := max(height-9, 3)
	out := make([]string, 0, budget+1)

	for _, line := range lines {
		if len(out) == budget {
			out = append(out, m.theme.Muted.Render("…"+itoa(len(lines)-budget)+" more lines; press Esc and use "+
				m.keys.Key(ActionYAML)+" to read the whole document"))
			break
		}
		switch line.Op {
		case diff.Add:
			out = append(out, m.theme.Healthy.Render(clipTo("+ "+line.Text, width, 1)))
		case diff.Remove:
			out = append(out, m.theme.Critical.Render(clipTo("- "+line.Text, width, 1)))
		case diff.Keep:
			out = append(out, m.theme.Muted.Render(clipTo("  "+line.Text, width, 1)))
		}
	}
	return out
}

// contextBadge renders the cluster a change would hit, marked when it is
// production — the one fact that must never be missing from a confirmation.
func (m *Model) contextBadge() string {
	if m.currentContext().Production {
		return m.theme.ContextProd.Render(m.theme.Glyphs.Prod+" PROD "+m.contextName) +
			m.theme.Muted.Render("  "+m.scopeLabel())
	}
	return m.theme.Emphasis.Render(m.contextName) + m.theme.Muted.Render("  "+m.scopeLabel())
}

// renderPrompt draws the overlay that asks for a value.
func (m *Model) renderPrompt(width, height int) string {
	var b strings.Builder
	b.WriteString(m.theme.OverlayTitle.Render(clipTo(m.promptTitle, width, 1)))
	if m.promptNote != "" {
		b.WriteString("\n" + m.theme.Muted.Width(width).Render(m.promptNote))
	}
	b.WriteString("\n\n" + m.promptInput.Render(m.theme, width, true))
	if m.promptError != "" {
		b.WriteString("\n" + m.theme.Warning.Width(width).Render(m.promptError))
	}
	b.WriteString("\n\n" + m.theme.Muted.Render("Enter continue   Esc cancel"))
	return clipTo(b.String(), width, height)
}

// newInput builds a single-line input for the overlays above.
func newInput(placeholder string) components.Input {
	return components.Input{Placeholder: placeholder}
}
