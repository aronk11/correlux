package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// noticeTTL is how long a transient status message stays on screen.
const noticeTTL = 5 * time.Second

// busyGrace is how long a timed reload may run before the header admits to it.
//
// Below this the reload is over before the eye has settled on the word, and
// all the indicator conveys is that something blinked. Above it the screen has
// gone quiet for long enough that silence would read as a hang.
const busyGrace = 400 * time.Millisecond

// Update is the single place where state changes. It must never block: any
// work that can take time is returned as a command.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleResize(msg)

	case resizeSettledMsg:
		run, retry := m.resize.Ready(time.Now())
		if !run {
			if retry > 0 {
				return m, scheduleResize(retry)
			}
			return m, nil
		}
		m.applyLayout()
		return m, nil

	case tea.BackgroundColorMsg:
		// The terminal told us its real background; rebuild the theme so the
		// palette matches instead of guessing from environment variables.
		dark := msg.IsDark()
		if dark != m.caps.Dark {
			m.caps.Dark = dark
			m.theme = theme.New(m.caps, m.cfg.Theme)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.MouseWheelMsg:
		return m, m.handleWheel(msg)

	case tea.MouseClickMsg:
		return m, m.handleClick(msg)

	case autoRefreshTickMsg:
		return m, m.handleAutoRefreshTick(msg)

	case clusterProbedMsg:
		if m.cluster.Accepts(msg.gen) {
			m.clusterLoading = false
		}
		if m.cluster.Succeed(msg.gen, msg.info) && msg.info.State != kubeclient.ConnOK {
			m.notice(connectionNotice(msg.info), theme.StatusCritical)
			return m, m.expireNotice()
		}
		return m, nil

	case namespacesLoadedMsg:
		if msg.err != nil {
			m.namespaces.Fail(msg.gen, msg.err)
		} else if m.namespaces.Succeed(msg.gen, msg.list) {
			m.rebuildCommands()
		}
		m.nsPicker.Refresh()
		return m, nil

	case catalogLoadedMsg:
		if msg.err != nil {
			m.catalog.Fail(msg.gen, msg.err)
		} else if m.catalog.Succeed(msg.gen, msg.catalog) {
			m.rebuildCommands()
			if msg.catalog.Partial() {
				m.notice(itoa(len(msg.catalog.Failures))+" API group(s) could not be discovered; the rest is usable",
					theme.StatusWarning)
				m.resPicker.Refresh()
				return m, m.expireNotice()
			}
		}
		m.resPicker.Refresh()
		return m, nil

	case applicationsLoadedMsg:
		return m, m.applyApplications(msg)

	case fleetStartedMsg:
		if msg.gen != m.fleetGeneration {
			return m, nil
		}
		m.fleetResults = msg.results
		return m, waitForFleet(msg.gen, msg.results)

	case fleetMemberMsg:
		return m, m.applyFleetMember(msg)

	case fleetPartsMsg:
		if msg.gen != m.fleetGeneration {
			return m, nil
		}
		m.fleetPartsChan = msg.parts
		return m, waitForFleetPart(msg.gen, msg.parts)

	case fleetPartMsg:
		return m, m.applyFleetPart(msg)

	case logsStartedMsg:
		if msg.gen != m.logGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.logErr = msg.err
			return m, nil
		}
		m.logStream = msg.stream
		return m, waitForLogs(msg.gen, msg.stream)

	case logBatchMsg:
		return m, m.applyLogBatch(msg)

	case objectLoadedMsg:
		if m.object.Accepts(msg.gen) {
			m.objectLoading = false
		}
		if msg.err != nil {
			m.object.Fail(msg.gen, msg.err)
			return m, nil
		}
		m.object.Succeed(msg.gen, msg.object)
		return m, nil

	case usageLoadedMsg:
		m.applyUsage(msg)
		return m, nil

	case scaledMsg:
		return m, m.applyScaled(msg)

	case execEndedMsg:
		return m, m.applyExecEnded(msg)

	case editFinishedMsg:
		return m, m.applyEditedBuffer(msg)

	case editedMsg:
		return m, m.applyEdited(msg)

	case evidenceLoadedMsg:
		if m.evidence.Accepts(msg.gen) {
			m.evidenceLoading = false
		}
		if msg.err != nil {
			m.evidence.Fail(msg.gen, msg.err)
			return m, nil
		}
		if m.evidence.Succeed(msg.gen, msg.context) {
			// The explanation improves as soon as the evidence lands.
			m.rediagnose()
		}
		return m, nil

	case tableLoadedMsg:
		return m, m.applyTablePage(msg)

	case kubeconfigReloadedMsg:
		return m, m.applyReloadedKubeconfig(msg)

	case messageExpiredMsg:
		if msg.seq == m.messageSeq {
			m.message = ""
			m.messageStatus = theme.StatusUnknown
		}
		return m, nil

	case busyAdmittedMsg:
		// Only if the burst this was scheduled for is the current one and is
		// still running. A reload that landed inside the grace period leaves
		// the header alone entirely.
		if msg.seq == m.busySeq && m.busy() {
			m.busyShown = true
		}
		return m, nil
	}
	return m, nil
}

