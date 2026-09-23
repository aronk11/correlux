package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// readOnlyReason reports whether changes are refused in the current context,
// and the reason in words that fit on the status line.
//
// The client factory refuses the same requests at the transport, from the same
// configuration; this is the half the user sees, so a refused key says why
// instead of failing at the API server with an error that reads like RBAC.
func (m *Model) readOnlyReason() (string, bool) {
	return m.cfg.Safety.ReadOnlyIn(m.contextName, m.currentContext().Production)
}

// readOnly reports whether the current context refuses changes.
func (m *Model) readOnly() bool {
	_, readOnly := m.readOnlyReason()
	return readOnly
}

// refuseReadOnly answers a request to change the cluster in a read-only
// context: with a notice rather than silence, because a key that does nothing
// reads as a broken key.
func (m *Model) refuseReadOnly() (tea.Cmd, bool) {
	reason, readOnly := m.readOnlyReason()
	if !readOnly {
		return nil, false
	}
	m.notice("Read-only: "+reason+". Nothing here changes the cluster.", theme.StatusWarning)
	return m.expireNotice(), true
}

// writesCluster reports the keys that change a cluster or open a session
// inside one. The edit key is one of them only where it edits an object: in
// the fleet it chooses clusters, which changes nothing but the config file.
func (m *Model) writesCluster(action string) bool {
	switch action {
	case ActionScale, ActionCordon, ActionDelete, ActionRestart, ActionExec, ActionRollback:
		return true
	case ActionEdit:
		return m.view == viewObject
	}
	return false
}

// commandWritesCluster is the same question asked of a palette entry. Reading
// a Helm release, managing the sessions already running and stopping a
// port-forward stay available: none of them changes anything.
func commandWritesCluster(c *palette.Command) bool {
	switch c.Action {
	case paletteScale, paletteCordon, paletteRestart, paletteDelete, paletteEdit, paletteExec, paletteRollback, paletteFlux:
		return true
	case paletteHelm:
		switch c.Arg {
		case "upgrade", "rollback", "test", "uninstall":
			return true
		}
	case paletteDebug:
		return c.Arg != "sessions"
	case paletteForward:
		return c.Arg == "start"
	}
	return false
}

// withReadOnly disables the palette entries a read-only context refuses. They
// stay listed, with the reason, because "where did Scale go?" is a question
// the palette should answer rather than raise.
func (m *Model) withReadOnly(cmds []palette.Command) []palette.Command {
	reason, readOnly := m.readOnlyReason()
	if !readOnly {
		return cmds
	}
	for i := range cmds {
		if commandWritesCluster(&cmds[i]) {
			cmds[i].Enabled = false
			cmds[i].DisabledReason = "read-only: " + reason
		}
	}
	return cmds
}

// withoutWritingHints drops the keys a read-only context refuses from the
// status bar: a hint is a promise about the keystroke.
func (m *Model) withoutWritingHints(hints []components.KeyHint) []components.KeyHint {
	if _, readOnly := m.readOnlyReason(); !readOnly {
		return hints
	}
	refused := map[string]bool{}
	for _, action := range []string{ActionScale, ActionCordon, ActionDelete, ActionRestart, ActionExec, ActionRollback, ActionEdit} {
		if m.writesCluster(action) {
			refused[m.keys.Key(action)] = true
		}
	}
	out := hints[:0]
	for _, h := range hints {
		if h.Group == components.HintView && refused[h.Key] {
			continue
		}
		out = append(out, h)
	}
	return out
}
