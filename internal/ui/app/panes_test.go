package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/layout"
)

// splitModel is a model of the given size with the catalog and a mixed
// dashboard loaded, which is what every pane test starts from. The resize is
// applied at once: a real one is debounced.
func splitModel(t *testing.T, width, height int) *Model {
	t.Helper()
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m.applyLayout()
	loadCatalogInto(m, testCatalog())
	loadApplicationsInto(m, dumpApplications()...)
	return m
}

// openPaneByKey presses a split key and chooses entry id in the picker, the
// way a person would.
func openPaneByKey(t *testing.T, m *Model, key, id string) tea.Cmd {
	t.Helper()
	press(t, m, key)
	if m.overlay != overlaySplit {
		t.Fatalf("%s must open the split picker, got overlay %v", key, m.overlay)
	}
	m.splitPicker.SelectID(id)
	return press(t, m, "enter")
}

// openCompanion opens a companion of the given kind beside the focused pane.
func openCompanion(t *testing.T, m *Model, kind followKind) {
	t.Helper()
	openPaneByKey(t, m, "|", kind.id())
}

// openAnotherView opens a second dashboard that is navigated by hand.
func openAnotherView(t *testing.T, m *Model) {
	t.Helper()
	openPaneByKey(t, m, "|", splitIndependent)
}

// settle delivers the "selection has rested" tick the runtime would deliver
// after followDelay.
func settle(m *Model) tea.Cmd {
	_, cmd := m.Update(followDueMsg{seq: m.followSeq})
	return cmd
}

// cursorOn reports which application pane p's cursor is on.
func cursorOn(m *Model, p *pane) string {
	var key string
	m.withPane(p, func() { key = m.cursorApplicationKey() })
	return key
}

// appKey is the key the dashboard knows an application of the fixtures by.
func appKey(name string) string {
	a := testApplication(name, application.Healthy, 1, 1)
	return a.Key()
}

// loadTableInto opens a pods table in pane p and answers it with rows.
func loadTableInto(t *testing.T, m *Model, p *pane, names ...string) {
	t.Helper()
	m.inPane(p, func() tea.Cmd { return m.openResource("pods") })
	m.Update(paneMsg{pane: p.id, msg: tableLoadedMsg{gen: p.table.Generation(), table: podTablePage(names...)}})
	if p.table.Get() == nil {
		t.Fatalf("pane %d did not accept the table it asked for", p.id)
	}
}

// assertFits fails when a frame is wider or taller than the terminal: the
// terminal would wrap it or scroll, and the screen would tear.
func assertFits(t *testing.T, m *Model, what string) {
	t.Helper()
	lines := strings.Split(xansi.Strip(view(m)), "\n")
	if len(lines) > m.height {
		t.Errorf("%s: the frame is %d lines tall in a %d-line terminal", what, len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("%s: line %d is %d wide in a %d-wide terminal: %q", what, i, w, m.width, line)
			return
		}
	}
}

func TestTheSplitKeyAsksWhatToShowAndEnterOpensIt(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "|")
	if m.overlay != overlaySplit {
		t.Fatal("| must ask what the new pane should show before opening anything")
	}
	if len(m.panes) != 1 {
		t.Error("the picker is a question; no pane may exist before it is answered")
	}
	m.splitPicker.SelectID(followLogs.id())
	press(t, m, "enter")
	if m.overlay != overlayNone || len(m.panes) != 2 {
		t.Fatalf("Enter must open the chosen pane and close the picker, got %d panes, overlay %v", len(m.panes), m.overlay)
	}
	if m.panes[1].follow != followLogs {
		t.Errorf("the new pane follows %v, want the logs that were chosen", m.panes[1].follow)
	}
}

func TestEscapeInTheSplitPickerOpensNothing(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "|")
	press(t, m, "esc")
	if m.overlay != overlayNone || len(m.panes) != 1 {
		t.Errorf("Esc must cancel the split, got %d panes and overlay %v", len(m.panes), m.overlay)
	}
}

func TestTheUnderscoreKeySplitsBelow(t *testing.T) {
	m := splitModel(t, 150, 40)
	openPaneByKey(t, m, "_", followDetails.id())
	if len(m.panes) != 2 || m.split != layout.SplitBelow {
		t.Fatalf("_ must open a pane below, got %d panes, split %v", len(m.panes), m.split)
	}
	a, b := m.panes[0].frame, m.panes[1].frame
	if b.Y <= a.Y || b.X != a.X {
		t.Errorf("the second pane must sit under the first, got %+v under %+v", b, a)
	}
}

