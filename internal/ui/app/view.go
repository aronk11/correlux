package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// View renders the frame. It is a pure function of the model: nothing is
// fetched, computed lazily or mutated here.
func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Correlux — " + m.contextName

	if m.quitting {
		v.SetContent("")
		return v
	}
	if !m.ready {
		v.SetContent("Starting Correlux…")
		return v
	}
	if m.screen.TooSmall {
		v.SetContent(m.renderTooSmall())
		return v
	}

	base := strings.Join([]string{
		components.RenderHeader(m.theme, m.headerData(), m.screen.Header.Width),
		m.renderBody(),
		components.RenderStatus(m.theme, m.statusData(), m.screen.Status.Width),
	}, "\n")

	if m.overlay == overlayNone {
		v.SetContent(base)
		return v
	}

	rect := m.overlayRect()
	overlay := m.renderOverlay(rect)
	canvas := lipgloss.NewCanvas(m.screen.Width, m.screen.Height)
	canvas.Compose(lipgloss.NewLayer(base))
	canvas.Compose(lipgloss.NewLayer(overlay).X(rect.X).Y(rect.Y).Z(1))
	v.SetContent(canvas.Render())
	return v
}

func (m *Model) renderTooSmall() string {
	msg := "Correlux needs at least " +
		itoa(layout.MinWidth) + "x" + itoa(layout.MinHeight) + " — this terminal is " +
		itoa(m.screen.Width) + "x" + itoa(m.screen.Height)
	return m.theme.Warning.Render(msg)
}

func (m *Model) headerData() components.HeaderData {
	kctx := m.currentContext()
	d := components.HeaderData{
		Context:    m.contextName,
		Production: kctx.Production,
		Scope:      m.scopeLabel(),
		Version:    m.version(),
		Breadcrumb: m.breadcrumb(),
		Auto:       m.autoRefreshLabel(),
	}

	info := m.cluster.Get()
	switch m.cluster.State() {
	case async.Idle, async.Loading:
		d.ConnStatus = theme.StatusUnknown
		d.ConnGlyph = m.theme.Glyphs.Pending
		d.ConnLabel = "connecting"
	case async.Failed:
		d.ConnStatus = theme.StatusCritical
		d.ConnLabel = "unreachable"
	case async.Ready:
		switch info.State {
		case kubeclient.ConnOK:
			d.ConnStatus = theme.StatusHealthy
			d.ConnLabel = "connected"
			detail := info.ServerVersion
			if info.Latency > 0 {
				detail += " " + formatLatency(info.Latency)
			}
			d.ConnDetail = strings.TrimSpace(detail)
		case kubeclient.ConnForbidden:
			d.ConnStatus = theme.StatusWarning
			d.ConnLabel = info.State.String()
		default:
			d.ConnStatus = theme.StatusCritical
			d.ConnLabel = info.State.String()
		}
	}
	return d
}

// autoRefreshHint says what Ctrl+F will do, not what is currently happening —
// the header already shows that.
func autoRefreshHint(on bool) string {
	if on {
		return "Stop auto"
	}
	return "Auto"
}

// autoRefreshLabel names the timed reload for the header, empty when it is off.
func (m *Model) autoRefreshLabel() string {
	if !m.autoRefresh {
		return ""
	}
	return "auto " + m.refreshEvery.String()
}

