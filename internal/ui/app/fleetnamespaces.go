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

// The fleet can be scoped to a few namespaces, across every cluster in it.
//
// A scope in Correlux is a namespace of one cluster (SPEC 8). One level up it
// is the same question asked of many: a team owning two namespaces in twelve
// clusters wants an overview of those two, not of everybody else's software in
// all twelve. Without it the fleet is unreadable to them, and unreachable to a
// namespace-scoped service account, whose cluster-wide list calls are refused.
//
// The namespaces are asked of the API server rather than filtered out of a
// cluster-wide read: it is less to fetch, and it is the only version that works
// for an account that may not read the whole cluster.

// openFleetNamespacePicker starts editing the active group's scope.
func (m *Model) openFleetNamespacePicker() tea.Cmd {
	m.fleetNSDraft = map[string]bool{}
	for _, name := range m.fleetNamespaces() {
		m.fleetNSDraft[name] = true
	}
	return m.openOverlay(overlayFleetNamespaces)
}

// fleetNamespacePickerTitle names the group being scoped: "which namespaces"
// has no answer without "of what".
func (m *Model) fleetNamespacePickerTitle() string {
	return "Namespaces in " + m.fleetGroupLabel()
}

// fleetNamespacePickerFooter counts as it goes, and says what no ticks means —
// which is the state the picker opens in and the one people need a way back to.
func (m *Model) fleetNamespacePickerFooter() string {
	chosen := 0
	for _, on := range m.fleetNSDraft {
		if on {
			chosen++
		}
	}
	scope := "every namespace"
	if chosen > 0 {
		scope = itoa(chosen) + " " + plural(chosen, "namespace")
	}
	return "Tab pick   Ctrl+T clear   Enter save   Esc cancel   " +
		m.theme.Glyphs.Bullet + " " + scope
}