func TestThePickerSuggestsTheDiagnosisForABrokenApplicationAndLogsForAHealthyOne(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "|")
	if sel, _ := m.splitPicker.Selected(); sel.ID != followWhy.id() {
		t.Errorf("on a broken application the picker must open on the explanation, got %q", sel.ID)
	}
	press(t, m, "esc")

	press(t, m, "down")
	press(t, m, "down") // payments, worker, then api, which is healthy
	press(t, m, "|")
	if sel, _ := m.splitPicker.Selected(); sel.ID != followLogs.id() {
		t.Errorf("on a healthy application there is nothing to explain; the picker must open on the logs, got %q", sel.ID)
	}
}

func TestACompanionLeavesTheFocusWhereItWas(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	// The point of logs beside a list is to keep walking the list.
	if m.focus != 0 || m.focused().follow != followNone {
		t.Errorf("opening a companion must keep the keyboard in the list, focus is pane %d", m.focus)
	}
}

func TestAnotherViewTakesTheFocus(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	if len(m.panes) != 2 || m.focus != 1 {
		t.Fatalf("a pane with a mind of its own is where the next key goes, got %d panes and focus %d", len(m.panes), m.focus)
	}
	if m.panes[1].view != viewApplications {
		t.Errorf("another view starts on the dashboard, got view %v", m.panes[1].view)
	}
}

func TestAtMostFourPanesAndAFifthSaysSo(t *testing.T) {
	m := splitModel(t, 150, 40)
	for _, kind := range []followKind{followLogs, followWhy, followEvents} {
		openCompanion(t, m, kind)
	}
	if len(m.panes) != maxPanes {
		t.Fatalf("setup: %d panes, want %d", len(m.panes), maxPanes)
	}
	m.message = ""
	press(t, m, "|")
	if m.overlay != overlayNone {
		t.Error("a picker that can only end in a refusal is a wasted question")
	}
	if len(m.panes) != maxPanes {
		t.Errorf("a fifth pane was opened: %d panes", len(m.panes))
	}
	if !strings.Contains(m.message, "Four panes") {
		t.Errorf("the refusal must say why nothing happened, message = %q", m.message)
	}
	press(t, m, "_")
	if len(m.panes) != maxPanes || m.overlay != overlayNone {
		t.Error("splitting below must be refused the same way")
	}
}

func TestTabMovesTheFocusAroundThePanesAndShiftTabBack(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	openCompanion(t, m, followEvents)

	press(t, m, "tab")
	if m.focus != 1 {
		t.Errorf("Tab must move to the next pane, focus = %d", m.focus)
	}
	press(t, m, "tab")
	press(t, m, "tab")
	if m.focus != 0 {
		t.Errorf("Tab must wrap around after the last pane, focus = %d", m.focus)
	}
	press(t, m, "shift+tab")
	if m.focus != 2 {
		t.Errorf("Shift+Tab must walk backwards, wrapping to the last pane, focus = %d", m.focus)
	}
}

func TestTabWithOnePaneSaysThereIsNothingToSwitchTo(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "tab")
	if m.focus != 0 || !strings.Contains(m.message, "One pane") {
		t.Errorf("a key that does nothing must say why, message = %q", m.message)
	}
}

func TestOnePaneDrawsExactlyWhatASessionThatNeverSplitDraws(t *testing.T) {
	never := splitModel(t, 150, 40)
	if strings.Contains(plainView(never), "follows selection") {
		t.Fatal("setup: the never-split frame must not carry a pane title")
	}

	split := splitModel(t, 150, 40)
	openCompanion(t, split, followLogs)
	press(t, split, "tab")
	press(t, split, "X")
	if len(split.panes) != 1 {
		t.Fatalf("setup: %d panes left", len(split.panes))
	}
	// Whoever splits once and closes the pane again must not be left with a
	// title row, a rule or a hint for a pane that is no longer there.
	if got, want := view(split), view(never); got != want {
		t.Errorf("after closing the last companion the frame must equal the never-split frame:\n%s\n--- want ---\n%s",
			xansi.Strip(got), xansi.Strip(want))
	}
}