// breadcrumb shows where the user is in the navigation model:
// cluster, scope, application, object (SPEC 5).
func (m *Model) breadcrumb() []string {
	switch m.view {
	case viewFleet:
		// The fleet sits above the cluster: these are the two screens that are
		// not about the context in the header.
		return []string{"Fleet", fleetCrumb(m.fleetMembers)}
	case viewFleetResource:
		return []string{"Fleet", m.fleetResource.Kind(), m.fleetResourceLabel()}
	}

	crumbs := []string{"Cluster", m.scopeLabel()}
	switch m.view {
	case viewTable:
		label := m.resource.Kind()
		if table := m.table.Get(); table != nil {
			label += "  " + m.rowCountLabel(table)
		}
		return append(crumbs, label)
	case viewApplication, viewWhy:
		crumbs = append(crumbs, "Applications")
		name := m.selectedApp
		if a, ok := m.currentApplication(); ok {
			name = a.Name
		}
		crumbs = append(crumbs, name)
		if m.view == viewWhy {
			return append(crumbs, "Why")
		}
		return crumbs
	case viewObject:
		// The trail says where the object was reached from, and the breadcrumb
		// has to agree with it: an object opened from the resource browser did
		// not come through an application.
		switch m.objectFrom {
		case viewTable:
			crumbs = append(crumbs, m.resource.Kind())
		case viewActivity:
			crumbs = append(crumbs, "Recent activity")
		default:
			crumbs = append(crumbs, "Applications")
			if a, ok := m.currentApplication(); ok {
				crumbs = append(crumbs, a.Name)
			}
		}
		for _, ref := range m.objectTrail {
			crumbs = append(crumbs, ref.label())
		}
		return append(crumbs, m.objectTarget.label())
	case viewLogs:
		// The body already carries the full title; the breadcrumb only has to
		// say where in the navigation you are.
		return append(crumbs, "Logs")
	case viewUsage:
		return append(crumbs, "Resource usage")
	case viewActivity:
		return append(crumbs, "Recent activity")
	case viewOverview:
		return append(crumbs, "Session")
	default:
		return append(crumbs, "Applications  "+m.applicationsLabel())
	}
}

// applicationsLabel counts the dashboard the way an operator triages it: how
// many are broken first, and never a bare number while the list is still
// loading.
func (m *Model) applicationsLabel() string {
	switch m.apps.State() {
	case async.Idle, async.Loading:
		return "loading…"
	case async.Failed:
		return "unavailable"
	}
	counts := application.Summarise(m.applications())
	if counts.Total == 0 {
		return "none"
	}
	parts := []string{itoa(counts.Total) + " total"}
	if counts.Down > 0 {
		parts = append(parts, itoa(counts.Down)+" down")
	}
	if counts.Degraded > 0 {
		parts = append(parts, itoa(counts.Degraded)+" degraded")
	}
	if counts.Healthy > 0 {
		parts = append(parts, itoa(counts.Healthy)+" healthy")
	}
	if counts.Unknown > 0 {
		parts = append(parts, itoa(counts.Unknown)+" unknown")
	}
	return strings.Join(parts, ", ")
}

// rowCountLabel is honest about paging: "500 of 4213" beats a bare "500" that
// looks like the whole truth.
func (m *Model) rowCountLabel(table *resources.Table) string {
	loaded := len(table.Rows)
	switch {
	case table.Remaining > 0:
		return itoa(loaded) + " of " + itoa(loaded+int(table.Remaining))
	case table.HasMore():
		return itoa(loaded) + "+"
	default:
		return itoa(loaded)
	}
}

