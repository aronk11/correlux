package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// maxPanes is how many panes one screen holds. A fifth would have to take its
// rows from the four already there, and none of them has any to spare on a
// terminal somebody actually owns.
const maxPanes = 4

// splitIndependent is the picker's entry for a pane that is navigated by
// hand; the others name what a companion shows.
const splitIndependent = "independent"

// focused is the pane the keyboard is talking to.
func (m *Model) focused() *pane { return m.panes[m.focus] }

// split reports whether the body is shared by more than one pane.
func (m *Model) splitScreen() bool { return len(m.panes) > 1 }

// solo reports whether only one pane is drawn, because there is only one, the
// user zoomed into it, or the terminal has no room for the others.
func (m *Model) solo() bool { return !m.splitScreen() || m.zoomed || m.crowded }

// layoutPanes gives every pane its rectangle. A pane that is not drawn keeps
// the whole body to measure against: its scroll position must still mean
// something when it comes back.
func (m *Model) layoutPanes() {
	body := m.fullBody
	if !m.splitScreen() {
		m.crowded, m.rules = false, nil
		m.panes[0].frame, m.panes[0].body = body, body
		m.activate(m.focused())
		return
	}
	plan := layout.Panes(body, len(m.panes), m.split)
	if !plan.OK {
		// The direction somebody asked for is a preference. Where it does
		// not fit and another one does, showing every pane the other way
		// round beats showing one of them the way they asked.
		for _, alt := range []layout.Split{layout.SplitRight, layout.SplitBelow} {
			if other := layout.Panes(body, len(m.panes), alt); other.OK {
				plan = other
				break
			}
		}
	}
	m.crowded = !plan.OK
	if m.solo() {
		m.rules = nil
		inner := body
		inner.Y++
		inner.Height = max(inner.Height-1, 0)
		for _, p := range m.panes {
			p.frame, p.body = body, inner
		}
		m.activate(m.focused())
		return
	}
	m.rules = plan.Rules
	for i, p := range m.panes {
		p.frame = plan.Frames[i]
		p.body = p.frame
		p.body.Y++
		p.body.Height = max(p.body.Height-1, 0)
	}
	m.activate(m.focused())
}

// visiblePanes are the panes the current frame draws.
func (m *Model) visiblePanes() []*pane {
	if m.solo() {
		return []*pane{m.focused()}
	}
	return m.panes
}

// focusPane moves the keyboard to the pane at index i.
func (m *Model) focusPane(i int) tea.Cmd {
	if i < 0 || i >= len(m.panes) || i == m.focus {
		return nil
	}
	m.focus = i
	if m.focused().follow == followNone {
		m.driverID = m.focused().id
	}
	m.layoutPanes()
	m.rebuildCommands()
	return nil
}

// cyclePane moves the focus to the next pane, or the previous one.
func (m *Model) cyclePane(delta int) tea.Cmd {
	if !m.splitScreen() {
		m.notice("One pane open — "+m.keys.Key(ActionSplitRight)+" splits the screen", theme.StatusUnknown)
		return m.expireNotice()
	}
	n := len(m.panes)
	return m.focusPane(((m.focus+delta)%n + n) % n)
}

// driver is the pane companions take their subject from: the one a person
// last navigated by hand.
func (m *Model) driver() *pane {
	if p := m.paneByID(m.driverID); p != nil && p.follow == followNone {
		for _, q := range m.panes {
			if q == p {
				return p
			}
		}
	}
	for _, p := range m.panes {
		if p.follow == followNone {
			m.driverID = p.id
			return p
		}
	}
	return m.panes[0]
}

// openSplitPicker asks what the new pane should show. The direction is
// decided by the key; the content is the part worth a question.
func (m *Model) openSplitPicker(split layout.Split) tea.Cmd {
	if len(m.panes) >= maxPanes {
		m.notice("Four panes is what one screen holds — close one first ("+m.keys.Key(ActionPaneClose)+")",
			theme.StatusWarning)
		return m.expireNotice()
	}
	m.pendingSplit = split
	m.overlay = overlaySplit
	m.splitPicker.Title = "Open beside " + m.commandSubject()
	if split == layout.SplitBelow {
		m.splitPicker.Title = "Open below " + m.commandSubject()
	}
	m.splitPicker.Footer = "Enter open   Esc cancel"
	m.splitPicker.Reset()
	m.splitPicker.SelectID(m.suggestedCompanion())
	return nil
}

// suggestedCompanion is the entry the picker opens on: the explanation where
// something is broken, the logs where there is something to read, the details
// otherwise.
func (m *Model) suggestedCompanion() string {
	s := m.subjectOf(m.driver())
	switch {
	case s.app != "" && !s.hasRef && s.unhealthy:
		return followWhy.id()
	case s.hasLogs:
		return followLogs.id()
	}
	return followDetails.id()
}