// fleetNamespaceCandidates are the namespaces worth offering: the ones already
// chosen, the ones the fleet has actually answered with, and the ones the
// session's own cluster knows about.
//
// There is deliberately no request behind this list. Listing the namespaces of
// every cluster in the fleet would be one more fan-out for a question the fleet
// has usually already answered, and a namespace missing from the list is still
// reachable by typing it.
func (m *Model) fleetNamespaceCandidates() []string {
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" {
			seen[name] = true
		}
	}
	for name, on := range m.fleetNSDraft {
		if on {
			add(name)
		}
	}
	for _, member := range m.fleetMembers {
		for i := range member.Applications {
			add(member.Applications[i].Namespace)
		}
	}
	for _, name := range m.namespaces.Get().Names {
		add(name)
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// filterFleetNamespaces lists the candidates with their tick, and lets a
// namespace nobody has seen yet be typed in.
func (m *Model) filterFleetNamespaces(query string) []components.Item {
	names := m.fleetNamespaceCandidates()

	matched := names
	var highlights map[int][]int
	query = strings.TrimSpace(query)
	if query != "" && len(names) > 0 {
		highlights = make(map[int][]int)
		matched = nil
		for _, res := range fuzzy.Find(query, names) {
			highlights[len(matched)] = res.MatchedIndexes
			matched = append(matched, names[res.Index])
		}
	}

	items := make([]components.Item, 0, len(matched)+2)
	for i, name := range matched {
		item := components.Item{
			ID:        name,
			Title:     name,
			Highlight: highlights[i],
			Checked:   m.fleetNSDraft[name],
		}
		items = append(items, item)
	}

	// A namespace the fleet has not mentioned and this cluster does not have is
	// still a namespace in some other cluster of the fleet.
	if query != "" && !containsString(matched, query) {
		items = append(items, components.Item{
			ID:       query,
			Title:    "Use namespace \"" + query + "\"",
			Subtitle: "not in the visible list",
			Checked:  m.fleetNSDraft[query],
		})
	}

	if len(items) == 0 {
		items = append(items, components.Item{
			ID:       "__empty",
			Title:    "No namespace to offer yet",
			Subtitle: "open the fleet first, or type a name",
			Disabled: true,
		})
	}
	return items
}

// toggleFleetNamespacePick flips the row under the cursor.
func (m *Model) toggleFleetNamespacePick() {
	item, ok := m.fleetNSPicker.Selected()
	if !ok || item.Disabled {
		return
	}
	m.fleetNSDraft[item.ID] = !m.fleetNSDraft[item.ID]
	m.fleetNSPicker.Footer = m.fleetNamespacePickerFooter()
	m.fleetNSPicker.Refresh()
}

// clearFleetNamespacePicks is the way back to the whole fleet. It is one key
// because "actually, show me everything" is the thing somebody wants in the
// middle of an incident, and unticking six rows is not that.
func (m *Model) clearFleetNamespacePicks() {
	m.fleetNSDraft = map[string]bool{}
	m.fleetNSPicker.Footer = m.fleetNamespacePickerFooter()
	m.fleetNSPicker.Refresh()
}

// saveFleetNamespaces writes the scope to the configuration file and reopens
// the fleet on it, the same way choosing the clusters does: a team's namespaces
// are not something anybody wants to retype every morning.
func (m *Model) saveFleetNamespaces() tea.Cmd {
	chosen := make([]string, 0, len(m.fleetNSDraft))
	for name, on := range m.fleetNSDraft {
		if on {
			chosen = append(chosen, name)
		}
	}
	sort.Strings(chosen)

	group := m.activeFleetGroup
	fleet := m.cfg.Fleet
	namespaces := m.cfg.FleetNamespaces
	groups := append([]config.FleetGroup(nil), m.cfg.FleetGroups...)

	if group == "" || (group == defaultFleetGroup && !m.hasNamedDefaultGroup()) {
		namespaces = chosen
	} else {
		existing := config.FleetGroup{Name: group}
		for _, g := range groups {
			if g.Name == group {
				existing = g
				break
			}
		}
		existing.Namespaces = chosen
		groups = withGroup(groups, existing)
	}

	if err := m.writeFleet(fleet, namespaces, groups); err != nil {
		m.notice("Could not save the namespaces: "+err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	m.closeOverlay()

	m.cfg.FleetNamespaces, m.cfg.FleetGroups = namespaces, groups
	m.rebuildCommands()

	m.notice(savedFleetScopeNotice(chosen), theme.StatusHealthy)
	return tea.Batch(m.reopenFleetScoped(), m.expireNotice())
}

// reopenFleetScoped reads again on the new scope, without moving the user off
// the fleet screen they are on: a table of one kind across the fleet is as much
// a fleet view as the overview is, and it is the screen the scope was just
// changed from.
func (m *Model) reopenFleetScoped() tea.Cmd {
	if m.view == viewFleetResource {
		return m.openFleetResource(m.fleetResource)
	}
	return m.openFleetFresh()
}

// clearFleetNamespaces drops the scope without opening the picker, which is
// what somebody who narrowed the fleet an hour ago wants from the palette.
func (m *Model) clearFleetNamespaces() tea.Cmd {
	m.fleetNSDraft = map[string]bool{}
	return m.saveFleetNamespaces()
}

// savedFleetScopeNotice says what the fleet now covers, in the words the screen
// itself uses.
func savedFleetScopeNotice(namespaces []string) string {
	if len(namespaces) == 0 {
		return "Fleet: every namespace"
	}
	return "Fleet: " + strings.Join(namespaces, ", ")
}

// fleetScopeSubtitle describes the scope for the command palette.
func (m *Model) fleetScopeSubtitle() string {
	namespaces := m.fleetNamespaces()
	if len(namespaces) == 0 {
		return "every namespace of every cluster in " + m.fleetGroupLabel()
	}
	return strings.Join(namespaces, ", ") + " — in every cluster in " + m.fleetGroupLabel()
}
