package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
)

func TestALogsCompanionOpensTheLogsOfTheSelectedApplication(t *testing.T) {
	m := splitModel(t, 150, 40)
	cmd := openPaneByKey(t, m, "|", followLogs.id())
	if cmd == nil {
		t.Error("opening a logs companion must start reading the logs")
	}
	c := m.panes[1]
	if c.view != viewLogs || c.logTitle != "Logs of payments" {
		t.Errorf("the companion must show the logs of what the list has selected, got view %v, %q", c.view, c.logTitle)
	}
	if c.cancelLogs == nil {
		t.Error("the companion's stream is open; closing the pane has to be able to cancel it")
	}
	if !strings.Contains(plainView(m), "Logs "+m.theme.Glyphs.Bullet+" payments") {
		t.Errorf("the title must name what the logs are of:\n%s", plainView(m))
	}
}

func TestMovingTheSelectionSchedulesACatchUpAndTheCompanionWaitsForIt(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	before := m.followSeq

	if cmd := press(t, m, "down"); cmd == nil {
		t.Error("a moved selection must return the tick that lets the companion catch up")
	}
	if m.followSeq != before+1 {
		t.Fatalf("followSeq = %d, want a new sequence number for the new selection", m.followSeq)
	}
	if got := m.panes[1].subjectLabel; got != "payments" {
		t.Errorf("the companion followed at once (%q); holding the key down would open a stream per row", got)
	}
	settle(m)
	if got := m.panes[1].subjectLabel; got != "worker" {
		t.Errorf("after the pause the companion must show the new selection, got %q", got)
	}
	if m.panes[1].logTitle != "Logs of worker" {
		t.Errorf("and read its logs, got %q", m.panes[1].logTitle)
	}
}

func TestAStaleFollowTickDoesNothing(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "down")
	stale := m.followSeq
	press(t, m, "down")

	m.Update(followDueMsg{seq: stale})
	if got := m.panes[1].subjectLabel; got != "payments" {
		t.Errorf("a tick for a selection that has been passed already must not move the companion, got %q", got)
	}
	m.Update(followDueMsg{seq: m.followSeq})
	if got := m.panes[1].subjectLabel; got != "api" {
		t.Errorf("the latest tick must catch up with where the selection is now, got %q", got)
	}
}

func TestAKeyThatDoesNotMoveTheSelectionSchedulesNothing(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	generation := m.panes[1].logGeneration
	seq := m.followSeq

	press(t, m, "up") // already on the first row
	settle(m)
	if m.followSeq != seq {
		t.Error("a key that leaves the selection where it is must not start a new catch-up")
	}
	if m.panes[1].logGeneration != generation {
		t.Error("the companion restarted its stream for a selection it already shows")
	}
}

func TestNoCatchUpIsScheduledWithoutACompanion(t *testing.T) {
	m := splitModel(t, 150, 40)
	press(t, m, "down")
	if m.followSeq != 0 {
		t.Error("a session that never splits has nobody to follow the selection")
	}
}

func TestAPinnedCompanionKeepsItsSubjectAndUnpinningCatchesUp(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "P")
	press(t, m, "down")
	settle(m)
	if got := m.panes[1].subjectLabel; got != "payments" {
		t.Errorf("a pinned pane must hold what it shows, got %q", got)
	}

	press(t, m, "P")
	if got := m.panes[1].subjectLabel; got != "worker" {
		t.Errorf("unpinning must catch up with the selection at once, got %q", got)
	}
}

func TestPinningFromTheCompanionItselfWorks(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "tab")
	press(t, m, "P")
	if !m.panes[1].pinned {
		t.Error("P in the companion pins that pane")
	}
}

func TestACompanionHasNothingToReadWhereTheSelectionHasNoContainers(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openResource("nodes")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("node-1", "node-2")})
	openCompanion(t, m, followLogs)

	c := m.panes[1]
	if !strings.Contains(c.blank, "node-1") || !strings.Contains(c.blank, "no containers") {
		t.Errorf("a pane that stays empty reads as one that is still loading; it must say why, blank = %q", c.blank)
	}
	if !strings.Contains(plainView(m), "no containers to read logs from") {
		t.Errorf("the sentence must be on screen:\n%s", plainView(m))
	}
	if c.cancelLogs != nil {
		t.Error("nothing to read, so no stream may be open")
	}
}

func TestACompanionWithNothingSelectedSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m)
	openCompanion(t, m, followLogs)
	if got := m.panes[1].blank; !strings.Contains(got, "Nothing selected") {
		t.Errorf("an empty dashboard gives the companion nothing; it must say so, blank = %q", got)
	}
}

func TestADetailsCompanionFollowsARowOfATable(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("web-0", "web-1")})
	cmd := openPaneByKey(t, m, "|", followDetails.id())
	if cmd == nil {
		t.Error("opening the details of a row must ask the server for the object")
	}
	c := m.panes[1]
	if c.view != viewObject || c.objectTarget.Name != "web-0" || c.objectYAML {
		t.Errorf("the details of the selected row, got view %v, target %q, yaml %v", c.view, c.objectTarget.Name, c.objectYAML)
	}

	press(t, m, "down")
	settle(m)
	if c.objectTarget.Name != "web-1" {
		t.Errorf("walking down the table must walk the details along, got %q", c.objectTarget.Name)
	}
}

