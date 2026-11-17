package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// maxTabs is how many tabs a session holds: one per digit, so every one of
// them has a key.
const maxTabs = 9

// tabNameWidth is where a tab's name is cut. The bar shares a row with the
// navigation, and a name is only there to tell two tabs apart.
const tabNameWidth = 14

// tab is a screenful of panes that is not on screen: its layout, and which
// of its panes had the keyboard. The tab in front keeps all of this on the
// Model instead, and is written back here when it is left.
type tab struct {
	panes     []*pane
	focus     int
	split     layout.Split
	zoomed    bool
	driverID  int
	followKey string
}

// tabItem is one entry of the tab bar, and where it was drawn.
type tabItem struct {
	label      string
	index      int
	start, end int
	active     bool
}

// stashTab writes the tab in front back into its slot.
func (m *Model) stashTab() {
	*m.tabs[m.tabIndex] = tab{
		panes: m.panes, focus: m.focus, split: m.split, zoomed: m.zoomed,
		driverID: m.driverID, followKey: m.followKey,
	}
}

// loadTab brings the tab at index i to the front.
func (m *Model) loadTab(i int) {
	t := m.tabs[i]
	m.tabIndex = i
	m.panes, m.focus, m.split, m.zoomed = t.panes, t.focus, t.split, t.zoomed
	m.driverID, m.followKey = t.driverID, t.followKey
	m.pane = m.focused()
	m.layoutPanes()
}

// newTab opens a tab on the dashboard and moves to it.
func (m *Model) newTab() tea.Cmd {
	if len(m.tabs) >= maxTabs {
		m.notice("Nine tabs is what the digits reach — close one first", theme.StatusWarning)
		return m.expireNotice()
	}
	m.suspendTab()
	m.stashTab()
	m.nextPaneID++
	p := newPane(m.nextPaneID)
	p.appSort, p.appGroup = m.pane.appSort, m.pane.appGroup
	p.frame, p.body = m.fullBody, m.fullBody
	m.tabs = append(m.tabs, &tab{panes: []*pane{p}, driverID: p.id})
	m.loadTab(len(m.tabs) - 1)
	m.rebuildCommands()
	return nil
}

// switchTab moves to the tab at index i.
//
// A tab that is not on screen reads nothing: its logs stop when it is left
// and start again when it is back, because a stream nobody is looking at is
// still a connection to somebody's API server (ADR 17).
func (m *Model) switchTab(i int) tea.Cmd {
	if i < 0 || i >= len(m.tabs) {
		if len(m.tabs) == 1 {
			m.notice("One tab open — "+m.keys.Key(ActionTabNew)+" opens another", theme.StatusUnknown)
			return m.expireNotice()
		}
		return nil
	}
	if i == m.tabIndex {
		return nil
	}
	m.suspendTab()
	m.stashTab()
	m.loadTab(i)
	cmd := m.resumeTab()
	m.rebuildCommands()
	return cmd
}

// cycleTab moves to the next tab, or the previous one.
func (m *Model) cycleTab(delta int) tea.Cmd {
	n := len(m.tabs)
	if n == 1 {
		return m.switchTab(-1)
	}
	return m.switchTab(((m.tabIndex+delta)%n + n) % n)
}

// suspendTab stops the streams of the tab in front, keeping what they were
// reading so they can be opened again.
func (m *Model) suspendTab() {
	for _, p := range m.panes {
		m.withPane(p, func() {
			if m.view != viewLogs || m.cancelLogs == nil {
				return
			}
			m.cancelLogs()
			m.cancelLogs = nil
			m.logGeneration++
			m.logStream = nil
			m.logSuspended = true
		})
	}
}

// resumeTab reopens what suspendTab stopped and lets the companions catch up
// with a selection that may have been reloaded underneath them.
func (m *Model) resumeTab() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range m.panes {
		if !p.logSuspended {
			continue
		}
		cmds = append(cmds, m.inPane(p, func() tea.Cmd {
			m.logSuspended = false
			if m.view != viewLogs || len(m.logTargets) == 0 {
				return nil
			}
			// The tail is read again from the server; keeping the old lines
			// would print it twice.
			m.logLines, m.logDropped = nil, 0
			return m.startLogs()
		}))
	}
	if m.following() {
		m.followKey = m.subjectKey()
		cmds = append(cmds, m.syncFollowers())
	}
	return tea.Batch(cmds...)
}

// closeTab closes the tab in front and everything open in it.
func (m *Model) closeTab() tea.Cmd {
	if len(m.tabs) == 1 {
		return nil
	}
	for _, p := range m.panes {
		m.withPane(p, m.releasePane)
	}
	i := m.tabIndex
	m.tabs = append(m.tabs[:i:i], m.tabs[i+1:]...)
	m.loadTab(min(i, len(m.tabs)-1))
	cmd := m.resumeTab()
	m.rebuildCommands()
	return cmd
}

// tabName is what the bar calls a tab: what its main pane shows.
func (m *Model) tabName(i int) string {
	panes := m.tabs[i].panes
	if i == m.tabIndex {
		panes = m.panes
	}
	if len(panes) == 0 {
		return ""
	}
	lead := panes[0]
	for _, p := range panes {
		if p.follow == followNone {
			lead = p
			break
		}
	}
	return m.paneName(lead)
}

