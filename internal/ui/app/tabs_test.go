package app

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
)

// tabBarPattern matches a tab's label as the bar draws it: a number, which no
// navigation item has.
var tabBarPattern = regexp.MustCompile(`\b[1-9]( |\])`)

// navigationRow is the row of the frame the navigation and the tab bar share.
func navigationRow(m *Model) string {
	return strings.Split(plainView(m), "\n")[m.screen.Nav.Y]
}

// openTabs opens tabs until there are n of them, leaving the last one in front.
func openTabs(t *testing.T, m *Model, n int) {
	t.Helper()
	for len(m.tabs) < n {
		press(t, m, "T")
	}
	if len(m.tabs) != n {
		t.Fatalf("setup: %d tabs, want %d", len(m.tabs), n)
	}
}

func TestTOpensATabOnTheDashboardAndMovesThere(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openApplication("payments")
	press(t, m, "T")
	if len(m.tabs) != 2 || m.tabIndex != 1 {
		t.Fatalf("T must open a second tab and move to it, got %d tabs, in tab %d", len(m.tabs), m.tabIndex)
	}
	if m.view != viewApplications || len(m.panes) != 1 {
		t.Errorf("a new tab is a fresh investigation: the dashboard in one pane, got view %v with %d panes", m.view, len(m.panes))
	}
}

func TestTheTabBarIsOnlyThereWithTwoOrMoreTabs(t *testing.T) {
	m := splitModel(t, 150, 40)
	if tabBarPattern.MatchString(navigationRow(m)) {
		t.Errorf("a session with one tab must look as it did before tabs existed: %q", navigationRow(m))
	}
	press(t, m, "T")
	row := navigationRow(m)
	if !strings.Contains(row, "[2") || !strings.Contains(row, " 1 ") {
		t.Errorf("with two tabs the navigation row must show both, the one in front bracketed: %q", row)
	}
	press(t, m, "X")
	if tabBarPattern.MatchString(navigationRow(m)) {
		t.Errorf("closing back to one tab must take the bar away again: %q", navigationRow(m))
	}
}

func TestTheTabBarNamesTabsWhereThereIsRoomAndNumbersThemWhereThereIsNot(t *testing.T) {
	wide := splitModel(t, 200, 40)
	press(t, wide, "T")
	if row := navigationRow(wide); !strings.Contains(row, "2 Applications") {
		t.Errorf("a wide terminal has room to say what each tab is: %q", row)
	}

	narrow := splitModel(t, 80, 24)
	press(t, narrow, "T")
	row := navigationRow(narrow)
	if strings.Contains(row, "Applications]") || !strings.Contains(row, "[2]") {
		t.Errorf("where the names do not fit the bar must fall back to numbers: %q", row)
	}
	assertFits(t, narrow, "80x24 with two tabs")
}

func TestBracketsMoveBetweenTabsAndWrapAround(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 3)

	press(t, m, "]")
	if m.tabIndex != 0 {
		t.Errorf("] after the last tab wraps to the first, got tab %d", m.tabIndex)
	}
	press(t, m, "[")
	if m.tabIndex != 2 {
		t.Errorf("[ before the first tab wraps to the last, got tab %d", m.tabIndex)
	}
	press(t, m, "[")
	if m.tabIndex != 1 {
		t.Errorf("[ walks backwards, got tab %d", m.tabIndex)
	}
}

func TestBracketsWithOneTabSaySo(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "]")
	if m.tabIndex != 0 || !strings.Contains(m.message, "One tab") {
		t.Errorf("a key that does nothing must say why, message = %q", m.message)
	}
}

func TestAltAndADigitJumpsToThatTab(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 3)
	press(t, m, "alt+1")
	if m.tabIndex != 0 {
		t.Errorf("Alt+1 is the first tab, got %d", m.tabIndex)
	}
	press(t, m, "alt+3")
	if m.tabIndex != 2 {
		t.Errorf("Alt+3 is the third tab, got %d", m.tabIndex)
	}
	press(t, m, "alt+9")
	if m.tabIndex != 2 {
		t.Errorf("a tab that does not exist must leave things where they are, got %d", m.tabIndex)
	}
}