func (m *Model) statusData() components.StatusData {
	if m.message != "" {
		return components.StatusData{Message: m.message, MessageStatus: m.messageStatus}
	}
	filter, note := m.filterStatus()

	// Priorities decide what survives a narrow terminal: the palette is the way
	// to reach everything else, and help is the way to learn it, so those two
	// go last.
	hints := []components.KeyHint{
		{Key: m.keys.Key(ActionPalette), Desc: "Commands", Priority: 100},
		{Key: m.keys.Key(ActionResourcePicker), Desc: "Resources", Priority: 70},
		{Key: m.keys.Key(ActionContextPicker), Desc: "Cluster", Priority: 80},
		{Key: m.keys.Key(ActionNamespacePicker), Desc: "Scope", Priority: 78},
		{Key: m.keys.Key(ActionAutoRefresh), Desc: autoRefreshHint(m.autoRefresh), Priority: 76},
		{Key: m.keys.Key(ActionRefresh), Desc: "Refresh", Priority: 60},
		{Key: m.keys.Key(ActionHelp), Desc: "Help", Priority: 95},
		{Key: m.keys.Key(ActionQuit), Desc: "Quit", Priority: 40},
	}
	// The key is advertised only while it is not in use: with a filter on
	// screen, a hint telling you how to filter is a line of noise.
	if m.searchable() && !m.searching && !m.filtering() {
		hints = append(hints, components.KeyHint{
			Key: m.keys.Key(ActionSearch), Desc: "Filter", Priority: 83,
		})
	}

	switch m.view {
	case viewTable:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Rows", Priority: 70},
			{Key: "Enter", Desc: "Open", Priority: 72},
			{Key: m.keys.Key(ActionToggleWide), Desc: wideHint(m.tableWide), Priority: 50},
			{Key: "Esc", Desc: "Applications", Priority: 85},
		}, hints...)
	case viewApplications:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Move", Priority: 70},
			{Key: "Enter", Desc: "Open", Priority: 72},
			{Key: m.keys.Key(ActionWhy), Desc: "Why", Priority: 88},
			{Key: m.keys.Key(ActionGrouping), Desc: "Grouping", Priority: 45},
			{Key: m.keys.Key(ActionUsage), Desc: "Usage", Priority: 79},
			{Key: m.keys.Key(ActionToggleWide), Desc: wideHint(m.tableWide), Priority: 50},
		}, hints...)
	case viewUsage:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Scroll", Priority: 70},
			{Key: m.keys.Key(ActionRefresh), Desc: "Measure again", Priority: 84},
			{Key: "Esc", Desc: "Applications", Priority: 85},
		}, hints...)
	case viewActivity:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Events", Priority: 70},
			{Key: "Enter", Desc: "Open object", Priority: 72},
			{Key: m.keys.Key(ActionRefresh), Desc: "Reload", Priority: 84},
			{Key: "Esc", Desc: "Applications", Priority: 85},
		}, hints...)
	case viewApplication:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Objects", Priority: 70},
			{Key: "Enter", Desc: "Open", Priority: 72},
			{Key: m.keys.Key(ActionWhy), Desc: "Why", Priority: 88},
			{Key: m.keys.Key(ActionGrouping), Desc: groupingHint(m.groupingShown), Priority: 45},
			{Key: m.keys.Key(ActionLogs), Desc: "Logs", Priority: 87},
			{Key: m.keys.Key(ActionUsage), Desc: "Usage", Priority: 79},
			{Key: "Esc", Desc: "Applications", Priority: 85},
		}, hints...)
	case viewWhy:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Scroll", Priority: 70},
			{Key: "Enter", Desc: "Objects", Priority: 72},
			{Key: m.keys.Key(ActionLogs), Desc: "Logs", Priority: 87},
			{Key: "Esc", Desc: "Applications", Priority: 85},
		}, hints...)
	case viewFleet:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Clusters", Priority: 70},
			{Key: "Enter", Desc: "Go there", Priority: 72},
			{Key: m.keys.Key(ActionResourcePicker), Desc: "Across the fleet", Priority: 86},
			{Key: m.keys.Key(ActionRefresh), Desc: "Reload", Priority: 84},
			{Key: "Esc", Desc: "Back", Priority: 85},
		}, hints...)
	case viewFleetResource:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Rows", Priority: 70},
			{Key: "Enter", Desc: "Open there", Priority: 72},
			{Key: m.keys.Key(ActionToggleWide), Desc: wideHint(m.tableWide), Priority: 82},
			{Key: "Esc", Desc: "Fleet", Priority: 85},
		}, hints...)
	case viewLogs:
		hints = append([]components.KeyHint{
			{Key: "↑↓", Desc: "Scroll", Priority: 70},
			{Key: m.keys.Key(ActionFollow), Desc: followHint(m.logFollow && !m.logClosed), Priority: 89},
			{Key: m.keys.Key(ActionTimestamps), Desc: "Times", Priority: 82},
			{Key: m.keys.Key(ActionPrevious), Desc: previousHint(m.logPrevious), Priority: 81},
			{Key: m.keys.Key(ActionToggleWide), Desc: wrapHint(m.logWrap), Priority: 80},
			{Key: "Esc", Desc: "Back", Priority: 85},
		}, hints...)
	case viewObject:
		object := []components.KeyHint{
			{Key: "↑↓", Desc: "Related", Priority: 70},
			{Key: "Enter", Desc: "Follow", Priority: 72},
			{Key: m.keys.Key(ActionYAML), Desc: yamlHint(m.objectYAML), Priority: 87},
			{Key: m.keys.Key(ActionDecode), Desc: decodeHint(m.objectDecode), Priority: 83},
			{Key: "Esc", Desc: "Back", Priority: 85},
		}
		object = append(object, components.KeyHint{
			Key: m.keys.Key(ActionEdit), Desc: "Edit", Priority: 86,
		}, components.KeyHint{
			Key: m.keys.Key(ActionLogs), Desc: "Logs", Priority: 87,
		})
		if _, ok := m.scalableTarget(); ok {
			object = append(object, components.KeyHint{
				Key: m.keys.Key(ActionScale), Desc: "Scale", Priority: 84,
			})
		}
		hints = append(object, hints...)
	}
	switch m.overlay {
	case overlayNone:
	case overlayConfirm:
		hints = []components.KeyHint{
			{Key: "Enter", Desc: "Apply", Priority: 90},
			{Key: "Esc", Desc: "Cancel", Priority: 90},
		}
	case overlayPrompt:
		hints = []components.KeyHint{
			{Key: "Enter", Desc: "Continue", Priority: 90},
			{Key: "Esc", Desc: "Cancel", Priority: 90},
		}
	default:
		hints = []components.KeyHint{
			{Key: "↑↓", Desc: "Navigate", Priority: 90},
			{Key: "Enter", Desc: "Select", Priority: 90},
			{Key: "Esc", Desc: "Cancel", Priority: 90},
		}
	}
	return components.StatusData{
		Hints:         hints,
		Filter:        filter,
		FilterNote:    note,
		FilterFocused: m.searching,
	}
}