func TestSeveralPanesEachWearATitleAndTheFocusedOneIsBracketed(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	out := plainView(m)
	for _, want := range []string{"[Applications]", "Why", "follows selection"} {
		if !strings.Contains(out, want) {
			t.Errorf("the pane titles must say %q:\n%s", want, out)
		}
	}
	press(t, m, "tab")
	if out := plainView(m); strings.Contains(out, "[Applications]") || !strings.Contains(out, "[Why") {
		t.Errorf("the brackets must follow the focus, which a monochrome terminal cannot show by colour:\n%s", out)
	}
}

func TestPanesSitSideBySideOnAWideTerminalAndStackedOnANarrowOne(t *testing.T) {
	wide := splitModel(t, 150, 40)
	wide.runPaneCommand("beside:logs")
	a, b := wide.panes[0].frame, wide.panes[1].frame
	if b.X <= a.X || b.Y != a.Y {
		t.Errorf("at 150 columns the panes must be side by side, got %+v and %+v", a, b)
	}

	narrow := splitModel(t, 110, 40)
	narrow.runPaneCommand("beside:logs")
	a, b = narrow.panes[0].frame, narrow.panes[1].frame
	if b.Y <= a.Y || b.X != a.X {
		t.Errorf("below 120 columns two panes side by side would each lose the columns that matter, got %+v and %+v", a, b)
	}
}

func TestWhenNothingFitsOnlyTheFocusedPaneIsDrawnAndTabSwitches(t *testing.T) {
	m := splitModel(t, 60, 12)
	openCompanion(t, m, followDetails)
	if !m.crowded {
		t.Fatal("setup: a 60x12 terminal must have no room for two panes")
	}
	if !strings.Contains(m.message, "No room") {
		t.Errorf("opening a pane that cannot be shown must say so, message = %q", m.message)
	}
	out := plainView(m)
	if !strings.Contains(out, "pane 1 of 2") {
		t.Errorf("the one pane drawn must say that another exists:\n%s", out)
	}
	if strings.Contains(out, "Details") {
		t.Errorf("the pane that does not fit must not be drawn:\n%s", out)
	}
	press(t, m, "tab")
	out = plainView(m)
	if !strings.Contains(out, "pane 2 of 2") || !strings.Contains(out, "Details") {
		t.Errorf("Tab must bring the other pane to the front:\n%s", out)
	}
}

func TestZoomGivesTheFocusedPaneTheWholeBodyAndGivesItBack(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)

	press(t, m, "z")
	out := plainView(m)
	if !m.zoomed || !strings.Contains(out, "zoomed 1 of 2") {
		t.Errorf("a zoomed pane must say it is one of several:\n%s", out)
	}
	if strings.Contains(out, "Logs") {
		t.Errorf("the pane that is not zoomed must not be drawn:\n%s", out)
	}
	press(t, m, "z")
	if m.zoomed || !strings.Contains(plainView(m), "Logs") {
		t.Error("z again must show every pane")
	}
}

func TestZoomDoesNothingWithOnePane(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "z")
	if m.zoomed {
		t.Error("there is nothing to zoom out of with one pane, and the next split would open already zoomed")
	}
}

func TestOpeningAPaneAfterZoomingShowsEveryPaneAgain(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "z")
	openCompanion(t, m, followEvents)
	if m.zoomed {
		t.Error("a pane opened while zoomed would be invisible; opening one must unzoom")
	}
}

func TestXClosesTheFocusedCompanionAndReturnsToTheDriver(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "tab")
	press(t, m, "X")
	if len(m.panes) != 1 || m.focus != 0 {
		t.Errorf("X must close the pane in front, got %d panes and focus %d", len(m.panes), m.focus)
	}
}

func TestClosingAPaneStopsItsLogStream(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	companion := m.panes[1]
	if companion.cancelLogs == nil {
		t.Fatal("setup: the logs companion must have opened a stream")
	}
	press(t, m, "tab")
	press(t, m, "X")
	if companion.cancelLogs != nil {
		t.Error("a closed pane must not leave a connection to the API server running")
	}
}

func TestXWithOnePaneAndOneTabOnlySaysSo(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "X")
	if len(m.panes) != 1 || !strings.Contains(m.message, "only pane") {
		t.Errorf("X must refuse to close the only pane and say why, message = %q", m.message)
	}
}

