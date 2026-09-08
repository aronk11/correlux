package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/components"
)

// A single description drives both the visible navigation and its hit targets.
// Commands remain a tool over the current workspace, never a destination.
type navigationItem struct {
	label, key, action string
	active             bool
	start, end         int
}

// navigationGap is the gutter between two destinations. Every item is the same
// width whether it is active or not — the active one spends its padding on
// brackets — so the bar has one rhythm and does not shuffle sideways as you
// move through it.
const navigationGap = 2

// navigationItems describes the bar as it will be drawn, shortcuts included
// when there is room for them.
func (m *Model) navigationItems() []navigationItem {
	return m.navigationItemsWith(m.navigationShowsKeys())
}

// navigationShowsKeys reports whether the whole bar fits with a shortcut
// printed beside every destination. It is measured rather than guessed at a
// round number of columns, because the answer decides something else as well:
// the status bar repeats a destination's shortcut only when the bar could not
// print it.
func (m *Model) navigationShowsKeys() bool {
	items := m.navigationItemsWith(true)
	return items[len(items)-1].end <= m.screen.Width
}

func (m *Model) navigationItemsWith(keys bool) []navigationItem {
	items := []navigationItem{
		{label: "Apps", action: ActionApplications, active: m.view == viewApplications || m.view == viewApplication || m.view == viewWhy},
		{label: "Resources", action: ActionResourcePicker, active: m.view == viewTable || m.view == viewObject},
		{label: "Usage", action: ActionUsage, active: m.view == viewUsage},
		{label: "Events", action: ActionActivity, active: m.view == viewActivity},
		{label: "Fleet", action: ActionFleet, active: m.view == viewFleet || m.view == viewFleetResource},
		{label: "Commands", action: ActionPalette, active: m.overlay == overlayPalette},
	}
	x := 0
	for i := range items {
		item := &items[i]
		// The shortcut is printed beside the destination it opens, on a
		// terminal wide enough to spare the columns.
		if keys {
			item.key = m.keys.Key(item.action)
		}
		item.start = x
		item.end = x + navigationWidth(*item)
		x = item.end + navigationGap
	}
	return items
}

// navigationWidth is what one item occupies, brackets or padding included. The
// active item is bracketed rather than padded, so both spellings are the same
// width and the bar does not shuffle sideways as you move through it.
func navigationWidth(item navigationItem) int {
	w := lipgloss.Width(item.label) + 2
	if item.key != "" {
		w += 1 + lipgloss.Width(item.key)
	}
	return w
}

func (m *Model) renderNavigation() string {
	var b strings.Builder
	for i, item := range m.navigationItems() {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", navigationGap))
		}
		// A shortcut is drawn in the key style wherever it appears, the same
		// as in the status bar — except inside the selection, where a second
		// colour is a hole in the highlight rather than a second word.
		if item.active {
			label := item.label
			if item.key != "" {
				label += " " + item.key
			}
			b.WriteString(m.theme.SelectedRow.Render("[" + label + "]"))
			continue
		}
		b.WriteString(m.theme.Muted.Render(" " + item.label))
		if item.key != "" {
			b.WriteString(m.theme.Key.Render(" " + item.key))
		}
		b.WriteString(m.theme.Muted.Render(" "))
	}
	return b.String() + strings.Repeat(" ", max(m.screen.Width-lipgloss.Width(b.String()), 0))
}

func (m *Model) clickNavigation(x int) tea.Cmd {
	for _, item := range m.navigationItems() {
		if x < item.start || x >= item.end {
			continue
		}
		// Clicking an active destination keeps its selection and scroll position.
		if item.active && (m.view == viewApplications || m.view == viewUsage || m.view == viewActivity || m.view == viewFleet) && item.action != ActionResourcePicker {
			return nil
		}
		switch item.action {
		case ActionApplications:
			return m.backToApplications()
		case ActionResourcePicker:
			return m.openOverlay(overlayResources)
		case ActionUsage:
			return m.openUsage()
		case ActionActivity:
			return m.openActivity()
		case ActionFleet:
			return m.openFleet()
		case ActionPalette:
			return m.openOverlay(overlayPalette)
		}
	}
	return nil
}

func (m *Model) commandSubject() string {
	if ref, ok := m.targetRef(); ok {
		return ref.label()
	}
	if a, ok := m.explanationTarget(); ok {
		return a.Name
	}
	if m.view == viewFleet || m.view == viewFleetResource {
		return "Fleet · " + m.fleetGroupLabel()
	}
	return m.contextName + " / " + m.scopeLabel()
}