// filterStatus renders the filter and how much of the list it is showing.
func (m *Model) filterStatus() (filter, note string) {
	if !m.searching && !m.filtering() {
		return "", ""
	}
	filter = m.search.Value()
	if !m.filtering() {
		return filter, ""
	}

	switch m.view {
	case viewTable:
		table := m.table.Get()
		complete := table != nil && !table.HasMore()
		return filter, searchNote(len(m.visibleRows()), len(m.tableRows()), complete)
	case viewApplications:
		return filter, searchNote(len(m.visibleApplications()), len(m.applications()), true)
	case viewFleetResource:
		return filter, searchNote(len(m.visibleFleetRows()), len(m.fleetTable.Rows),
			m.fleetPending == 0 && !m.fleetTable.Truncated)
	default:
		return filter, ""
	}
}

func (m *Model) renderBody() string {
	body := m.screen.Body
	var content string
	switch m.view {
	case viewTable:
		content = screens.RenderTable(m.theme, m.tableData(), body.Width, body.Height)
	case viewApplications:
		content = screens.RenderTable(m.theme, m.applicationsData(), body.Width, body.Height)
	case viewApplication:
		content = screens.RenderApplication(m.theme, m.applicationData(), body.Width, body.Height)
	case viewWhy:
		content = screens.RenderWhy(m.theme, m.whyData(), body.Width, body.Height)
	case viewObject:
		content = screens.RenderObject(m.theme, m.objectData(), body.Width, body.Height)
	case viewLogs:
		content = screens.RenderLogs(m.theme, m.logsData(), body.Width, body.Height)
	case viewUsage:
		content = screens.RenderUsage(m.theme, m.usageData(), body.Width, body.Height)
	case viewActivity:
		content = screens.RenderApplication(m.theme, m.activityData(), body.Width, body.Height)
	case viewFleet:
		content = screens.RenderFleet(m.theme, m.fleetData(), body.Width, body.Height)
	case viewFleetResource:
		content = screens.RenderTable(m.theme, m.fleetResourceData(), body.Width, body.Height)
	default:
		content = screens.RenderOverview(m.theme, m.overviewData(), body.Width, body.Height)
	}
	return padBlock(content, body.Width, body.Height)
}

