package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// The fleet is chosen on screen rather than in a text editor.
//
// Naming clusters in a configuration file is the one thing standing between
// somebody installing Correlux and seeing their estate, and it asks them to
// know the shape of a file, the spelling of every context and where the file
// lives — before the program has shown them anything. The picker asks the
// question the other way round: here is your kubeconfig, tick the clusters
// that belong together.
//
// Nothing is ticked by default and nothing is contacted until the fleet is
// opened, which is the same rule as before: Correlux never decides on its own
// to authenticate against every cluster somebody holds credentials for.

// openFleetPicker starts editing one group's membership. An empty name edits
// the plain top-level list, which is the group somebody who has never thought
// about groups is editing.
func (m *Model) openFleetPicker(group string) tea.Cmd {
	m.fleetDraftGroup = group
	m.fleetDraft = map[string]bool{}
	for _, name := range m.groupContexts(group) {
		m.fleetDraft[name] = true
	}
	return m.openOverlay(overlayFleetPicker)
}

// groupContexts is the saved membership of one group.
func (m *Model) groupContexts(group string) []string {
	if group == "" || group == defaultFleetGroup {
		return m.cfg.Fleet
	}
	for _, g := range m.cfg.FleetGroups {
		if g.Name == group {
			return g.Contexts
		}
	}
	return nil
}

// fleetPickerTitle names what is being edited, because "which clusters" is
// only half the question once there is more than one group.
func (m *Model) fleetPickerTitle() string {
	if m.fleetDraftGroup == "" || m.fleetDraftGroup == defaultFleetGroup {
		return "Clusters in the fleet"
	}
	return "Clusters in " + m.fleetDraftGroup
}

// fleetPickerFooter counts as it goes: a list of thirty contexts with six
// ticked somewhere in it is not something anybody should have to scroll to
// audit.
func (m *Model) fleetPickerFooter() string {
	chosen := 0
	for _, on := range m.fleetDraft {
		if on {
			chosen++
		}
	}
	return "Tab pick   Ctrl+T all   Enter save   Esc cancel   " +
		m.theme.Glyphs.Bullet + " " + itoa(chosen) + " of " +
		itoa(len(m.kubeconfig.Contexts)) + " chosen"
}

// filterFleetPicker lists every context in the kubeconfig with its current
// membership. Every context is offered — the point of the screen is to choose
// from all of them — and the ones already in the group are ticked.
func (m *Model) filterFleetPicker(query string) []components.Item {
	contexts := m.kubeconfig.Contexts
	names := make([]string, len(contexts))
	for i, c := range contexts {
		names[i] = c.Name
	}

	order := make([]int, 0, len(contexts))
	highlights := make(map[int][]int, len(contexts))
	if strings.TrimSpace(query) == "" {
		for i := range contexts {
			order = append(order, i)
		}
	} else {
		for _, res := range fuzzy.Find(query, names) {
			order = append(order, res.Index)
			highlights[res.Index] = res.MatchedIndexes
		}
	}

	items := make([]components.Item, 0, len(order))
	for _, idx := range order {
		c := contexts[idx]
		item := components.Item{
			ID:        c.Name,
			Title:     c.Name,
			Subtitle:  c.Server,
			Highlight: highlights[idx],
			Checked:   m.fleetDraft[c.Name],
		}
		if c.Production {
			item.Badge = "PROD"
			item.BadgeStatus = theme.StatusCritical
		}
		if c.Name == m.contextName {
			item.Right = "active"
		}
		items = append(items, item)
	}
	return items
}

// toggleFleetPick flips the row under the cursor.
func (m *Model) toggleFleetPick() {
	item, ok := m.fleetPicker.Selected()
	if !ok {
		return
	}
	m.fleetDraft[item.ID] = !m.fleetDraft[item.ID]
	m.fleetPicker.Footer = m.fleetPickerFooter()
	m.fleetPicker.Refresh()
}

// toggleEveryFleetPick is the "all of them" case, and the way back out of it.
//
// It is one key rather than a select-all and a clear-all, because the state it
// leads to is one somebody either wants or does not: an operator with four
// clusters wants all four, and the moment they decide otherwise the same key
// undoes it.
func (m *Model) toggleEveryFleetPick() {
	wantAll := false
	for _, c := range m.kubeconfig.Contexts {
		if !m.fleetDraft[c.Name] {
			wantAll = true
			break
		}
	}
	for _, c := range m.kubeconfig.Contexts {
		m.fleetDraft[c.Name] = wantAll
	}
	m.fleetPicker.Footer = m.fleetPickerFooter()
	m.fleetPicker.Refresh()
}