// goBack is what Esc does, everywhere: one step back the way you came.
//
// It never jumps home. Home is the Apps key, and a back key that sometimes
// returns to the screen behind you and sometimes throws away the whole
// investigation is a key you have to stop and think about before pressing —
// which is the opposite of what a back key is for.
//
// Most views answer this themselves, because only they know what "the way you
// came" was: an object pops its trail, logs return to whatever opened them.
// What is left is the screens one step below the dashboard.
func (m *Model) goBack() tea.Cmd {
	if m.view == viewWhy {
		// The explanation is about one application, and that application is
		// the screen behind it — not the dashboard two steps up.
		m.view = viewApplication
		m.detailPort.Offset = 0
		m.rebuildCommands()
		return nil
	}
	return m.backToApplications()
}

// backDestination names where Esc goes from here, for the status bar. It is
// empty on the dashboard, which is where back stops.
//
// The bar prints the destination rather than the word "Back" because "Back"
// answers the question nobody has: the question is not whether Esc leaves,
// it is what you land on — the application, the table you were browsing, or
// the whole way out to the dashboard.
func (m *Model) backDestination() string {
	switch m.view {
	case viewApplications:
		return ""
	case viewWhy:
		return m.applicationLabel()
	case viewObject:
		if len(m.objectTrail) > 0 {
			return m.objectTrail[len(m.objectTrail)-1].label()
		}
		return m.viewLabel(m.objectFrom)
	case viewLogs:
		return m.viewLabel(m.logFrom)
	case viewUsage:
		if m.usageDrilledIn {
			return "All namespaces"
		}
	case viewFleetResource:
		return "Fleet"
	}
	return "Applications"
}

// viewLabel names a screen the way the breadcrumb does, so "Esc <label>" and
// the trail at the top of the frame agree about where you are going.
func (m *Model) viewLabel(v viewKind) string {
	switch v {
	case viewTable:
		return m.resource.Kind()
	case viewActivity:
		return "Recent activity"
	case viewApplication:
		return m.applicationLabel()
	case viewWhy:
		return "Why"
	case viewObject:
		return m.objectTarget.label()
	case viewUsage:
		return "Resource usage"
	case viewFleet:
		return "Fleet"
	case viewFleetResource:
		return m.fleetResource.Kind()
	}
	return "Applications"
}

// applicationLabel names the open application, falling back to the dashboard
// when there is none — an application that has just been deleted must not
// leave the bar pointing at a screen that no longer exists.
func (m *Model) applicationLabel() string {
	if a, ok := m.currentApplication(); ok {
		return a.Name
	}
	if m.selectedApp != "" {
		return m.selectedApp
	}
	return "Applications"
}

// withoutNavigationDuplicates drops the status-bar hints for destinations the
// navigation bar is already printing a shortcut for.
//
// A shortcut belongs on screen once. Printed in both places it reads as two
// different offers, and it spends the bottom row — the only row that can say
// what *this* screen does — on keys that are two lines above it. On a
// terminal too narrow for the bar to print them, the status bar is the only
// place they appear, and they stay.
func (m *Model) withoutNavigationDuplicates(hints []components.KeyHint) []components.KeyHint {
	if !m.navigationShowsKeys() {
		return hints
	}
	shown := make(map[string]bool, len(hints))
	for _, item := range m.navigationItems() {
		if item.key != "" {
			shown[item.key+"\x00"+item.label] = true
		}
	}
	out := make([]components.KeyHint, 0, len(hints))
	for _, h := range hints {
		if shown[h.Key+"\x00"+h.Desc] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// listRows is how many rows the screen in front of you has, and zero for a
// screen that is not a list.
//
// It is what decides whether the status bar offers a key that acts on a row.
// An unreachable cluster, an empty namespace or a filter that matched nothing
// all end up here, and on all three the honest answer is the same: there is
// nothing to open, explain or filter, so those keys are not on offer and the
// ones that get you out of it are not buried among them.
func (m *Model) listRows() int {
	switch m.view {
	case viewApplications:
		return len(m.visibleApplications())
	case viewTable:
		return len(m.visibleRows())
	case viewFleet:
		return len(m.fleetMembers)
	case viewFleetResource:
		return len(m.visibleFleetRows())
	}
	return 0
}