// handleResize coalesces resize bursts: dragging a window emits dozens of
// events per second and re-laying out on each one tears the frame.
func (m *Model) handleResize(msg tea.WindowSizeMsg) tea.Cmd {
	m.width, m.height = msg.Width, msg.Height
	if !m.ready {
		// The first size must be applied immediately or there is nothing to
		// draw at all.
		m.ready = true
		m.applyLayout()
		return nil
	}
	if m.resize.Observe(time.Now()) {
		return scheduleResize(m.resize.Interval())
	}
	return nil
}

func (m *Model) applyLayout() {
	m.screen = layout.Compute(m.width, m.height)
}

// applyTablePage stores a page of results, appending it when it continues the
// current table rather than replacing it.
func (m *Model) applyTablePage(msg tableLoadedMsg) tea.Cmd {
	if msg.append {
		m.loadingMore = false
	} else if m.table.Accepts(msg.gen) {
		m.tableLoading = false
	}
	if msg.err != nil {
		if !msg.append {
			if m.table.Fail(msg.gen, msg.err) {
				m.refreshFailures++
			}
			return nil
		}
		// A failed continuation leaves the rows we already have on screen.
		m.notice("Could not load more rows: "+shortError(msg.err), theme.StatusWarning)
		return m.expireNotice()
	}

	if msg.append {
		current := m.table.Get()
		if current == nil || !m.table.Accepts(msg.gen) {
			return nil
		}
		merged := *msg.table
		merged.Rows = append(append([]resources.Row(nil), current.Rows...), msg.table.Rows...)
		if len(merged.Columns) == 0 {
			merged.Columns = current.Columns
		}
		m.table.Succeed(msg.gen, &merged)
		return nil
	}

	previous := m.cursorRowKey()
	if !m.table.Succeed(msg.gen, msg.table) {
		return nil
	}
	m.refreshFailures = 0
	m.keepCursorOnRow(previous)
	return nil
}

// cursorRowKey identifies the row under the cursor, so a reload can put the
// cursor back on the object rather than on the row number.
func (m *Model) cursorRowKey() string {
	rows := m.tableRows()
	if m.tablePort.Cursor < 0 || m.tablePort.Cursor >= len(rows) {
		return ""
	}
	return rows[m.tablePort.Cursor].Namespace + "/" + rows[m.tablePort.Cursor].Name
}

// keepCursorOnRow restores the cursor after a table was replaced. A refresh
// re-lists the resource, and objects come and go between two lists: following
// the name keeps the selection where the user left it, and a deleted object
// leaves the cursor at the same place in the list rather than at the top.
func (m *Model) keepCursorOnRow(previous string) {
	rows := m.visibleRows()
	found := -1
	if previous != "" {
		for i := range rows {
			if rows[i].Namespace+"/"+rows[i].Name == previous {
				found = i
				break
			}
		}
	}
	m.tablePort.KeepCursor(found, len(rows), m.rowsPerScreen())
}