func TestClosingTheDriverLeavesTheCompanionAPaneOfItsOwn(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "X") // focus is on the list
	if len(m.panes) != 1 {
		t.Fatalf("setup: %d panes left", len(m.panes))
	}
	left := m.panes[0]
	if left.follow != followNone {
		t.Error("with nobody left to follow the survivor must become an ordinary pane")
	}
	if left.view != viewLogs {
		t.Errorf("it keeps what it showed, got view %v", left.view)
	}
	press(t, m, "esc")
	if m.view != viewApplications {
		t.Errorf("Esc in what used to be a companion must walk out of the logs, got view %v", m.view)
	}
}

func TestEscapeInACompanionAtItsRootClosesItAndReturnsToTheDriver(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	press(t, m, "tab")
	if m.focused().follow != followWhy {
		t.Fatal("setup: the focus must be in the companion")
	}
	press(t, m, "esc")
	if len(m.panes) != 1 || m.focus != 0 {
		t.Errorf("there is no screen behind a companion, so Esc closes it; got %d panes, focus %d", len(m.panes), m.focus)
	}
}

func TestLeftAndHDoNothingInACompanionAtItsRoot(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	press(t, m, "tab")
	for _, key := range []string{"left", "h"} {
		press(t, m, key)
		if len(m.panes) != 2 || m.view != viewWhy {
			t.Errorf("%s must not take a companion to a screen nobody opened it on, got view %v with %d panes", key, m.view, len(m.panes))
		}
	}
}

func TestEscapeOnTheDashboardOfTheFirstPaneClosesNothing(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "esc")
	if len(m.panes) != 2 {
		t.Error("the first pane is where the session lives; Esc on its dashboard must not close it or the pane beside it")
	}
}

func TestEscapeOnTheDashboardOfAnotherViewClosesThatPane(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	press(t, m, "esc")
	if len(m.panes) != 1 || m.focus != 0 {
		t.Errorf("the dashboard is where back stops; the next step out is the pane, got %d panes, focus %d", len(m.panes), m.focus)
	}
}

func TestPinningAndUnpinningFromTheKeyboard(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	// The list has the focus; with one companion on screen "pin" can only mean it.
	press(t, m, "P")
	if !m.panes[1].pinned {
		t.Error("P with a single companion must pin it, wherever the focus is")
	}
	if !strings.Contains(plainView(m), "pinned") {
		t.Error("a pinned pane must say so in its title")
	}
	press(t, m, "P")
	if m.panes[1].pinned {
		t.Error("P again must let it follow the selection")
	}
}

func TestPinWithNoCompanionExplainsItself(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "P")
	if !strings.Contains(m.message, "Nothing to pin") {
		t.Errorf("a key with nothing to do must say so, message = %q", m.message)
	}
}

func TestTheIncidentLayoutOpensWhyLogsAndEventsNextToTheList(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "I")
	if len(m.panes) != 4 {
		t.Fatalf("the incident layout is the list and three companions, got %d panes", len(m.panes))
	}
	want := []followKind{followNone, followWhy, followLogs, followEvents}
	for i, kind := range want {
		if m.panes[i].follow != kind {
			t.Errorf("pane %d follows %v, want %v", i, m.panes[i].follow, kind)
		}
	}
	if m.focused().follow != followNone {
		t.Error("the focus must stay in the list: the layout is for reading while walking it")
	}
}

func TestTheIncidentLayoutRefusesWithoutAnApplication(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m)
	press(t, m, "I")
	if len(m.panes) != 1 || !strings.Contains(m.message, "Select an application") {
		t.Errorf("with nothing to lay out around, the key must say so; panes %d, message %q", len(m.panes), m.message)
	}
}

func TestTheIncidentLayoutStartsFromTheDriverWhenAFollowerHasTheFocus(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "tab")
	press(t, m, "I")
	if len(m.panes) != 4 || m.focused().follow != followNone {
		t.Errorf("pressing I from a companion must still give the list and three companions, got %d panes", len(m.panes))
	}
}