func TestEachTabKeepsItsOwnPanesAndLayout(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "z")
	press(t, m, "T")
	if len(m.panes) != 1 || m.zoomed {
		t.Fatalf("the new tab starts clean, got %d panes, zoomed %v", len(m.panes), m.zoomed)
	}
	openCompanion(t, m, followWhy)
	openCompanion(t, m, followEvents)

	press(t, m, "alt+1")
	if len(m.panes) != 2 || !m.zoomed || m.panes[1].follow != followLogs {
		t.Errorf("the first tab must come back as it was left: %d panes, zoomed %v", len(m.panes), m.zoomed)
	}
	press(t, m, "alt+2")
	if len(m.panes) != 3 || m.zoomed {
		t.Errorf("the second tab must come back as it was left: %d panes, zoomed %v", len(m.panes), m.zoomed)
	}
}

func TestXOnTheLastPaneOfATabClosesTheTab(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 2)
	press(t, m, "X")
	if len(m.tabs) != 1 || m.tabIndex != 0 {
		t.Errorf("the last pane of a tab is the tab, got %d tabs, in tab %d", len(m.tabs), m.tabIndex)
	}
}

func TestXOnAPaneOfSeveralClosesThePaneNotTheTab(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 2)
	openCompanion(t, m, followLogs)
	press(t, m, "tab")
	press(t, m, "X")
	if len(m.tabs) != 2 || len(m.panes) != 1 {
		t.Errorf("only the last pane takes its tab with it, got %d tabs and %d panes", len(m.tabs), len(m.panes))
	}
}

func TestClosingAMiddleTabLandsOnItsNeighbour(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 3)
	press(t, m, "alt+2")
	press(t, m, "X")
	if len(m.tabs) != 2 || m.tabIndex != 1 {
		t.Errorf("closing the middle tab leaves two, in the one that followed it; got %d tabs, in tab %d", len(m.tabs), m.tabIndex)
	}
}

func TestAtMostNineTabsAndATenthSaysSo(t *testing.T) {
	m := splitModel(t, 200, 40)
	openTabs(t, m, maxTabs)
	m.message = ""
	press(t, m, "T")
	if len(m.tabs) != maxTabs {
		t.Errorf("a tenth tab would have no key: %d tabs", len(m.tabs))
	}
	if !strings.Contains(m.message, "Nine tabs") {
		t.Errorf("the refusal must say why, message = %q", m.message)
	}
	assertFits(t, m, "nine tabs at 200x40")
}

func TestLeavingATabStopsItsLogStreamAndComingBackRestartsIt(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openApplication("payments")
	press(t, m, "l")
	if m.view != viewLogs || m.cancelLogs == nil {
		t.Fatalf("setup: the first tab must be reading logs, view %v", m.view)
	}
	reading := m.pane

	press(t, m, "T")
	if reading.cancelLogs != nil || !reading.logSuspended {
		t.Error("a tab nobody can see must not hold a connection to the API server open")
	}
	if reading.view != viewLogs || len(reading.logTargets) == 0 {
		t.Error("the tab must remember what it was reading so that it can pick it up again")
	}

	cmd := press(t, m, "alt+1")
	if cmd == nil {
		t.Error("coming back must return the command that reopens the stream")
	}
	if reading.logSuspended || reading.cancelLogs == nil {
		t.Errorf("the stream must be open again, suspended = %v, cancel nil = %v", reading.logSuspended, reading.cancelLogs == nil)
	}
}

func TestACompanionsLogsAlsoStopWhenItsTabIsLeft(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	companion := m.panes[1]
	press(t, m, "T")
	if companion.cancelLogs != nil || !companion.logSuspended {
		t.Error("every pane of a hidden tab stops reading, not just the first")
	}
	press(t, m, "alt+1")
	if companion.cancelLogs == nil || companion.logSuspended {
		t.Error("the companion must read again when its tab is back")
	}
}