// tableData turns the loaded page into rendering data. The four remote states
// become four different sentences: a spinner-free "Loading…", an explicit
// "none", a permission error and a transport error do not look alike.
func (m *Model) tableData() screens.TableData {
	d := screens.TableData{
		Cursor:   m.tablePort.Cursor,
		Offset:   m.tablePort.Offset,
		ShowWide: m.tableWide,
	}

	switch m.table.State() {
	case async.Idle, async.Loading:
		d.Message = "Loading " + m.resource.Plural() + "…"
		return d
	case async.Failed:
		d.Message = "Could not list " + m.resource.Plural() + ": " + shortError(m.table.Err())
		d.MessageStatus = theme.StatusCritical
		return d
	}

	table := m.table.Get()
	if table == nil {
		d.Message = "No data."
		return d
	}
	if rows := m.visibleRows(); len(rows) == 0 && m.filtering() {
		d.Message = "Nothing matches " + m.query() + " among " +
			itoa(len(table.Rows)) + " loaded " + m.resource.Plural() + "."
		return d
	}
	if len(table.Rows) == 0 {
		d.Message = "No " + m.resource.Plural() + " in " + m.scopeLabel() + "."
		d.MessageStatus = theme.StatusUnknown
		return d
	}

	d.Columns = make([]screens.TableColumn, 0, len(table.Columns))
	for _, c := range table.Columns {
		d.Columns = append(d.Columns, screens.TableColumn{
			Title: c.Name,
			Wide:  c.Wide(),
			Right: c.Type == "integer" || c.Type == "number",
		})
	}
	rows := m.visibleRows()
	d.Rows = make([]screens.TableRow, 0, len(rows))
	for i := range rows {
		d.Rows = append(d.Rows, screens.TableRow{
			Cells:  rows[i].Cells,
			Status: rowStatus(rows[i].Cells),
		})
	}
	return d
}

// unhealthyCells are the status words that make a row worth noticing. The
// colour is a hint on top of the text the server already printed, never the
// only signal.
var unhealthyCells = map[string]theme.Status{
	"CrashLoopBackOff":     theme.StatusCritical,
	"ImagePullBackOff":     theme.StatusCritical,
	"ErrImagePull":         theme.StatusCritical,
	"CreateContainerError": theme.StatusCritical,
	"Failed":               theme.StatusCritical,
	"Evicted":              theme.StatusCritical,
	"OOMKilled":            theme.StatusCritical,
	"NotReady":             theme.StatusCritical,
	"Pending":              theme.StatusWarning,
	"ContainerCreating":    theme.StatusWarning,
	"PodInitializing":      theme.StatusWarning,
	"Terminating":          theme.StatusWarning,
	"Unknown":              theme.StatusWarning,
}

func rowStatus(cells []string) theme.Status {
	for _, cell := range cells {
		if status, ok := unhealthyCells[cell]; ok {
			return status
		}
	}
	return theme.StatusUnknown
}