// filterSplitChoices lists what a new pane can show.
func (m *Model) filterSplitChoices(q string) []components.Item {
	s := m.subjectOf(m.driver())
	what := s.label
	if what == "" {
		what = "the selection"
	}
	rows := []components.Item{
		{ID: followLogs.id(), Title: "Logs", Subtitle: "of " + what + ", following the selection"},
		{ID: followWhy.id(), Title: "Why", Subtitle: "the diagnosis of the selected application"},
		{ID: followDetails.id(), Title: "Details", Subtitle: "what " + what + " is made of"},
		{ID: followYAML.id(), Title: "YAML", Subtitle: "the document the server holds"},
		{ID: followEvents.id(), Title: "Events", Subtitle: "recent activity in " + m.scopeLabel()},
		{ID: splitIndependent, Title: "Another view", Subtitle: "a second dashboard to navigate on its own"},
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Title+" "+r.Subtitle), q) {
			out = append(out, r)
		}
	}
	return out
}

// openPane adds a pane showing kind and lays the screen out again.
//
// A companion leaves the focus where it was: the point of opening logs beside
// a list is to keep walking the list. A pane with a mind of its own takes the
// focus, because the next thing anybody does with it is go somewhere.
func (m *Model) openPane(kind followKind, split layout.Split) tea.Cmd {
	if len(m.panes) >= maxPanes {
		m.notice("Four panes is what one screen holds — close one first", theme.StatusWarning)
		return m.expireNotice()
	}
	m.nextPaneID++
	p := newPane(m.nextPaneID)
	p.follow = kind
	p.appSort, p.appGroup = m.focused().appSort, m.focused().appGroup
	m.panes = append(m.panes, p)
	if split != layout.SplitAuto || len(m.panes) == 2 {
		m.split = split
	}
	m.zoomed = false
	m.layoutPanes()

	var cmds []tea.Cmd
	if kind == followNone {
		m.focus = len(m.panes) - 1
		m.driverID = p.id
		m.layoutPanes()
	} else {
		s := m.subjectOf(m.driver())
		cmds = append(cmds, m.syncPane(p, &s))
		m.followKey = m.subjectKey()
	}
	if m.crowded {
		m.notice("No room to show panes next to each other here — "+m.keys.Key(ActionPaneNext)+" switches between them",
			theme.StatusWarning)
		cmds = append(cmds, m.expireNotice())
	}
	m.rebuildCommands()
	return tea.Batch(cmds...)
}

// closePane removes the pane at index i and everything it had open.
func (m *Model) closePane(i int) tea.Cmd {
	if !m.splitScreen() || i < 0 || i >= len(m.panes) {
		return nil
	}
	p := m.panes[i]
	m.inPane(p, func() tea.Cmd {
		m.releasePane()
		return nil
	})
	wasFocused := i == m.focus
	m.panes = append(m.panes[:i:i], m.panes[i+1:]...)
	if m.focus > i || m.focus >= len(m.panes) {
		m.focus--
	}
	m.focus = max(m.focus, 0)
	if wasFocused && p.follow != followNone {
		// Closing a companion goes back to the pane it was a companion of,
		// not to whichever pane happened to be drawn next.
		for j, q := range m.panes {
			if q.id == m.driverID {
				m.focus = j
			}
		}
	}
	if !m.splitScreen() {
		m.zoomed = false
	}
	// Somebody has to be the pane the others follow. If the last one that was
	// navigated by hand is gone, the first one left takes over, on whatever
	// it is showing.
	hasDriver := false
	for _, q := range m.panes {
		hasDriver = hasDriver || q.follow == followNone
	}
	if !hasDriver {
		m.panes[0].release()
	}
	m.pane = m.focused()
	m.layoutPanes()
	m.rebuildCommands()
	return nil
}

// release turns a companion into a pane of its own, on what it shows now.
func (p *pane) release() {
	p.follow, p.pinned, p.blank = followNone, false, ""
	if p.view == viewLogs {
		p.logFrom = viewApplications
	}
	p.objectFrom, p.whyFrom, p.activityFrom = viewApplications, viewApplications, viewApplications
}

// releasePane stops whatever the active pane has in flight.
func (m *Model) releasePane() {
	m.stopLogs()
	if m.cancelTable != nil {
		m.cancelTable()
		m.cancelTable = nil
	}
	if m.cancelObject != nil {
		m.cancelObject()
		m.cancelObject = nil
	}
	m.tableLoading, m.objectLoading, m.loadingMore = false, false, false
}

// closeFocusedPane closes the pane in front.
func (m *Model) closeFocusedPane() tea.Cmd {
	if !m.splitScreen() {
		if len(m.tabs) > 1 {
			// The last pane of a tab is the tab.
			return m.closeTab()
		}
		m.notice("This is the only pane", theme.StatusUnknown)
		return m.expireNotice()
	}
	return m.closePane(m.focus)
}