// handleAutoRefreshTick runs one timed reload and schedules the next.
//
// Three rules keep it cheap enough to leave running on a large cluster: it
// reloads only what the current screen shows, it never starts a request while
// the previous one is still in flight, and it does nothing at all while an
// overlay has the user's attention.
func (m *Model) handleAutoRefreshTick(msg autoRefreshTickMsg) tea.Cmd {
	if !m.autoRefresh || msg.seq != m.refreshSeq {
		// A ticker from a toggle the user has since undone.
		return nil
	}
	cmds := []tea.Cmd{scheduleAutoRefresh(m.refreshSeq, m.autoRefreshDelay())}
	if m.overlay == overlayNone {
		if reload := m.autoReload(); len(reload) > 0 {
			// The timer announces itself only if it turns out to be slow, so a
			// cluster that answers quickly leaves the header still.
			cmds = append(cmds, m.beginBusy(false))
			cmds = append(cmds, reload...)
		}
	}
	return tea.Batch(cmds...)
}

// autoReload refetches what the current view is showing, and only that. The
// resource catalog and the namespace list do not change on the timescale of a
// refresh interval, and re-discovering every API group every ten seconds would
// cost far more than the screen is worth.
func (m *Model) autoReload() []tea.Cmd {
	switch m.view {
	case viewApplications, viewApplication, viewWhy:
		if m.appsLoading {
			return nil
		}
		cmds := []tea.Cmd{m.loadApplications()}
		// The explanation is only worth refreshing where it is being read.
		if m.view == viewWhy && !m.evidenceLoading {
			cmds = append(cmds, m.loadEvidence())
		}
		return cmds
	case viewActivity:
		if m.evidenceLoading {
			return nil
		}
		return []tea.Cmd{m.loadEvidence()}
	case viewUsage:
		// Two requests a tick — the pods, and the nodes with the metrics — and
		// only while somebody is looking at this screen. The dashboard's own
		// timer never pays for them.
		if m.appsLoading || m.usageLoading {
			return nil
		}
		return []tea.Cmd{m.loadApplications(), m.loadUsage()}
	case viewTable:
		// Only the first page is refreshed. Someone who has paged deep into a
		// large resource would otherwise watch their rows disappear every tick.
		if m.tableLoading || m.loadingMore || m.tablePaged() {
			return nil
		}
		return []tea.Cmd{m.loadTable()}
	case viewOverview:
		if m.clusterLoading {
			return nil
		}
		return []tea.Cmd{m.probeCluster()}
	}
	return nil
}

// tablePaged reports whether more than the first page has been loaded.
func (m *Model) tablePaged() bool {
	table := m.table.Get()
	return table != nil && len(table.Rows) > int(resources.DefaultPageSize)
}

// autoRefreshDelay backs off after failures: a cluster that is unreachable
// stays unreachable for a while, and polling it every ten seconds only fills
// the screen with the same error faster.
func (m *Model) autoRefreshDelay() time.Duration {
	delay := m.refreshEvery
	for i := 0; i < m.refreshFailures && i < 3; i++ {
		delay *= 2
	}
	return delay
}

// applyApplications stores a freshly grouped dashboard, keeping the cursor on
// the application it was on rather than on the row number it was at.
func (m *Model) applyApplications(msg applicationsLoadedMsg) tea.Cmd {
	if m.apps.Accepts(msg.gen) {
		m.appsLoading = false
	}
	if msg.err != nil {
		if m.apps.Fail(msg.gen, msg.err) {
			m.refreshFailures++
			m.notice(applicationsNotice(msg.err), theme.StatusCritical)
			return m.expireNotice()
		}
		return nil
	}
	previous := m.cursorApplicationKey()
	if !m.apps.Succeed(msg.gen, msg.list) {
		return nil
	}
	m.refreshFailures = 0
	m.keepCursorOnApplication(previous)
	m.rediagnose()
	// The usage view is built on this snapshot; a reload of one is a reload of
	// the other.
	m.recomputeUsage()
	m.rebuildCommands()

	if m.pendingApplication != "" {
		// Chosen in the fleet view, before this cluster had answered.
		name := m.pendingApplication
		m.pendingApplication = ""
		return m.openApplication(name)
	}
	if !m.pendingObject.empty() {
		ref := m.pendingObject
		m.pendingObject = objectRef{}
		return m.openObject(ref)
	}
	return nil
}