func TestComingBackToATabDoesNotPrintTheTailTwice(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openApplication("payments")
	press(t, m, "l")
	reading := m.pane
	reading.logLines = nil
	m.Update(logBatchMsg{gen: reading.logGeneration})
	press(t, m, "T")
	press(t, m, "alt+1")
	if len(reading.logLines) != 0 {
		t.Errorf("the tail is read again from the server, keeping the old lines would show it twice; got %d lines", len(reading.logLines))
	}
}

func TestLeavingATabWithoutLogsStartsNothingOnTheWayBack(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "T")
	press(t, m, "alt+1")
	if m.panes[0].logSuspended || m.panes[0].cancelLogs != nil {
		t.Error("a tab that was not reading logs must not be given a stream")
	}
}

func TestClosingATabStopsEverythingOpenInIt(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "T")
	openCompanion(t, m, followLogs)
	companion := m.panes[1]
	if companion.cancelLogs == nil {
		t.Fatal("setup: the companion must be reading")
	}
	press(t, m, "tab")
	press(t, m, "X") // the companion
	press(t, m, "X") // the tab
	if len(m.tabs) != 1 {
		t.Fatalf("setup: %d tabs", len(m.tabs))
	}
	if companion.cancelLogs != nil {
		t.Error("the stream of a closed pane outlived it")
	}

	press(t, m, "T")
	openCompanion(t, m, followLogs)
	reading := m.panes[1]
	press(t, m, "X") // closes the list, leaving the logs pane alone
	press(t, m, "X") // and then the tab
	if len(m.tabs) != 1 || reading.cancelLogs != nil {
		t.Errorf("closing a tab must cancel the streams of all its panes, %d tabs left, stream open: %v",
			len(m.tabs), reading.cancelLogs != nil)
	}
}

func TestAnAnswerForAPaneInAHiddenTabStillArrives(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	asked := m.panes[1]
	m.openResource("pods")
	gen := asked.table.Generation()

	press(t, m, "T")
	m.Update(paneMsg{pane: asked.id, msg: tableLoadedMsg{gen: gen, table: podTablePage("late-0")}})
	if asked.table.Get() == nil {
		t.Error("an answer for a pane of a tab that is not in front is not for the tab in front, and is not lost")
	}
	if m.panes[0].table.Get() != nil {
		t.Error("the answer landed in the tab that happens to be in front")
	}
}

func TestTheWheelAndClicksOnlyReachThePanesOfTheTabInFront(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "T")
	// The hidden tab's second pane was drawn where this click lands; it is not
	// on screen now and must not take the focus.
	m.Update(tea.MouseClickMsg{X: 100, Y: 10, Button: tea.MouseLeft})
	if m.tabIndex != 1 || m.focus != 0 {
		t.Errorf("a click must stay in the tab in front, in tab %d with focus %d", m.tabIndex, m.focus)
	}
}

func TestClickingATabLabelSwitchesToThatTab(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 3)
	items := m.tabBar(m.navigationEnd())
	if len(items) != 3 {
		t.Fatalf("setup: %d tab labels", len(items))
	}
	m.Update(tea.MouseClickMsg{X: items[0].start + 1, Y: m.screen.Nav.Y, Button: tea.MouseLeft})
	if m.tabIndex != 0 {
		t.Errorf("a click on the first label must go to the first tab, in tab %d", m.tabIndex)
	}
	m.Update(tea.MouseClickMsg{X: items[1].end - 1, Y: m.screen.Nav.Y, Button: tea.MouseLeft})
	if m.tabIndex != 1 {
		t.Errorf("a click on the last cell of the second label must go to the second tab, in tab %d", m.tabIndex)
	}
}

func TestClickingNextToTheTabBarDoesNotSwitchTabs(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "T")
	items := m.tabBar(m.navigationEnd())
	m.Update(tea.MouseClickMsg{X: items[0].start - 2, Y: m.screen.Nav.Y, Button: tea.MouseLeft})
	if m.tabIndex != 1 {
		t.Errorf("the gap before the bar is not a tab, in tab %d", m.tabIndex)
	}
}

