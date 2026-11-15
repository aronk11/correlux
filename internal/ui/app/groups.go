package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// groupMode is what the dashboard is grouped by.
//
// Worst first is the right order for "what is broken" and the wrong one for
// every question that starts with "everything that…": everything in this
// namespace, everything of the checkout system, everything team payments
// owns, everything the platform itself is made of. Those are answered by
// putting the applications that share a label next to each other, with the
// label printed once and the state of the group beside it.
type groupMode int

const (
	groupNone groupMode = iota
	groupNamespace
	groupRole
	groupSystem
	groupTeam
	groupManager
	groupModes // the number of modes, for cycling
)

func (g groupMode) active() bool { return g != groupNone }

// title is the heading of the group column, which is also the name of the
// column it replaces — so a filter or a sort naming it keeps working.
func (g groupMode) title() string {
	switch g {
	case groupNamespace:
		return "Namespace"
	case groupRole:
		return "Role"
	case groupSystem:
		return "System"
	case groupTeam:
		return "Team"
	case groupManager:
		return "Managed by"
	}
	return ""
}

// word names the mode in a sentence: "grouped by role".
func (g groupMode) word() string {
	switch g {
	case groupNamespace:
		return "namespace"
	case groupRole:
		return "role"
	case groupSystem:
		return "system"
	case groupTeam:
		return "team"
	case groupManager:
		return "what manages them"
	}
	return "nothing"
}

// key is the group one application falls in. The empty key is a group too:
// the applications nobody labelled are a fact worth seeing together.
func (g groupMode) key(a *application.Application) string {
	switch g {
	case groupNamespace:
		return a.Namespace
	case groupRole:
		return a.Traits.Role.Name
	case groupSystem:
		return a.Traits.System
	case groupTeam:
		return a.Traits.Team
	case groupManager:
		return a.Manager.Tool
	}
	return ""
}

// none is how the group without the label reads.
func (g groupMode) none() string {
	switch g {
	case groupRole:
		// Not a gap: an application with no platform role is what the cluster
		// is there to run.
		return "applications"
	case groupSystem:
		return "(no system)"
	case groupTeam:
		return "(no team)"
	case groupManager:
		return "(unmanaged)"
	}
	return "—"
}

// useful reports whether grouping this way would say anything about these
// applications: at least one of them carries the label, and — for a
// namespace — there is more than one to tell apart.
func (g groupMode) useful(apps []application.Application) bool {
	if g == groupNone {
		return true
	}
	seen := map[string]bool{}
	labelled := false
	for i := range apps {
		k := g.key(&apps[i])
		seen[k] = true
		labelled = labelled || k != ""
	}
	if g == groupNamespace {
		return len(seen) > 1
	}
	return labelled
}

// grouped puts the applications that share a group next to each other.
//
// The groups come in the order their first member already had, and the
// members keep theirs. Since the list arrives worst first, the group holding
// the worst application leads and reads worst first inside; sorted by a
// column, the group holding that column's top row leads instead. Grouping
// never invents an order of its own to argue with the one that was asked for.
func grouped(apps []application.Application, g groupMode) []application.Application {
	if !g.active() || len(apps) < 2 {
		return apps
	}
	order := []string{}
	members := map[string][]int{}
	for i := range apps {
		k := g.key(&apps[i])
		if _, ok := members[k]; !ok {
			order = append(order, k)
		}
		members[k] = append(members[k], i)
	}
	out := make([]application.Application, 0, len(apps))
	for _, k := range order {
		for _, i := range members[k] {
			out = append(out, apps[i])
		}
	}
	return out
}

// groupLabels is the leading cell of each row of a grouped dashboard: the
// group's name, how many applications it holds and how many of them are not
// well — on the group's first row, and blank on the rest.
//
// The first row on screen carries it too, so a group taller than the window
// does not scroll its own name out of sight.
func groupLabels(t *theme.Theme, apps []application.Application, g groupMode,
	health func(*application.Application) application.Health, offset int) []string {
	if !g.active() {
		return nil
	}
	type tally struct{ total, down, degraded int }
	tallies := map[string]*tally{}
	for i := range apps {
		k := g.key(&apps[i])
		if tallies[k] == nil {
			tallies[k] = &tally{}
		}
		tallies[k].total++
		switch health(&apps[i]) {
		case application.Down:
			tallies[k].down++
		case application.Degraded:
			tallies[k].degraded++
		}
	}
	out := make([]string, len(apps))
	for i := range apps {
		k := g.key(&apps[i])
		if i != offset && i > 0 && g.key(&apps[i-1]) == k {
			continue
		}
		label := k
		if label == "" {
			label = g.none()
		}
		c := tallies[k]
		label += " (" + itoa(c.total) + ")"
		if c.down > 0 {
			label += " " + t.Glyph(theme.StatusCritical) + itoa(c.down)
		}
		if c.degraded > 0 {
			label += " " + t.Glyph(theme.StatusWarning) + itoa(c.degraded)
		}
		out[i] = label
	}
	return out
}

// cycleGrouping moves the dashboard to the next way of grouping that says
// something here, and back to none after the last. A mode no application has
// a label for is skipped: a key that regroups a list into one group called
// "(no team)" is a key that appears to do nothing.
func (m *Model) cycleGrouping() tea.Cmd {
	if m.view != viewApplications {
		return nil
	}
	next := m.appGroup
	for range int(groupModes) {
		next = (next + 1) % groupModes
		if next.useful(m.applications()) {
			break
		}
	}
	return m.setGrouping(next)
}

// setGrouping groups the dashboard one way, keeping the cursor on the
// application it was on.
func (m *Model) setGrouping(g groupMode) tea.Cmd {
	previous := m.cursorApplicationKey()
	m.appGroup = g
	m.keepCursorOnApplication(previous)
	m.rebuildCommands()
	if g.active() {
		m.notice("Grouped by "+g.word(), theme.StatusHealthy)
	} else {
		m.notice("Grouping off: worst first", theme.StatusHealthy)
	}
	return m.expireNotice()
}

// groupHint is what the status bar says the key does next.
func (m *Model) groupHint() string {
	if m.appGroup.active() {
		return "Group: " + m.appGroup.word()
	}
	return "Group"
}

// labels are the organisation's own label spellings from the config file.
func (m *Model) labels() application.Labels {
	return application.Labels{
		Team:        m.cfg.Labels.Team,
		Environment: m.cfg.Labels.Environment,
		System:      m.cfg.Labels.System,
	}
}