// saveFleetPick writes the selection to the configuration file and reopens the
// fleet on it.
//
// It saves rather than holding the choice for the session, because a fleet is
// not a thing anybody wants to reassemble every morning. The file is the same
// one a user may have written by hand, and only the fleet keys in it are
// touched.
func (m *Model) saveFleetPick() tea.Cmd {
	chosen := make([]string, 0, len(m.fleetDraft))
	for name, on := range m.fleetDraft {
		if on {
			chosen = append(chosen, name)
		}
	}
	sort.Strings(chosen)

	group := m.fleetDraftGroup
	if group == defaultFleetGroup {
		group = ""
	}
	fleet, groups := m.cfg.Fleet, append([]config.FleetGroup(nil), m.cfg.FleetGroups...)
	if group == "" {
		fleet = chosen
	} else {
		groups = withGroup(groups, config.FleetGroup{Name: group, Contexts: chosen})
	}

	if err := m.writeFleet(fleet, groups); err != nil {
		m.notice("Could not save the fleet: "+err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	m.closeOverlay()

	m.cfg.Fleet, m.cfg.FleetGroups = fleet, groups
	m.activeFleetGroup = m.fleetGroupLabel()
	if group != "" {
		m.activeFleetGroup = group
	}
	m.fleetExtra = nil
	m.rebuildCommands()

	m.notice(savedFleetNotice(len(chosen), m.cfg.SourcePath), theme.StatusHealthy)
	return tea.Batch(m.openFleetFresh(), m.expireNotice())
}

// savedFleetNotice says what was saved and where. Somebody whose configuration
// has just been written by a program is owed the path it was written to.
func savedFleetNotice(chosen int, path string) string {
	msg := itoa(chosen) + " " + plural(chosen, "cluster") + " saved"
	if path != "" {
		msg += " to " + path
	}
	return msg
}

// withGroup replaces a group by name, or appends it. A group with nothing in
// it is removed: an empty group is a row in a menu that leads nowhere.
func withGroup(groups []config.FleetGroup, g config.FleetGroup) []config.FleetGroup {
	out := make([]config.FleetGroup, 0, len(groups)+1)
	replaced := false
	for _, existing := range groups {
		if existing.Name != g.Name {
			out = append(out, existing)
			continue
		}
		replaced = true
		if len(g.Contexts) > 0 {
			out = append(out, g)
		}
	}
	if !replaced && len(g.Contexts) > 0 {
		out = append(out, g)
	}
	return out
}

// deleteFleetGroup removes a named group. The clusters themselves are
// untouched; only the grouping goes.
func (m *Model) deleteFleetGroup(name string) tea.Cmd {
	if name == "" || name == defaultFleetGroup {
		return nil
	}
	groups := withGroup(append([]config.FleetGroup(nil), m.cfg.FleetGroups...),
		config.FleetGroup{Name: name})
	if err := m.writeFleet(m.cfg.Fleet, groups); err != nil {
		m.notice("Could not save the fleet: "+err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}

	m.cfg.FleetGroups = groups
	if m.activeFleetGroup == name {
		m.activeFleetGroup = ""
		if remaining := m.fleetGroups(); len(remaining) > 0 {
			m.activeFleetGroup = remaining[0].Name
		}
		m.fleetExtra = nil
	}
	m.rebuildCommands()
	m.notice("Fleet group "+name+" removed", theme.StatusUnknown)
	return tea.Batch(m.expireNotice(), m.reopenFleetIfShown())
}

// writeFleet persists the fleet, remembering where the configuration lives for
// a session that started without a file.
func (m *Model) writeFleet(fleet []string, groups []config.FleetGroup) error {
	path := m.cfg.SourcePath
	if path == "" {
		var err error
		if path, err = config.Path(); err != nil {
			return err
		}
	}
	if err := config.SaveFleet(path, fleet, groups); err != nil {
		return err
	}
	m.cfg.SourcePath = path
	return nil
}

// reopenFleetIfShown refetches the fleet when the change is visible, and does
// nothing when it is not: a group edited from the dashboard must not start
// contacting clusters behind a screen that is not showing them.
func (m *Model) reopenFleetIfShown() tea.Cmd {
	if m.view != viewFleet && m.view != viewFleetResource {
		return nil
	}
	return m.openFleetFresh()
}

// openFleetFresh restarts the fan-out on the current membership.
func (m *Model) openFleetFresh() tea.Cmd {
	m.stopFleet()
	if m.view != viewFleet {
		return m.openFleet()
	}
	// openFleet toggles when the fleet is already on screen; this is a reload,
	// not a toggle.
	return m.startFleet(m.fleetContexts())
}

// promptNewFleetGroup asks for a name, then opens the picker on it. Naming
// first is what makes the tick marks mean something: "which clusters" has no
// answer until "for what" does.
func (m *Model) promptNewFleetGroup() tea.Cmd {
	m.promptTitle = "Name the fleet group"
	m.promptNote = "Production, staging, a region, a team — whatever you open together."
	m.promptError = ""
	m.promptRef = objectRef{}
	m.promptInput.SetValue("")
	m.promptAccept = func(m *Model, value string) tea.Cmd { return m.createFleetGroup(value) }
	m.overlay = overlayPrompt
	return nil
}

// createFleetGroup validates the name and hands over to the picker. Nothing is
// written yet: an empty group is not worth a line in the configuration file,
// and the user may still press Esc.
func (m *Model) createFleetGroup(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	switch name {
	case "":
		m.promptError = "A group needs a name."
		return nil
	case defaultFleetGroup:
		m.promptError = "That name belongs to the plain fleet list."
		return nil
	}
	for _, g := range m.cfg.FleetGroups {
		if strings.EqualFold(g.Name, name) {
			m.promptError = "There is already a group called " + g.Name + "."
			return nil
		}
	}
	m.cancelPrompt()
	return m.openFleetPicker(name)
}

// chooseFleetSubtitle says what is there now, so the command reads differently
// to somebody who has never chosen a cluster and somebody adjusting six.
func chooseFleetSubtitle(chosen int) string {
	if chosen == 0 {
		return "nothing is in the fleet yet"
	}
	return itoa(chosen) + " " + plural(chosen, "cluster") + " now"
}