// tabItems lays the tab bar out against the right-hand edge of the navigation
// row. With one tab there is no bar: a session that never opens a second one
// looks exactly as it did before there were any.
func (m *Model) tabItems(names bool) []tabItem {
	if len(m.tabs) < 2 {
		return nil
	}
	items := make([]tabItem, len(m.tabs))
	width := 0
	for i := range m.tabs {
		label := itoa(i + 1)
		if names {
			if name := m.tabName(i); name != "" {
				label += " " + xansi.Truncate(name, tabNameWidth, m.theme.Glyphs.Ellipsis)
			}
		}
		items[i] = tabItem{label: label, index: i, active: i == m.tabIndex}
		width += lipgloss.Width(label) + 2
	}
	width += len(items) - 1
	x := max(m.screen.Width-width, 0)
	for i := range items {
		items[i].start = x
		items[i].end = x + lipgloss.Width(items[i].label) + 2
		x = items[i].end + 1
	}
	return items
}

// tabBar is tabItems as it fits beside a navigation that ends at column used:
// with names where there is room, as bare numbers where there is not.
func (m *Model) tabBar(used int) []tabItem {
	for _, names := range []bool{true, false} {
		items := m.tabItems(names)
		if len(items) == 0 || items[0].start >= used+navigationGap {
			return items
		}
	}
	return m.tabItems(false)
}

// renderTabBar draws items, starting at the column the first of them is at.
func (m *Model) renderTabBar(items []tabItem) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString(" ")
		}
		if item.active {
			b.WriteString(m.theme.SelectedRow.Render("[" + item.label + "]"))
			continue
		}
		b.WriteString(m.theme.Muted.Render(" " + item.label + " "))
	}
	return b.String()
}

// clickTab switches to the tab drawn at column x, if one is.
func (m *Model) clickTab(x int) (tea.Cmd, bool) {
	for _, item := range m.tabBar(m.navigationEnd()) {
		if x >= item.start && x < item.end {
			return m.switchTab(item.index), true
		}
	}
	return nil, false
}

// navigationEnd is the column the navigation's last destination ends at.
func (m *Model) navigationEnd() int {
	items := m.navigationItems()
	return items[len(items)-1].end
}

// tabBarWidth is the room the tab bar asks the navigation to leave, at its
// narrowest.
func (m *Model) tabBarWidth() int {
	items := m.tabItems(false)
	if len(items) == 0 {
		return 0
	}
	return items[len(items)-1].end - items[0].start + navigationGap
}

// tabCommands are the palette's entries for tabs.
func (m *Model) tabCommands() []palette.Command {
	cmds := []palette.Command{
		{
			ID: "tab.new", Action: paletteTab, Arg: "new",
			Title: "Open a new tab", Subtitle: "a second investigation, with its own panes",
			Category: "Tabs", Keywords: []string{"tab", "window", "new", "workspace", "tmux", "multi"},
			Shortcut: m.keys.Key(ActionTabNew), Weight: 55, Enabled: len(m.tabs) < maxTabs,
		},
		{
			ID: "tab.next", Action: paletteTab, Arg: "next",
			Title: "Move to the next tab", Category: "Tabs", Keywords: []string{"tab", "window", "next", "switch"},
			Shortcut: m.keys.Key(ActionTabNext), Weight: 54, Enabled: len(m.tabs) > 1,
		},
		{
			ID: "tab.close", Action: paletteTab, Arg: "close",
			Title: "Close this tab", Subtitle: "and every pane in it",
			Category: "Tabs", Keywords: []string{"tab", "window", "close", "kill"},
			Weight: 52, Enabled: len(m.tabs) > 1,
		},
	}
	for i := range m.tabs {
		if i == m.tabIndex {
			continue
		}
		cmds = append(cmds, palette.Command{
			ID: "tab.go." + itoa(i+1), Action: paletteTab, Arg: "go:" + itoa(i),
			Title: "Go to tab " + itoa(i+1), Subtitle: m.tabName(i),
			Category: "Tabs", Keywords: []string{"tab", "window", "switch", itoa(i + 1)},
			Shortcut: "Alt+" + itoa(i+1), Weight: 53, Enabled: true,
		})
	}
	return cmds
}

// runTabCommand runs one of tabCommands.
func (m *Model) runTabCommand(arg string) tea.Cmd {
	if n, ok := strings.CutPrefix(arg, "go:"); ok {
		for i := range m.tabs {
			if itoa(i) == n {
				return m.switchTab(i)
			}
		}
		return nil
	}
	switch arg {
	case "new":
		return m.newTab()
	case "next":
		return m.cycleTab(1)
	case "close":
		return m.closeTab()
	}
	return nil
}

// tabKey reports the tab a keystroke like alt+3 names.
func tabKey(keystroke string) (int, bool) {
	digit, ok := strings.CutPrefix(keystroke, "alt+")
	if !ok || len(digit) != 1 || digit[0] < '1' || digit[0] > '9' {
		return 0, false
	}
	return int(digit[0] - '1'), true
}