func TestAYAMLCompanionShowsTheDocumentNotTheSummary(t *testing.T) {
	m := splitModel(t, 150, 40)
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("web-0")})
	openCompanion(t, m, followYAML)
	c := m.panes[1]
	if c.view != viewObject || !c.objectYAML || c.objectTarget.Name != "web-0" {
		t.Errorf("got view %v, yaml %v, target %q", c.view, c.objectYAML, c.objectTarget.Name)
	}
}

func TestAYAMLCompanionOfAnApplicationHasNoDocumentAndSaysSo(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followYAML)
	if got := m.panes[1].blank; !strings.Contains(got, "Select an object") {
		t.Errorf("an application has no document of its own, blank = %q", got)
	}
}

func TestADetailsCompanionOfAnApplicationShowsWhatItIsMadeOf(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followDetails)
	c := m.panes[1]
	if c.view != viewApplication || c.selectedApp != appKey("payments") {
		t.Errorf("got view %v on %q", c.view, c.selectedApp)
	}
}

func TestAWhyCompanionExplainsTheSelectedApplicationAndFollowsIt(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	c := m.panes[1]
	if c.view != viewWhy || c.selectedApp != appKey("payments") {
		t.Fatalf("got view %v on %q", c.view, c.selectedApp)
	}
	press(t, m, "down")
	settle(m)
	if c.selectedApp != appKey("worker") {
		t.Errorf("the diagnosis must move with the selection, got %q", c.selectedApp)
	}
}

func TestAnEventsCompanionShowsTheScopesActivityWhateverIsSelected(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followEvents)
	c := m.panes[1]
	if c.view != viewActivity {
		t.Fatalf("got view %v", c.view)
	}
	if !strings.Contains(plainView(m), "Events "+m.theme.Glyphs.Bullet+" default") {
		t.Errorf("the title says whose events these are:\n%s", plainView(m))
	}
	press(t, m, "P")
	if c.pinned {
		t.Error("events belong to the scope, not to the selection: there is nothing to pin")
	}
}

func TestACompanionKeepsWhatItHasWhileTheDriverIsOnAScreenWithNoSelection(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	press(t, m, "u")
	if m.view != viewUsage {
		t.Fatalf("setup: the driver must be on the usage screen, got %v", m.view)
	}
	settle(m)
	if got := m.panes[1].subjectLabel; got != "payments" {
		t.Errorf("a screen that selects nothing must not blank the pane beside it, got %q", got)
	}
	if m.panes[1].view != viewLogs || m.panes[1].cancelLogs == nil {
		t.Error("the stream must keep running while the driver is somewhere else for a moment")
	}
}

func TestACompanionFollowsWhicheverPaneWasNavigatedByHandLast(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followWhy)
	openAnotherView(t, m)
	if m.driver() != m.panes[2] {
		t.Fatal("setup: the pane just opened is the one being navigated")
	}
	press(t, m, "down")
	settle(m)
	if got := m.panes[1].selectedApp; got != appKey("worker") {
		t.Errorf("the companion must follow the pane the user is in, got %q", got)
	}

	// Pane 0 is still on payments; giving it the focus makes it the driver again.
	press(t, m, "shift+tab")
	press(t, m, "shift+tab")
	if m.focus != 0 || m.driver() != m.panes[0] {
		t.Fatalf("setup: focus = %d", m.focus)
	}
	press(t, m, "down") // any key: the catch-up compares the subject, not the key
	press(t, m, "up")
	settle(m)
	if got := m.panes[1].selectedApp; got != appKey("payments") {
		t.Errorf("the companion must follow the first list again, got %q", got)
	}
}

func TestAnApplicationReloadMovesCompanionsToTheRerankedSelection(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	loadApplicationsInto(m, dumpApplications()[1:]...) // payments is gone: worker leads
	if m.followSeq == 0 {
		t.Fatal("a reload that changes what is selected must schedule a catch-up")
	}
	settle(m)
	if got := m.panes[1].subjectLabel; got != "worker" {
		t.Errorf("the companion must follow a selection that changed underneath it, got %q", got)
	}
}

func TestACompanionCatchesUpWithTheNewScopeWhenItsApplicationsArrive(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	m.switchNamespace("other")
	if m.panes[1].subject != "" {
		t.Fatal("setup: the companion must have let go of the old namespace")
	}

	loadApplicationsInto(m, testApplication("shop", application.Healthy, 2, 2))
	settle(m)
	c := m.panes[1]
	if c.subjectLabel != "shop" || c.logTitle != "Logs of shop" || c.blank != "" {
		t.Errorf("a companion that said it was waiting must show the first selection of the new scope, got %q, %q, blank %q",
			c.subjectLabel, c.logTitle, c.blank)
	}
}

func TestAMessageThatCannotMoveTheSelectionSchedulesNothing(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followLogs)
	seq := m.followSeq
	// A busy container sends hundreds of these a second.
	m.Update(logBatchMsg{})
	m.Update(paneMsg{pane: m.panes[1].id, msg: logBatchMsg{}})
	if m.followSeq != seq {
		t.Error("log batches must not wake the catch-up logic")
	}
	if !movesSelection(paneMsg{pane: 1, msg: tea.KeyPressMsg{}}) {
		t.Error("a key addressed to a pane can still move its selection")
	}
}

func TestACompanionAtItsRootIsRecognisedAndAListIsNot(t *testing.T) {
	m := splitModel(t, 150, 40)
	openCompanion(t, m, followDetails)
	press(t, m, "tab")
	if !m.atFollowRoot() {
		t.Error("a companion showing what it was opened for is at its root")
	}
	press(t, m, "tab")
	if m.atFollowRoot() {
		t.Error("the list is not a companion: Esc there goes back as it always did")
	}
}