func TestNoFrameIsWiderThanTheTerminalOrTallerThanIt(t *testing.T) {
	sizes := [][2]int{{60, 12}, {80, 24}, {110, 32}, {150, 40}, {200, 50}}
	kinds := []followKind{followLogs, followWhy, followEvents}
	for _, size := range sizes {
		for n := 1; n <= maxPanes; n++ {
			for _, zoom := range []bool{false, true} {
				m := splitModel(t, size[0], size[1])
				for _, kind := range kinds[:n-1] {
					openCompanion(t, m, kind)
				}
				if len(m.panes) != n {
					t.Fatalf("setup: %d panes, want %d", len(m.panes), n)
				}
				if zoom {
					press(t, m, "z")
				}
				what := itoa(size[0]) + "x" + itoa(size[1]) + ", " + itoa(n) + " panes"
				if zoom {
					what += ", zoomed"
				}
				assertFits(t, m, what)
				// The pane the focus moves to is drawn too.
				press(t, m, "tab")
				assertFits(t, m, what+", after Tab")
			}
		}
	}
}

func TestAnAnswerAddressedToAPaneLandsInItWhileAnotherHasTheFocus(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	first, second := m.panes[0], m.panes[1]
	loadTableInto(t, m, first, "first-0", "first-1")
	loadTableInto(t, m, second, "second-0")
	// Both panes asked the same number of times: only the address tells them apart.
	if first.table.Generation() != second.table.Generation() {
		t.Fatal("setup: the panes must have the same generation, or this proves nothing")
	}

	m.Update(paneMsg{pane: first.id, msg: tableLoadedMsg{gen: first.table.Generation(), table: podTablePage("a", "b", "c")}})
	if got := len(first.table.Get().Rows); got != 3 {
		t.Errorf("the pane that asked has %d rows, want the 3 that arrived", got)
	}
	if got := len(second.table.Get().Rows); got != 1 {
		t.Errorf("the pane that has the focus has %d rows; an answer for the other pane must not land in it", got)
	}
}

func TestAnObjectAnswerLandsInThePaneThatAskedToo(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	first, second := m.panes[0], m.panes[1]
	ref := objectRef{Kind: "Pod", Name: "api-0", Namespace: "default", Resource: "pods"}
	m.inPane(first, func() tea.Cmd { return m.showObject(ref) })
	m.inPane(second, func() tea.Cmd { return m.showObject(ref) })
	if first.object.Generation() != second.object.Generation() {
		t.Fatal("setup: the panes must have the same generation, or this proves nothing")
	}

	m.Update(paneMsg{pane: first.id, msg: objectLoadedMsg{
		gen: first.object.Generation(), object: &resources.Object{Kind: "Pod", Name: "api-0", Namespace: "default", YAML: "a: b"},
	}})
	if first.object.Get() == nil {
		t.Error("the pane that asked for the object did not get it")
	}
	if second.object.Get() != nil {
		t.Error("the focused pane received the other pane's document")
	}
}

func TestAnAnswerForAClosedPaneIsDroppedWithoutHarm(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	gone := m.panes[1]
	loadTableInto(t, m, gone, "doomed-0")
	press(t, m, "X")
	if len(m.panes) != 1 {
		t.Fatal("setup: the pane must be closed")
	}
	loadTableInto(t, m, m.panes[0], "kept-0")

	for _, msg := range []tea.Msg{
		paneMsg{pane: gone.id, msg: tableLoadedMsg{gen: 1, table: podTablePage("late-0")}},
		paneMsg{pane: gone.id, msg: objectLoadedMsg{gen: 1, object: &resources.Object{}}},
	} {
		m.Update(msg) // must not panic
	}
	if rows := m.panes[0].table.Get().Rows; len(rows) != 1 || rows[0].Name != "kept-0" {
		t.Errorf("a late answer for a closed pane changed another pane's rows: %v", rows)
	}
}

func TestLogAnswersForAClosedPaneAreDropped(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	gone := m.panes[1]
	press(t, m, "X")
	for _, msg := range []tea.Msg{logsStartedMsg{}, logBatchMsg{}} {
		m.Update(paneMsg{pane: gone.id, msg: msg}) // must not panic
	}
	if len(m.panes) != 1 {
		t.Errorf("%d panes", len(m.panes))
	}
}

func TestClosingEveryOtherPaneFromACompanionLeavesAPaneOfItsOwn(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "tab")
	m.runCommand("pane.only")
	if len(m.panes) != 1 || m.panes[0].follow != followNone {
		t.Errorf("with the list gone nothing is left to follow; got %d panes, follow %v", len(m.panes), m.panes[0].follow)
	}
}