// tableRows returns the rows currently loaded.
func (m *Model) tableRows() []resources.Row {
	if t := m.table.Get(); t != nil {
		return t.Rows
	}
	return nil
}

// moveTableCursor scrolls the table, fetching the next page when the user
// reaches the end of what is loaded.
func (m *Model) moveTableCursor(delta int) tea.Cmd {
	rows := m.visibleRows()
	m.tablePort.MoveCursor(delta, len(rows), m.rowsPerScreen())

	// Prefetch the next page a screen before the end, so scrolling stays smooth
	// on a resource with thousands of objects.
	if len(rows) > 0 && m.tablePort.Cursor >= len(rows)-m.rowsPerScreen() {
		return m.loadMoreRows()
	}
	return nil
}

// rowsPerScreen is how many rows fit under a table's column header.
func (m *Model) rowsPerScreen() int { return max(m.screen.Body.Height-1, 1) }

// bodyHeight is how many lines a view that has no header may draw.
func (m *Model) bodyHeight() int { return max(m.screen.Body.Height, 1) }

// handleTableKey handles the keys that only exist in the resource browser.
func (m *Model) handleTableKey(keystroke string) (tea.Cmd, bool) {
	visible := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		return m.moveTableCursor(-1), true
	case "down", "j":
		return m.moveTableCursor(1), true
	case "pgup":
		return m.moveTableCursor(-visible), true
	case "pgdown", " ":
		return m.moveTableCursor(visible), true
	case "home", "g":
		return m.moveTableCursor(-len(m.visibleRows())), true
	case "end", "G":
		return m.moveTableCursor(len(m.visibleRows())), true
	case "enter", "right":
		return m.openSelectedRow(), true
	}
	return nil, false
}

// handleApplicationsKey handles the dashboard's own keys.
func (m *Model) handleApplicationsKey(keystroke string) (tea.Cmd, bool) {
	visible := m.applicationsVisible()
	switch keystroke {
	case "up", "k":
		m.moveAppCursor(-1)
	case "down", "j":
		m.moveAppCursor(1)
	case "pgup":
		m.moveAppCursor(-visible)
	case "pgdown", " ":
		m.moveAppCursor(visible)
	case "home", "g":
		m.moveAppCursor(-len(m.visibleApplications()))
	case "end", "G":
		m.moveAppCursor(len(m.visibleApplications()))
	case "enter", "right":
		return m.openSelectedApplication(), true
	default:
		return nil, false
	}
	return nil, true
}

// handleApplicationKey moves through the objects an application is made of.
//
// Up and down move the selection rather than the viewport, because the point of
// the screen is to pick something and open it; the viewport follows the
// selection, and scrolls on its own once there is nothing left to select.
func (m *Model) handleApplicationKey(keystroke string) (tea.Cmd, bool) {
	page := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.moveDetailCursor(-1)
	case "down", "j":
		m.moveDetailCursor(1)
	case "pgup":
		m.scrollDetail(-page)
	case "pgdown", " ":
		m.scrollDetail(page)
	case "home", "g":
		m.detailPort.Cursor, m.detailPort.Offset = 0, 0
	case "end", "G":
		m.scrollDetail(m.detailLines())
	case "enter", "right":
		return m.openSelectedObject(), true
	case "left", "h":
		return m.backToApplications(), true
	default:
		return nil, false
	}
	return nil, true
}