// closeOtherPanes leaves only the pane in front.
func (m *Model) closeOtherPanes() tea.Cmd {
	keep := m.focused()
	for i := len(m.panes) - 1; i >= 0; i-- {
		if m.panes[i] != keep {
			m.closePane(i)
		}
	}
	return nil
}

// toggleZoom gives the focused pane the whole body, or gives it back.
func (m *Model) toggleZoom() tea.Cmd {
	if !m.splitScreen() {
		return nil
	}
	if m.crowded {
		m.notice("Only one pane fits here; "+m.keys.Key(ActionPaneNext)+" shows the next", theme.StatusUnknown)
		return m.expireNotice()
	}
	m.zoomed = !m.zoomed
	m.layoutPanes()
	m.rebuildCommands()
	return nil
}

// togglePin holds a companion on what it shows, or lets it follow again. It
// acts on the pane in front when that is a companion, and on the only
// companion there is otherwise — with one list and one log on screen, "pin"
// can only mean the log.
func (m *Model) togglePin() tea.Cmd {
	target := m.pinTarget()
	if target == nil {
		if m.splitScreen() {
			m.notice("Move to the pane to pin with "+m.keys.Key(ActionPaneNext), theme.StatusUnknown)
		} else {
			m.notice("Nothing to pin: no pane is following the selection", theme.StatusUnknown)
		}
		return m.expireNotice()
	}
	target.pinned = !target.pinned
	m.rebuildCommands()
	if target.pinned {
		return nil
	}
	// Following again starts with catching up.
	target.subject = ""
	s := m.subjectOf(m.driver())
	return m.syncPane(target, &s)
}

// pinTarget is the pane the pin key means.
func (m *Model) pinTarget() *pane {
	if p := m.focused(); p.follow != followNone && p.follow != followEvents {
		return p
	}
	var only *pane
	for _, p := range m.panes {
		if p.follow == followNone || p.follow == followEvents {
			continue
		}
		if only != nil {
			return nil
		}
		only = p
	}
	return only
}

// paneAt finds the pane drawn at a screen position.
func (m *Model) paneAt(x, y int) (int, bool) {
	if m.solo() {
		if m.focused().frame.Contains(x, y) {
			return m.focus, true
		}
		return 0, false
	}
	for i, p := range m.panes {
		if p.frame.Contains(x, y) {
			return i, true
		}
	}
	return 0, false
}

// openIncident lays the screen out for the application in hand: why it is
// broken, what it is printing and what the cluster did to it, next to the
// list it was picked from. It is the three screens somebody opens one after
// another anyway, opened at once.
func (m *Model) openIncident() tea.Cmd {
	if !m.incidentAvailable() {
		m.notice("Select an application first", theme.StatusWarning)
		return m.expireNotice()
	}
	if m.focused().follow != followNone {
		m.focusPaneOf(m.driver())
	}
	m.closeOtherPanes()

	want := []followKind{followWhy, followLogs, followEvents}
	for len(want) > 0 && !layout.Panes(m.fullBody, len(want)+1, layout.SplitAuto).OK {
		want = want[:len(want)-1]
	}
	if len(want) == 0 {
		m.notice("This terminal is too small for an incident layout", theme.StatusWarning)
		return tea.Batch(m.explain(), m.expireNotice())
	}
	var cmds []tea.Cmd
	for _, kind := range want {
		cmds = append(cmds, m.openPane(kind, layout.SplitAuto))
	}
	return tea.Batch(cmds...)
}

// focusPaneOf moves the focus to p.
func (m *Model) focusPaneOf(p *pane) {
	for i, q := range m.panes {
		if q == p {
			m.focusPane(i)
			return
		}
	}
}

// paneBusy reports whether any pane is reloading what it shows.
func (m *Model) paneBusy() bool {
	for _, p := range m.allPanes() {
		if p.objectLoading || p.tableLoading {
			return true
		}
	}
	return false
}