func TestAnAnswerWithoutAnAddressGoesToTheFirstPane(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	first, second := m.panes[0], m.panes[1]
	loadTableInto(t, m, first, "first-0")
	loadTableInto(t, m, second, "second-0")
	if m.focus != 1 {
		t.Fatal("setup: the second pane must have the focus")
	}

	// Whatever went out bare was asked for by the first pane, whichever pane
	// the user is in by the time it comes back.
	m.Update(tableLoadedMsg{gen: first.table.Generation(), table: podTablePage("bare-0", "bare-1")})
	if got := len(first.table.Get().Rows); got != 2 {
		t.Errorf("the first pane has %d rows, want the 2 that arrived", got)
	}
	if got := len(second.table.Get().Rows); got != 1 {
		t.Errorf("the focused pane has %d rows; a bare answer is not for it", got)
	}
}

func TestTheFirstPanesCommandsGoOutBare(t *testing.T) {
	cmd := func() tea.Msg { return tableLoadedMsg{gen: 7} }
	got := tagCmd(0, cmd)()
	if _, wrapped := got.(paneMsg); wrapped {
		t.Error("a session that never splits must send exactly the messages it always did")
	}
	if tagCmd(0, nil) != nil || tagCmd(3, nil) != nil {
		t.Error("no command stays no command")
	}
}

func TestOtherPanesCommandsAreAddressedToThem(t *testing.T) {
	got := tagCmd(3, func() tea.Msg { return tableLoadedMsg{gen: 7} })()
	pm, ok := got.(paneMsg)
	if !ok || pm.pane != 3 {
		t.Fatalf("an answer to pane 3 must carry its address, got %#v", got)
	}
	if inner, ok := pm.msg.(tableLoadedMsg); !ok || inner.gen != 7 {
		t.Errorf("the message itself must arrive intact, got %#v", pm.msg)
	}
}

func TestTaggingAnAlreadyAddressedMessageDoesNotAddressItTwice(t *testing.T) {
	got := tagCmd(3, func() tea.Msg { return paneMsg{pane: 5, msg: tableLoadedMsg{}} })()
	if pm, ok := got.(paneMsg); !ok || pm.pane != 5 {
		t.Errorf("a message that already names its pane keeps it, got %#v", got)
	}
}

func TestTaggingAddressesEveryCommandOfABatch(t *testing.T) {
	batch := tea.Batch(
		func() tea.Msg { return tableLoadedMsg{gen: 1} },
		nil,
		func() tea.Msg { return objectLoadedMsg{gen: 2} },
	)
	out, ok := tagCmd(2, batch)().(tea.BatchMsg)
	if !ok {
		t.Fatal("a batch must stay a batch, or its commands never run")
	}
	var seen int
	for _, c := range out {
		if c == nil {
			t.Error("nil commands must be dropped, not carried along")
			continue
		}
		pm, ok := c().(paneMsg)
		if !ok || pm.pane != 2 {
			t.Errorf("every command of a batch must be addressed to pane 2, got %#v", pm)
		}
		seen++
	}
	if seen != 2 {
		t.Errorf("the batch lost commands: %d of 2 left", seen)
	}
}

func TestTaggingLeavesBubbleTeasOwnMessagesAlone(t *testing.T) {
	// Wrapping tea.QuitMsg would turn quitting into a message nobody handles.
	if _, ok := tagCmd(2, tea.Quit)().(tea.QuitMsg); !ok {
		t.Error("tea.Quit must still quit when it comes from a pane other than the first")
	}
	if got := tagCmd(2, func() tea.Msg { return nil })(); got != nil {
		t.Errorf("a command that has nothing to say must keep saying nothing, got %#v", got)
	}
}

func TestReloadingApplicationsKeepsEveryPanesCursorOnItsApplication(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	first, second := m.panes[0], m.panes[1]
	press(t, m, "down") // second pane: worker
	press(t, m, "tab")
	press(t, m, "down")
	press(t, m, "down") // first pane: api
	if got := cursorOn(m, first); got != appKey("api") {
		t.Fatalf("setup: the first pane is on %q", got)
	}
	if got := cursorOn(m, second); got != appKey("worker") {
		t.Fatalf("setup: the second pane is on %q", got)
	}

	// api breaks and payments recovers: the ranking changes under both cursors.
	loadApplicationsInto(m,
		testApplication("api", application.Down, 0, 3),
		testApplication("worker", application.Degraded, 7, 8),
		testApplication("payments", application.Healthy, 3, 3),
		testApplication("frontend", application.Healthy, 6, 6),
	)
	if got := cursorOn(m, first); got != appKey("api") {
		t.Errorf("the first pane's cursor moved to %q when the list was re-ranked; it was on api", got)
	}
	if got := cursorOn(m, second); got != appKey("worker") {
		t.Errorf("the second pane's cursor moved to %q when the list was re-ranked; it was on worker", got)
	}
}