// moveDetailCursor moves the selection, and scrolls instead when the selection
// has nowhere left to go.
func (m *Model) moveDetailCursor(delta int) {
	data, targets := m.applicationView()
	m.detailPort.MoveTarget(delta, data.TargetLines(m.screen.Body.Width), len(targets),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

// openSelectedObject opens whatever the detail cursor is on.
func (m *Model) openSelectedObject() tea.Cmd {
	_, targets := m.applicationView()
	if m.detailPort.Cursor < 0 || m.detailPort.Cursor >= len(targets) {
		return nil
	}
	return m.openObject(targets[m.detailPort.Cursor])
}

// handleFleetKey moves through the clusters and what is broken in them.
func (m *Model) handleFleetKey(keystroke string) (tea.Cmd, bool) {
	page := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.moveFleetCursor(-1)
	case "down", "j":
		m.moveFleetCursor(1)
	case "pgup":
		m.scrollFleet(-page)
	case "pgdown", " ":
		m.scrollFleet(page)
	case "home", "g":
		m.fleetPort.Cursor, m.fleetPort.Offset = 0, 0
	case "end", "G":
		m.scrollFleet(m.fleetData().LineCount(m.screen.Body.Width))
	case "enter", "right":
		return m.enterFleetRow(), true
	default:
		// The edit key opens the picker rather than an object: on this screen
		// the thing worth editing is which clusters are on it.
		if keystroke == m.keys.Key(ActionEdit) {
			return m.openFleetPicker(m.activeFleetGroup), true
		}
		return nil, false
	}
	return nil, true
}

// moveFleetCursor moves the selection, scrolling when it has nowhere to go.
func (m *Model) moveFleetCursor(delta int) {
	data := m.fleetData()
	m.fleetPort.MoveTarget(delta, data.TargetLines(m.screen.Body.Width), len(m.fleetTargets()),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

// scrollFleet moves the viewport and drags the selection along.
func (m *Model) scrollFleet(delta int) {
	data := m.fleetData()
	m.fleetPort.ScrollTargets(delta, data.TargetLines(m.screen.Body.Width), len(m.fleetTargets()),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

// handleFleetResourceKey moves through one kind across the fleet.
func (m *Model) handleFleetResourceKey(keystroke string) (tea.Cmd, bool) {
	visible := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.moveFleetTableCursor(-1)
	case "down", "j":
		m.moveFleetTableCursor(1)
	case "pgup":
		m.scrollFleetTable(-visible)
	case "pgdown", " ":
		m.scrollFleetTable(visible)
	case "home", "g":
		m.moveFleetTableCursor(-len(m.visibleFleetRows()))
	case "end", "G":
		m.moveFleetTableCursor(len(m.visibleFleetRows()))
	case "enter", "right":
		return m.openFleetRow(), true
	case "esc", "left", "h":
		return m.openFleet(), true
	default:
		return nil, false
	}
	return nil, true
}

// handleLogsKey drives the log view.
func (m *Model) handleLogsKey(keystroke string) (tea.Cmd, bool) {
	page := max(m.screen.Body.Height-3, 1)
	switch keystroke {
	case "up", "k":
		m.scrollLogs(-1)
	case "down", "j":
		m.scrollLogs(1)
	case "pgup":
		m.scrollLogs(-page)
	case "pgdown", " ":
		m.scrollLogs(page)
	case "home", "g":
		m.logFollow = false
		m.logPort.Offset = 0
	case "end", "G":
		m.logFollow = true
		m.scrollLogs(0)
	case "esc", "left", "h":
		return m.closeLogs(), true
	default:
		return nil, false
	}
	return nil, true
}

// handleObjectKey moves through one object's relations, and its document.
func (m *Model) handleObjectKey(keystroke string) (tea.Cmd, bool) {
	page := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.moveObjectCursor(-1)
	case "down", "j":
		m.moveObjectCursor(1)
	case "pgup":
		m.scrollObject(-page)
	case "pgdown", " ":
		m.scrollObject(page)
	case "home", "g":
		m.objectPort.Cursor, m.objectPort.Offset = 0, 0
	case "end", "G":
		m.scrollObject(m.objectLines())
	case "enter", "right":
		return m.openSelectedRelation(), true
	case "esc", "left", "h":
		return m.backFromObject(), true
	default:
		return nil, false
	}
	return nil, true
}

// moveObjectCursor moves through the related objects; in the YAML view there is
// nothing to select, so it scrolls.
func (m *Model) moveObjectCursor(delta int) {
	data, targets := m.objectView()
	if m.objectYAML {
		// The document has nothing to select; the keys scroll it.
		m.scrollObject(delta)
		return
	}
	m.objectPort.MoveTarget(delta, data.TargetLines(m.screen.Body.Width), len(targets),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

// openSelectedRelation follows the owner or child under the cursor.
func (m *Model) openSelectedRelation() tea.Cmd {
	_, targets := m.objectView()
	if m.objectPort.Cursor < 0 || m.objectPort.Cursor >= len(targets) {
		return nil
	}
	return m.openObject(targets[m.objectPort.Cursor])
}

func (m *Model) objectLines() int {
	return m.objectData().LineCount(m.screen.Body.Width)
}

// scrollObject moves the object viewport, dragging the selection along.
func (m *Model) scrollObject(delta int) {
	data, targets := m.objectView()
	m.objectPort.ScrollTargets(delta, data.TargetLines(m.screen.Body.Width), len(targets),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

// handleWhyKey scrolls the explanation. Nothing here needs to fetch anything:
// the explanation is already in the model.
func (m *Model) handleWhyKey(keystroke string) bool {
	page := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.scrollWhy(-1)
	case "down", "j":
		m.scrollWhy(1)
	case "pgup":
		m.scrollWhy(-page)
	case "pgdown", " ":
		m.scrollWhy(page)
	case "home", "g":
		m.whyPort.Offset = 0
	case "end", "G":
		m.scrollWhy(m.whyLines())
	case "left", "h", "enter":
		// Enter goes to the objects the explanation is about.
		m.view = viewApplication
		m.detailPort.Offset = 0
		m.rebuildCommands()
	default:
		return false
	}
	return true
}

// detailLines is how many lines the open application renders to, which is what
// bounds scrolling.
func (m *Model) detailLines() int {
	return m.applicationData().LineCount(m.screen.Body.Width)
}

// scrollDetail moves the detail viewport, never past the last screenful — a
// view scrolled into empty space looks broken — and drags the selection along
// with it.
//
// Without that last part the page snaps back to the selection the moment an
// arrow key is pressed, which reads as "scrolling does not work here".
func (m *Model) scrollDetail(delta int) {
	data, targets := m.applicationView()
	m.detailPort.ScrollTargets(delta, data.TargetLines(m.screen.Body.Width), len(targets),
		data.LineCount(m.screen.Body.Width), m.bodyHeight())
}

func clampInt(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.Key()
	keystroke := key.Keystroke()

	// Overlays get first refusal, so typing "q" into a filter does not quit.
	if m.overlay != overlayNone {
		if cmd, handled := m.handleOverlayKey(keystroke, key.Text); handled {
			return cmd
		}
	}

	// The filter takes every key while it has focus, for the same reason: a
	// resource named "w" must be typeable.
	if m.overlay == overlayNone && m.searching {
		if m.handleSearchKey(keystroke, key.Text) {
			return nil
		}
	}

	if m.overlay == overlayNone {
		switch m.view {
		case viewTable:
			if cmd, handled := m.handleTableKey(keystroke); handled {
				return cmd
			}
		case viewApplications:
			if cmd, handled := m.handleApplicationsKey(keystroke); handled {
				return cmd
			}
		case viewApplication:
			if cmd, handled := m.handleApplicationKey(keystroke); handled {
				return cmd
			}
		case viewWhy:
			if m.handleWhyKey(keystroke) {
				return nil
			}
		case viewObject:
			if cmd, handled := m.handleObjectKey(keystroke); handled {
				return cmd
			}
		case viewLogs:
			if cmd, handled := m.handleLogsKey(keystroke); handled {
				return cmd
			}
		case viewUsage:
			if cmd, handled := m.handleUsageKey(keystroke); handled {
				return cmd
			}
		case viewActivity:
			if cmd, handled := m.handleActivityKey(keystroke); handled {
				return cmd
			}
		case viewFleet:
			if cmd, handled := m.handleFleetKey(keystroke); handled {
				return cmd
			}
		case viewFleetResource:
			if cmd, handled := m.handleFleetResourceKey(keystroke); handled {
				return cmd
			}
		}
	}

	action, ok := m.keys.Action(keystroke)
	if !ok {
		return nil
	}
	switch action {
	case ActionQuit:
		m.quitting = true
		return tea.Quit
	case ActionHelp:
		return m.openOverlay(overlayHelp)
	case ActionPalette:
		return m.openOverlay(overlayPalette)
	case ActionContextPicker:
		return m.openOverlay(overlayContexts)
	case ActionNamespacePicker:
		return m.openOverlay(overlayNamespaces)
	case ActionResourcePicker:
		return m.openOverlay(overlayResources)
	case ActionToggleWide:
		// Both table-shaped views hide the same secondary columns, and one
		// toggle for both is one thing to learn instead of two.
		switch m.view {
		case viewTable, viewApplications, viewFleetResource:
			return m.toggleWide()
		case viewLogs:
			// The same question — show me the part that is cut off — asked of
			// text rather than of columns.
			return m.toggleWrap()
		}
		return nil
	case ActionLogs:
		if m.view == viewLogs {
			return m.closeLogs()
		}
		return m.openLogs()
	case ActionUsage:
		return m.openUsage()
	case ActionActivity:
		return m.openActivity()
	case ActionRefresh:
		return m.refresh()
	case ActionAutoRefresh:
		return m.toggleAutoRefresh()
	case ActionClose:
		if m.overlay != overlayNone {
			m.closeOverlay()
			return nil
		}
		if m.filtering() {
			// Esc undoes the filter first; leaving the view is the next press.
			m.clearSearch()
			return nil
		}
		return m.backToApplications()
	case ActionApplications:
		return m.backToApplications()
	case ActionSearch:
		return m.startSearch()
	case ActionFleet:
		if m.view == viewFleet {
			m.stopFleet()
			return m.backToApplications()
		}
		return m.openFleet()
	case ActionWhy:
		return m.explain()
	case ActionGrouping:
		return m.toggleGrouping()
	case ActionYAML:
		if m.view == viewObject {
			m.toggleObjectYAML()
		}
		return nil
	case ActionDecode:
		if m.view == viewObject {
			m.toggleObjectDecode()
		}
		return nil
	case ActionScale:
		return m.scaleTarget()
	case ActionEdit:
		if m.view == viewObject {
			return m.editObject(m.objectTarget)
		}
		return nil
	case ActionExec:
		return m.openExec()
	case ActionCopy:
		return m.copyPrimary()
	case ActionFollow:
		if m.view == viewLogs {
			return m.toggleFollow()
		}
		return nil
	case ActionTimestamps:
		if m.view == viewLogs {
			return m.toggleLogTimestamps()
		}
		return nil
	case ActionPrevious:
		if m.view == viewLogs {
			return m.togglePrevious()
		}
		return nil

	}
	return nil
}

// wheelStep is how far one notch of the wheel moves. Three lines is what
// terminals and editors have settled on; one is sluggish on a long list.
const wheelStep = 3

// handleWheel scrolls whatever is under the pointer: the open overlay if there
// is one, otherwise the view itself. A list that cannot be scrolled with the
// wheel feels broken in a terminal that has one.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := wheelStep
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		delta = -wheelStep
	case tea.MouseWheelDown:
	default:
		// Horizontal wheels exist; Correlux has nothing to scroll sideways.
		return nil
	}

	if m.overlay != overlayNone {
		// The overlay owns the pointer while it is open, even the help, which
		// has nothing to scroll: the view behind it must not move.
		if sel := m.activeSelector(); sel != nil {
			sel.ScrollBy(delta)
		}
		return nil
	}
	switch m.view {
	case viewTable:
		return m.scrollTable(delta)
	case viewApplications:
		m.scrollApplications(delta)
	case viewApplication:
		m.scrollDetail(delta)
	case viewWhy:
		m.scrollWhy(delta)
	case viewObject:
		m.scrollObject(delta)
	case viewFleet:
		m.scrollFleet(delta)
	case viewFleetResource:
		m.scrollFleetTable(delta)
	case viewLogs:
		m.scrollLogs(delta)
	case viewUsage:
		m.scrollUsage(delta)
	}
	return nil
}

// scrollTable moves the viewport rather than the selection, and drags the
// cursor along only when it would otherwise scroll off the screen — the row a
// user picked must not change because they looked further down the list.
func (m *Model) scrollTable(delta int) tea.Cmd {
	rows := m.visibleRows()
	m.tablePort.ScrollRows(delta, len(rows), m.rowsPerScreen())

	// Scrolling towards the end pulls the next page in, exactly as the keys do.
	if len(rows) > 0 && m.tablePort.Offset+m.rowsPerScreen() >= len(rows) {
		return m.loadMoreRows()
	}
	return nil
}

// scrollApplications moves the dashboard viewport.
func (m *Model) scrollApplications(delta int) {
	m.appPort.ScrollRows(delta, len(m.visibleApplications()), m.rowsPerScreen())
}

func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	sel := m.activeSelector()
	if sel == nil || msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	rect := m.overlayRect()
	// The list starts after the border, the title and the input line.
	listTop := rect.Y + 1 + 2
	row := msg.Mouse().Y - listTop
	if row < 0 || msg.Mouse().X < rect.X || msg.Mouse().X >= rect.X+rect.Width {
		return nil
	}
	if sel.ClickRow(row) {
		return m.confirmSelection()
	}
	return nil
}

// applyReloadedKubeconfig swaps in a freshly read kubeconfig, keeping the
// session on its current context when that context still exists.
func (m *Model) applyReloadedKubeconfig(msg kubeconfigReloadedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Reload failed: "+kubeclient.FriendlyError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.kubeconfig = msg.cfg
	m.factory.Reset()

	if _, ok := m.kubeconfig.Context(m.contextName); !ok {
		name, err := m.kubeconfig.ResolveStartContext("", "")
		if err != nil {
			m.notice("Active context disappeared from the kubeconfig", theme.StatusCritical)
			m.rebuildCommands()
			return m.expireNotice()
		}
		m.rebuildCommands()
		return m.switchContext(name)
	}

	m.rebuildCommands()
	m.ctxPicker.Refresh()
	m.notice("Kubeconfig reloaded", theme.StatusHealthy)
	return tea.Batch(m.probeCluster(), m.loadNamespaces(), m.expireNotice())
}

// notice sets the transient status message.
func (m *Model) notice(text string, status theme.Status) {
	m.message = text
	m.messageStatus = status
	m.messageSeq++
}

// expireNotice schedules the current message to disappear.
func (m *Model) expireNotice() tea.Cmd { return expireMessage(m.messageSeq, noticeTTL) }

// connectionNotice renders a failed probe as one actionable line.
func connectionNotice(info kubeclient.ClusterInfo) string {
	msg := "Cluster " + info.State.String()
	if info.Hint != "" {
		return msg + ": " + info.Hint
	}
	if info.Err != nil {
		return msg + ": " + kubeclient.FriendlyError(info.Err)
	}
	return msg
}

func shortError(err error) string { return kubeclient.FriendlyError(err) }

// busy reports whether anything the screen is showing is being reloaded.
func (m *Model) busy() bool {
	return m.appsLoading || m.evidenceLoading || m.objectLoading ||
		m.tableLoading || m.clusterLoading || m.usageLoading
}

// beginBusy starts a burst of reloading and decides when the header may say
// so: at once for something the user asked for, after a grace period for the
// timer, which is the difference between acknowledging a keystroke and
// narrating a background job.
func (m *Model) beginBusy(announce bool) tea.Cmd {
	m.busySeq++
	m.busyShown = announce
	if announce {
		return nil
	}
	return admitBusy(m.busySeq, busyGrace)
}

// busyLabel is what the header says while a reload is in flight, and nothing
// at all once it has landed. There is no timer holding the word on screen
// after the work is done: an indicator that outlives its work is a claim that
// the work took longer than it did.
func (m *Model) busyLabel() string {
	if !m.busyShown || !m.busy() {
		return ""
	}
	return "refreshing"
}