// overviewData assembles the session view: what Correlux knows for certain
// about this connection. It is no longer the first screen — applications are —
// but it remains the place that answers "what am I actually connected to?".
func (m *Model) overviewData() screens.OverviewData {
	kctx := m.currentContext()
	info := m.cluster.Get()

	connection := screens.Panel{Title: "Connection"}
	switch m.cluster.State() {
	case async.Idle, async.Loading:
		connection.Fields = append(connection.Fields, screens.Field{
			Label: "Status", Value: "Connecting…", Status: theme.StatusUnknown, Glyph: true,
		})
	case async.Ready, async.Failed:
		status := theme.StatusCritical
		switch info.State {
		case kubeclient.ConnOK:
			status = theme.StatusHealthy
		case kubeclient.ConnForbidden:
			status = theme.StatusWarning
		}
		connection.Fields = append(connection.Fields, screens.Field{
			Label: "Status", Value: info.State.String(), Status: status, Glyph: true,
		})
		if info.ServerVersion != "" {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Version", Value: info.ServerVersion,
			})
		}
		if info.Latency > 0 {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Latency", Value: formatLatency(info.Latency),
			})
		}
		if info.Err != nil {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Error", Value: kubeclient.FriendlyError(info.Err), Status: theme.StatusCritical,
			})
		}
		if info.Hint != "" {
			connection.Note = info.Hint
		}
	}
	connection.Fields = append(connection.Fields, screens.Field{Label: "Server", Value: orNone(kctx.Server)})

	session := screens.Panel{Title: "Session"}
	contextValue := m.contextName
	contextStatus := theme.StatusUnknown
	if kctx.Production {
		contextValue += "  (production)"
		contextStatus = theme.StatusCritical
	}
	session.Fields = []screens.Field{
		{Label: "Context", Value: contextValue, Status: contextStatus},
		{Label: "Scope", Value: m.scopeLabel()},
		{Label: "User", Value: orNone(kctx.User)},
		{Label: "Namespaces", Value: m.namespacesSummary()},
		{Label: "API kinds", Value: m.catalogSummary(), Status: m.catalogStatus()},
	}
	session.Note = "Switching context here does not change your kubectl context."

	environment := screens.Panel{Title: "Environment"}
	environment.Fields = []screens.Field{
		{Label: "Kubeconfig", Value: orNone(strings.Join(m.kubeconfig.Sources, ", "))},
		{Label: "Config", Value: configLabel(m.cfg.SourcePath, m.configPath)},
		{Label: "Theme", Value: themeLabel(m.cfg.Theme, m.caps)},
		{Label: "Terminal", Value: terminalLabel(m.caps, m.screen)},
	}

	session.Fields = append(session.Fields, screens.Field{
		Label: "Applications", Value: m.applicationsLabel(),
	})

	return screens.OverviewData{
		Panels: []screens.Panel{connection, session, environment},
		Roadmap: []string{
			"WHY diagnosis engine (Phase 3)",
			"Logs, exec and object detail (Phase 4)",
		},
	}
}

// catalogSummary reports what the cluster serves, custom resources called out
// separately: on most real clusters they are half of what an operator works
// with, and they are what other tools hide behind a submenu.
func (m *Model) catalogSummary() string {
	switch m.catalog.State() {
	case async.Idle:
		return "not discovered"
	case async.Loading:
		return "discovering…"
	case async.Failed:
		return "unavailable — " + kubeclient.FriendlyError(m.catalog.Err())
	}
	catalog := m.catalog.Get()
	if catalog == nil || catalog.Len() == 0 {
		return "none"
	}
	summary := itoa(catalog.Len()) + " listable, " + itoa(len(catalog.CustomResources())) + " custom"
	if catalog.Partial() {
		summary += "  (" + itoa(len(catalog.Failures)) + " group(s) unavailable)"
	}
	return summary
}

func (m *Model) catalogStatus() theme.Status {
	if m.catalog.State() == async.Failed {
		return theme.StatusCritical
	}
	if catalog := m.catalog.Get(); catalog != nil && catalog.Partial() {
		return theme.StatusWarning
	}
	return theme.StatusUnknown
}

// namespacesSummary states the namespace count without ever implying that a
// pending request means an empty cluster.
func (m *Model) namespacesSummary() string {
	switch m.namespaces.State() {
	case async.Idle:
		return "not loaded"
	case async.Loading:
		return "loading…"
	case async.Failed:
		return "unavailable — " + kubeclient.FriendlyError(m.namespaces.Err())
	}
	list := m.namespaces.Get()
	switch {
	case list.Restricted:
		return "listing not permitted for this user"
	case len(list.Names) == 0:
		return "none visible"
	case list.Truncated:
		return itoa(len(list.Names)) + "+ visible"
	default:
		return itoa(len(list.Names)) + " visible"
	}
}