func TestSwitchingNamespaceBlanksCompanionsAndSendsOtherPanesBackToTheDashboard(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	openAnotherView(t, m)
	companion, other := m.panes[1], m.panes[2]
	m.openApplication("payments") // the focused pane, the second dashboard
	if other.view != viewApplication {
		t.Fatal("setup: the other pane must be on an application")
	}

	m.switchNamespace("other")
	if companion.view != viewWhy {
		t.Errorf("a companion keeps its place in a new scope, got view %v", companion.view)
	}
	if !strings.Contains(companion.blank, "Waiting for a selection") {
		t.Errorf("a companion with nothing to show must say it is waiting, not stay empty like a loading pane: %q", companion.blank)
	}
	if companion.subject != "" {
		t.Error("the companion still believes it shows something from the old namespace")
	}
	if other.view != viewApplications {
		t.Errorf("an application of the old namespace must not stay open under the new one, got view %v", other.view)
	}
}

func TestTheWheelScrollsThePaneUnderThePointerWithoutMovingTheFocus(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	names := make([]string, 80)
	for i := range names {
		names[i] = "pod-" + itoa(i)
	}
	loadTableInto(t, m, m.panes[1], names...)
	press(t, m, "tab")
	if m.focus != 0 {
		t.Fatal("setup: the focus must be in the first pane")
	}

	p := m.panes[1]
	m.Update(tea.MouseWheelMsg{X: p.frame.X + 5, Y: p.frame.Y + 4, Button: tea.MouseWheelDown})
	if p.tablePort.Offset == 0 && p.tablePort.Cursor == 0 {
		t.Error("the wheel over the second pane must scroll it")
	}
	if m.focus != 0 {
		t.Error("scrolling is not a request to move the keyboard")
	}
	if m.panes[0].appPort.Offset != 0 || m.panes[0].appPort.Cursor != 0 {
		t.Error("the pane the pointer is not over must not move")
	}
}

func TestAClickInAnotherPaneOnlyMovesTheFocusThere(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	press(t, m, "tab")
	other := m.panes[1]

	// Row three of the list, which is not where the cursor is.
	m.Update(tea.MouseClickMsg{X: other.frame.X + 4, Y: other.frame.Y + 4, Button: tea.MouseLeft})
	if m.focus != 1 {
		t.Errorf("a click in another pane must move the keyboard there, focus = %d", m.focus)
	}
	if other.view != viewApplications || other.appPort.Cursor != 0 {
		t.Errorf("selecting and opening are two deliberate acts; the click also opened or moved something (view %v, cursor %d)",
			other.view, other.appPort.Cursor)
	}
}

func TestAClickInTheFocusedPaneStillSelectsARow(t *testing.T) {
	m := splitModel(t, 150, 40)
	openAnotherView(t, m)
	p := m.focused()
	m.Update(tea.MouseClickMsg{X: p.frame.X + 4, Y: p.frame.Y + 4, Button: tea.MouseLeft})
	if p.appPort.Cursor == 0 {
		t.Error("only a click in another pane is swallowed; in the focused pane it selects as it always did")
	}
}

func TestPaneKeysAreNotActedOnWhileAnOverlayIsOpen(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "ctrl+p")
	if m.overlay != overlayPalette {
		t.Fatal("setup: the palette must be open")
	}
	for _, key := range []string{"|", "_", "X", "z", "T", "I"} {
		press(t, m, key)
	}
	if len(m.panes) != 2 || len(m.tabs) != 1 || m.zoomed || m.overlay != overlayPalette {
		t.Errorf("typing into the palette must only type: %d panes, %d tabs, zoomed %v, overlay %v",
			len(m.panes), len(m.tabs), m.zoomed, m.overlay)
	}
	if got := m.cmdPal.Query(); got != "|_XzTI" {
		t.Errorf("the keys must reach the palette's filter, it holds %q", got)
	}
}