func TestATabLabelIsNotClickableBehindAnOverlay(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "T")
	items := m.tabBar(m.navigationEnd())
	press(t, m, "ctrl+p")
	m.Update(tea.MouseClickMsg{X: items[0].start + 1, Y: m.screen.Nav.Y, Button: tea.MouseLeft})
	if m.tabIndex != 1 || m.overlay != overlayPalette {
		t.Errorf("the overlay owns the pointer: in tab %d, overlay %v", m.tabIndex, m.overlay)
	}
}

func TestTabKeysAreNotActedOnWhileAnOverlayIsOpen(t *testing.T) {
	m := splitModel(t, 150, 40)
	openTabs(t, m, 2)
	press(t, m, "ctrl+p")
	press(t, m, "alt+1")
	press(t, m, "]")
	if m.tabIndex != 1 {
		t.Errorf("Alt+1 behind an open palette switched tabs, in tab %d", m.tabIndex)
	}
}

func TestThePaletteGoesToATabByNumberAndClosesTheCurrentOne(t *testing.T) {
	m := splitModel(t, 150, 40)
	enabled := map[string]bool{}
	list := func() {
		for k := range enabled {
			delete(enabled, k)
		}
		for _, c := range m.registry.Commands() {
			if c.Category == "Tabs" {
				enabled[c.ID] = c.Enabled
			}
		}
	}
	list()
	if enabled["tab.close"] || enabled["tab.next"] || !enabled["tab.new"] {
		t.Errorf("with one tab only opening one makes sense: %v", enabled)
	}
	if _, ok := enabled["tab.go.2"]; ok {
		t.Error("there is no second tab to go to")
	}

	press(t, m, "T")
	list()
	if !enabled["tab.close"] || !enabled["tab.next"] || !enabled["tab.go.1"] {
		t.Errorf("with two tabs the palette must offer to switch and close: %v", enabled)
	}
	if _, ok := enabled["tab.go.2"]; ok {
		t.Error("the tab in front is not offered as a destination")
	}

	m.runCommand("tab.go.1")
	if m.tabIndex != 0 {
		t.Errorf("the palette entry must switch tab, in tab %d", m.tabIndex)
	}
	m.runCommand("tab.close")
	if len(m.tabs) != 1 {
		t.Errorf("the palette entry must close the tab, %d left", len(m.tabs))
	}
}

func TestNoFrameOverflowsWithSeveralTabsAndPanes(t *testing.T) {
	for _, size := range [][2]int{{60, 12}, {80, 24}, {110, 32}, {200, 50}} {
		m := splitModel(t, size[0], size[1])
		openTabs(t, m, 4)
		openCompanion(t, m, followLogs)
		openCompanion(t, m, followEvents)
		what := itoa(size[0]) + "x" + itoa(size[1]) + " with four tabs"
		assertFits(t, m, what)
		press(t, m, "alt+1")
		assertFits(t, m, what+", first tab")
	}
}

func TestATabIsNamedAfterWhatItsMainPaneShows(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openApplication("payments")
	press(t, m, "T")
	if got := m.tabName(0); !strings.Contains(got, "payments") {
		t.Errorf("tab 1 is on the payments application and must say so, got %q", got)
	}
	if got := m.tabName(1); !strings.Contains(got, "Applications") {
		t.Errorf("tab 2 is on the dashboard, got %q", got)
	}
}

func TestAnApplicationReloadKeepsTheCursorsOfTabsOutOfSight(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "down")
	press(t, m, "down") // api
	hidden := m.panes[0]
	press(t, m, "T")

	loadApplicationsInto(m,
		testApplication("api", application.Down, 0, 3),
		testApplication("payments", application.Healthy, 3, 3),
	)
	if got := cursorOn(m, hidden); got != appKey("api") {
		t.Errorf("a pane in another tab lost its application when the list was re-ranked, it is on %q", got)
	}
}