func (m *Model) renderOverlay(rect layout.Rect) string {
	inner := rect.Width - 4   // border + horizontal padding
	innerH := rect.Height - 2 // border
	if inner < 8 {
		inner = 8
	}
	if innerH < 3 {
		innerH = 3
	}

	var content string
	switch m.overlay {
	case overlayHelp:
		content = m.renderHelp(inner, innerH)
	case overlayConfirm:
		content = m.renderConfirm(inner, innerH)
	case overlayPrompt:
		content = m.renderPrompt(inner, innerH)
	default:
		if sel := m.activeSelector(); sel != nil {
			content = sel.Render(m.theme, inner, innerH)
		}
	}
	return m.theme.Overlay.Width(rect.Width).Render(content)
}

func (m *Model) renderHelp(width, height int) string {
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"Navigate", [][2]string{
			{m.keys.Key(ActionPalette), "Command palette — every action, by name"},
			{m.keys.Key(ActionApplications), "Back to the application dashboard"},
			{m.keys.Key(ActionFleet), "The fleet: every configured cluster at once, read-only"},
			{m.keys.Key(ActionResourcePicker), "In the fleet: browse one kind across every cluster"},
			{m.keys.Key(ActionContextPicker), "Switch cluster"},
			{m.keys.Key(ActionNamespacePicker), "Switch namespace"},
		}},
		{"In the application dashboard", [][2]string{
			{"↑ ↓ / j k", "Move between applications"},
			{"Enter", "Open the application: its workloads, pods and network"},
			{m.keys.Key(ActionWhy), "Explain why it is unhealthy, from the cluster's own evidence"},
			{m.keys.Key(ActionGrouping), "Show which signal grouped each object, and how sure it is"},
			{"Esc", "Back to the dashboard"},
		}},
		{"Logs", [][2]string{
			{m.keys.Key(ActionLogs), "Read the logs of the pod, workload or application in hand"},
			{m.keys.Key(ActionFollow), "Follow new output, or pause to read what is there"},
			{m.keys.Key(ActionPrevious), "Read the previous run of a container that restarted"},
			{m.keys.Key(ActionTimestamps), "Show the time each line was written"},
			{m.keys.Key(ActionToggleWide), "Wrap long lines instead of cutting them"},
		}},
		{"In an application or an object", [][2]string{
			{"↑ ↓ / j k", "Move between the objects; the page follows"},
			{"Enter", "Open the object under the cursor, or follow the relation"},
			{m.keys.Key(ActionYAML), "Show the document the server holds, and back"},
			{m.keys.Key(ActionDecode), "Decode the base64 values in it — a Secret's, above all"},
			{m.keys.Key(ActionScale), "Scale the selected workload, after confirming the blast radius"},
			{m.keys.Key(ActionEdit), "Edit the open object in $EDITOR, then review what changed"},
			{"Esc", "Back the way you came in"},
		}},
		{"Cluster", [][2]string{
			{m.keys.Key(ActionResourcePicker), "Browse resource kinds, including custom resources"},
			{m.keys.Key(ActionUsage), "Where the pods are, and what CPU and memory they use"},
			{m.keys.Key(ActionActivity), "Recent Kubernetes Events in the active scope"},
			{m.keys.Key(ActionRefresh), "Refresh"},
			{m.keys.Key(ActionAutoRefresh), "Refresh on a timer, until you turn it off"},
		}},
		{"Filtering", [][2]string{
			{m.keys.Key(ActionSearch), "Narrow the list on screen; type to filter, Esc to clear"},
			{"↑ ↓ / Enter", "Leave the filter and act on what is left"},
		}},
		{"In a resource table", [][2]string{
			{"↑ ↓ / j k", "Move; the next page loads as you reach the end"},
			{"Enter", "Open the object under the cursor, custom resources included"},
			{m.keys.Key(ActionToggleWide), "Toggle the wide columns"},
			{"Esc", "Back to the overview"},
		}},
		{"General", [][2]string{
			{m.keys.Key(ActionHelp), "This help"},
			{"Esc", "Close overlay"},
			{m.keys.Key(ActionQuit), "Quit"},
		}},
		{"In lists", [][2]string{
			{"↑ ↓", "Move"},
			{"PgUp PgDn", "Page"},
			{"Enter", "Select"},
			{"Type", "Fuzzy filter"},
		}},
	}

	var b strings.Builder
	b.WriteString(m.theme.OverlayTitle.Render("Keyboard shortcuts"))
	for _, s := range sections {
		b.WriteString("\n\n" + m.theme.Muted.Render(s.title))
		for _, row := range s.rows {
			if row[0] == "" {
				continue
			}
			b.WriteString("\n  " + m.theme.Key.Render(padTo(row[0], 12)) + m.theme.KeyDesc.Render(row[1]))
		}
	}
	b.WriteString("\n\n" + m.theme.Muted.Render("Keys are configurable in "+orNone(m.configPath)))
	return clipTo(b.String(), width, height)
}

func padBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		if lipgloss.Width(l) < width {
			lines[i] = l + strings.Repeat(" ", width-lipgloss.Width(l))
		}
	}
	return strings.Join(lines, "\n")
}

func clipTo(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func padTo(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// fleetCrumb summarises the fleet for the breadcrumb: how many clusters, and
// how many of them have answered.
func fleetCrumb(members []fleet.Member) string {
	if len(members) == 0 {
		return "not configured"
	}
	summary := fleet.Summarise(members)
	if summary.Complete() {
		return itoa(summary.Clusters) + " " + clusterWord(summary.Clusters)
	}
	return itoa(summary.Answered) + " of " + itoa(summary.Clusters) + " " +
		clusterWord(summary.Clusters) + " answered"
}

// followHint, previousHint and wrapHint all name what the key will do next,
// not what is on screen.
func followHint(following bool) string {
	if following {
		return "Pause"
	}
	return "Follow"
}

func previousHint(previous bool) string {
	if previous {
		return "Current run"
	}
	return "Previous run"
}

func wrapHint(wrapped bool) string {
	if wrapped {
		return "Clip"
	}
	return "Wrap"
}

// yamlHint names what the key will show next, not what is on screen.
func yamlHint(showing bool) string {
	if showing {
		return "Details"
	}
	return "YAML"
}

// decodeHint names what the key will show next, not what is on screen.
func decodeHint(decoding bool) string {
	if decoding {
		return "Encoded"
	}
	return "Decode"
}

func wideHint(wide bool) string {
	if wide {
		return "Compact"
	}
	return "Wide"
}

// groupingHint names what the key will show next, not what is on screen.
func groupingHint(shown bool) string {
	if shown {
		return "Hide grouping"
	}
	return "Grouping"
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return itoa(int(d.Round(time.Millisecond)/time.Millisecond)) + "ms"
}

// configLabel names the config file, saying plainly when none exists rather
// than showing an empty value.
func configLabel(source, path string) string {
	if source != "" {
		return source
	}
	if path == "" {
		return "—"
	}
	return path + " (not created)"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// themeLabel reports the effective theme, naming the preference only when it
// differs from what was detected.
func themeLabel(pref config.Theme, caps theme.Capabilities) string {
	if !caps.Color {
		return "no colour"
	}
	mode := "light"
	if caps.Dark {
		mode = "dark"
	}
	if pref == config.ThemeAuto || pref == "" {
		return mode + " (auto)"
	}
	return mode
}

func terminalLabel(caps theme.Capabilities, screen layout.Screen) string {
	label := itoa(screen.Width) + "x" + itoa(screen.Height)
	if caps.Unicode {
		return label + ", unicode"
	}
	return label + ", ascii"
}