func TestAClickInAnotherPaneIsIgnoredWhileAnOverlayIsOpen(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "ctrl+k")
	other := m.panes[1]
	m.Update(tea.MouseClickMsg{X: other.frame.X + 4, Y: other.frame.Y + 2, Button: tea.MouseLeft})
	if m.focus != 0 {
		t.Error("the overlay owns the pointer; a click behind it must not move the focus")
	}
}

func TestPaneKeysTypedIntoTheFilterAreText(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "/")
	if !m.searching {
		t.Fatal("setup: the filter must have the focus")
	}
	typeInto(t, m, "a|_XT")
	if len(m.panes) != 1 || len(m.tabs) != 1 || m.overlay != overlayNone {
		t.Errorf("| _ X and T are characters while the filter is focused, got %d panes, %d tabs, overlay %v",
			len(m.panes), len(m.tabs), m.overlay)
	}
	if got := m.search.Value(); got != "a|_XT" {
		t.Errorf("the filter holds %q, want what was typed", got)
	}
}

func TestThePaletteListsEveryPaneAndTabCommand(t *testing.T) {
	m := splitModel(t, 150, 40)
	have := map[string]bool{}
	for _, c := range m.registry.Commands() {
		have[c.Category+"/"+c.ID] = true
	}
	for _, id := range []string{
		"Panes/pane.right", "Panes/pane.below", "Panes/pane.incident", "Panes/pane.next",
		"Panes/pane.zoom", "Panes/pane.close", "Panes/pane.only", "Panes/pane.beside.logs",
		"Tabs/tab.new", "Tabs/tab.next", "Tabs/tab.close",
	} {
		if !have[id] {
			t.Errorf("the palette must offer %s: every key has a name, so nobody has to learn the key first", id)
		}
	}
}

func TestPaneCloseIsDisabledWithOnePaneAndEnabledWithTwo(t *testing.T) {
	enabled := func(m *Model, id string) bool {
		for _, c := range m.registry.Commands() {
			if c.ID == id {
				return c.Enabled
			}
		}
		t.Fatalf("no command %s", id)
		return false
	}
	m := splitModel(t, 150, 40)
	if enabled(m, "pane.close") || enabled(m, "pane.next") || enabled(m, "pane.zoom") {
		t.Error("closing, switching and zooming need a second pane; the palette must not offer them to run")
	}
	openCompanion(t, m, followLogs)
	if !enabled(m, "pane.close") || !enabled(m, "pane.next") || !enabled(m, "pane.zoom") {
		t.Error("with two panes the palette must offer to close, switch and zoom")
	}
	press(t, m, "tab")
	press(t, m, "X")
	if enabled(m, "pane.close") {
		t.Error("after closing back to one pane the entry must be disabled again")
	}
}

func TestSplitCommandsAreDisabledAtFourPanes(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "I")
	for _, c := range m.registry.Commands() {
		if (c.ID == "pane.right" || c.ID == "pane.below" || c.ID == "pane.beside.logs") && c.Enabled {
			t.Errorf("%s is enabled with four panes open; running it can only produce a refusal", c.ID)
		}
	}
}

func TestChoosingASplitFromThePaletteOpensThePicker(t *testing.T) {
	m := splitModel(t, 150, 40)
	if cmd := m.runCommand("pane.right"); cmd != nil {
		t.Error("the picker asks a question; nothing is started yet")
	}
	if m.overlay != overlaySplit {
		t.Errorf("the palette entry must open the same picker the key does, got overlay %v", m.overlay)
	}
}

// A filter left open in one pane must not keep swallowing keys there after
// the keyboard has moved on: coming back, "q" has to be a key again, not a
// letter typed into a filter nobody was looking at.
func TestMovingToAnotherPaneLeavesTheFilterButStopsTypingIntoIt(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	loadCatalogInto(m, testCatalog())
	loadApplicationsInto(m, dumpApplications()...)
	m.openPane(followLogs, layout.SplitRight)

	press(t, m, "/")
	press(t, m, "p")
	press(t, m, "tab")

	if m.panes[0].searching {
		t.Fatal("the first pane still takes keys for its filter after the focus left it")
	}
	if got := m.panes[0].search.Value(); got != "p" {
		t.Fatalf("the filter that was typed must stay, got %q", got)
	}
}