// paneCommands are the palette's entries for dividing the screen. Everything a
// key does here has a name, so nobody has to learn the key first.
func (m *Model) paneCommands() []palette.Command {
	room := len(m.panes) < maxPanes
	several := m.splitScreen()
	subject := m.subjectOf(m.driver()).label
	cmds := []palette.Command{
		{
			ID: "pane.right", Action: palettePane, Arg: "right",
			Title: "Split: open a pane on the right", Subtitle: "logs, diagnosis, details, YAML or events beside this",
			Category: "Panes", Keywords: []string{"split", "pane", "window", "side by side", "vertical", "tmux", "multi"},
			Shortcut: m.keys.Key(ActionSplitRight), Weight: 66, Enabled: room,
		},
		{
			ID: "pane.below", Action: palettePane, Arg: "below",
			Title: "Split: open a pane below", Subtitle: "the same choice, stacked",
			Category: "Panes", Keywords: []string{"split", "pane", "window", "stack", "horizontal", "tmux", "multi"},
			Shortcut: m.keys.Key(ActionSplitBelow), Weight: 64, Enabled: room,
		},
		{
			ID: "pane.incident", Action: palettePane, Arg: "incident",
			Title: "Incident layout", Subtitle: m.incidentSubtitle(),
			Category: "Panes", Keywords: []string{"incident", "layout", "why", "logs", "events", "triage", "split", "3am"},
			Shortcut: m.keys.Key(ActionIncident), Weight: 68, Enabled: m.incidentAvailable(),
		},
		{
			ID: "pane.next", Action: palettePane, Arg: "next",
			Title: "Move to the next pane", Category: "Panes", Keywords: []string{"pane", "focus", "switch", "next", "window"},
			Shortcut: m.keys.Key(ActionPaneNext), Weight: 62, Enabled: several,
		},
		{
			ID: "pane.zoom", Action: palettePane, Arg: "zoom",
			Title: zoomTitle(m.zoomed), Category: "Panes", Keywords: []string{"pane", "zoom", "maximize", "fullscreen", "full"},
			Shortcut: m.keys.Key(ActionPaneZoom), Weight: 60, Enabled: several && !m.crowded,
		},
		{
			ID: "pane.close", Action: palettePane, Arg: "close",
			Title: "Close this pane", Category: "Panes", Keywords: []string{"pane", "close", "kill", "unsplit"},
			Shortcut: m.keys.Key(ActionPaneClose), Weight: 58, Enabled: several,
		},
		{
			ID: "pane.only", Action: palettePane, Arg: "only",
			Title: "Close every other pane", Category: "Panes", Keywords: []string{"pane", "only", "close", "single", "unsplit"},
			Weight: 56, Enabled: several,
		},
	}
	for _, kind := range []followKind{followLogs, followWhy, followDetails, followYAML, followEvents} {
		of := ""
		if subject != "" && kind != followEvents {
			of = "of " + subject + ", following the selection"
		}
		cmds = append(cmds, palette.Command{
			ID: "pane.beside." + kind.id(), Action: palettePane, Arg: "beside:" + kind.id(),
			Title: "Open " + kind.label() + " in a pane beside this", Subtitle: of,
			Category: "Panes", Keywords: []string{"split", "pane", "beside", "follow", kind.id()},
			Weight: 50, Enabled: room,
		})
	}
	if p := m.pinTarget(); p != nil {
		cmds = append(cmds, palette.Command{
			ID: "pane.pin", Action: palettePane, Arg: "pin",
			Title: pinTitle(p.pinned), Subtitle: m.paneName(p),
			Category: "Panes", Keywords: []string{"pane", "pin", "hold", "freeze", "follow", "unpin"},
			Shortcut: m.keys.Key(ActionPanePin), Weight: 61, Enabled: true,
		})
	}
	return cmds
}

// runPaneCommand runs one of paneCommands.
func (m *Model) runPaneCommand(arg string) tea.Cmd {
	if id, ok := strings.CutPrefix(arg, "beside:"); ok {
		kind, known := followKindOf(id)
		if !known {
			return nil
		}
		return m.openPane(kind, layout.SplitAuto)
	}
	switch arg {
	case "right":
		return m.openSplitPicker(layout.SplitRight)
	case "below":
		return m.openSplitPicker(layout.SplitBelow)
	case "incident":
		return m.openIncident()
	case "next":
		return m.cyclePane(1)
	case "zoom":
		return m.toggleZoom()
	case "close":
		return m.closeFocusedPane()
	case "only":
		return m.closeOtherPanes()
	case "pin":
		return m.togglePin()
	}
	return nil
}

// incidentAvailable reports whether there is an application to lay the
// screen out around.
func (m *Model) incidentAvailable() bool {
	var ok bool
	m.withPane(m.driver(), func() { _, ok = m.explanationTarget() })
	return ok
}

func (m *Model) incidentSubtitle() string {
	var name string
	m.withPane(m.driver(), func() {
		if a, ok := m.explanationTarget(); ok {
			name = a.Name
		}
	})
	if name == "" {
		return "select an application first"
	}
	return "why, logs and events of " + name + ", at once"
}

func zoomTitle(zoomed bool) string {
	if zoomed {
		return "Show every pane again"
	}
	return "Zoom this pane to the whole screen"
}

func pinTitle(pinned bool) string {
	if pinned {
		return "Let the pane follow the selection again"
	}
	return "Pin the pane to what it shows now"
}
