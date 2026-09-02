package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/logs"
	"github.com/aronk11/kubeui/internal/ui/async"
	"github.com/aronk11/kubeui/internal/ui/palette"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// Palette action identifiers. Direct actions carry their target in Command.Arg,
// so "switch to prod-eu" is one keystroke away without opening a submenu.
const (
	paletteOpenContexts    palette.ActionID = "open.contexts"
	paletteOpenNamespaces  palette.ActionID = "open.namespaces"
	paletteSwitchContext   palette.ActionID = "switch.context"
	paletteSwitchNamespace palette.ActionID = "switch.namespace"
	paletteToggleAllNS     palette.ActionID = "toggle.allnamespaces"
	paletteOpenApps        palette.ActionID = "open.applications"
	paletteOpenFleet       palette.ActionID = "open.fleet"
	paletteFleetEverything palette.ActionID = "fleet.everything"
	paletteOpenApp         palette.ActionID = "open.application"
	paletteExplain         palette.ActionID = "explain"
	paletteToggleYAML      palette.ActionID = "object.yaml"
	paletteScale           palette.ActionID = "scale"
	paletteEdit            palette.ActionID = "edit"
	paletteLogs            palette.ActionID = "logs"
	paletteOpenResources   palette.ActionID = "open.resources"
	paletteOpenResource    palette.ActionID = "open.resource"
	paletteToggleWide      palette.ActionID = "toggle.wide"
	paletteBackToOverview  palette.ActionID = "open.overview"
	paletteRefresh         palette.ActionID = "refresh"
	paletteAutoRefresh     palette.ActionID = "refresh.auto"
	paletteReloadConfig    palette.ActionID = "reload.kubeconfig"
	paletteHelp            palette.ActionID = "help"
	paletteQuit            palette.ActionID = "quit"
)

// allNamespacesID is the synthetic namespace-picker row for cluster-wide scope.
const allNamespacesID = "__all_namespaces"

// rebuildCommands regenerates the palette. It runs whenever the world changes
// (context switched, namespaces loaded), which keeps every entry's subtitle and
// enabled-state honest rather than stale.
func (m *Model) rebuildCommands() {
	cmds := []palette.Command{
		{
			ID:       "cmd.contexts",
			Action:   paletteOpenContexts,
			Title:    "Switch cluster",
			Subtitle: m.contextName,
			Category: "Navigate",
			Keywords: []string{"context", "ctx", "cluster", "kubeconfig"},
			Shortcut: m.keys.Key(ActionContextPicker),
			Weight:   100,
			Enabled:  true,
		},
		{
			ID:       "cmd.namespaces",
			Action:   paletteOpenNamespaces,
			Title:    "Switch namespace",
			Subtitle: m.scopeLabel(),
			Category: "Navigate",
			Keywords: []string{"namespace", "ns", "scope", "project"},
			Shortcut: m.keys.Key(ActionNamespacePicker),
			Weight:   95,
			Enabled:  true,
		},
		{
			ID:       "cmd.allns",
			Action:   paletteToggleAllNS,
			Title:    allNamespacesTitle(m.allNamespaces),
			Category: "Navigate",
			Keywords: []string{"all", "namespaces", "cluster wide", "-A"},
			Weight:   70,
			Enabled:  true,
		},
		{
			ID:       "cmd.applications",
			Action:   paletteOpenApps,
			Title:    "Show applications",
			Subtitle: m.applicationsLabel(),
			Category: "Navigate",
			Keywords: []string{"application", "apps", "dashboard", "home", "overview", "health"},
			Shortcut: m.keys.Key(ActionApplications),
			Weight:   98,
			Enabled:  true,
		},
		{
			ID:       "cmd.fleet",
			Action:   paletteOpenFleet,
			Title:    "Show the fleet",
			Subtitle: m.fleetSubtitleForPalette(),
			Category: "Navigate",
			Keywords: []string{"fleet", "clusters", "multi", "all clusters", "overview", "tenants"},
			Shortcut: m.keys.Key(ActionFleet),
			Weight:   92,
			Enabled:  true,
		},
		{
			ID:       "cmd.resources",
			Action:   paletteOpenResources,
			Title:    "Browse resources",
			Subtitle: m.catalogSubtitle(),
			Category: "Navigate",
			Keywords: []string{"resource", "kind", "crd", "custom", "api", "objects"},
			Shortcut: m.keys.Key(ActionResourcePicker),
			Weight:   90,
			Enabled:  true,
		},
		{
			ID:       "cmd.why",
			Action:   paletteExplain,
			Title:    "Explain why this is unhealthy",
			Subtitle: m.whySubtitle(),
			Category: "Diagnose",
			Keywords: []string{"why", "diagnose", "explain", "root cause", "incident", "broken"},
			Shortcut: m.keys.Key(ActionWhy),
			Weight:   97,
			Enabled:  true,
		},
		{
			ID:       "cmd.session",
			Action:   paletteBackToOverview,
			Title:    "Show session and connection details",
			Subtitle: m.contextName,
			Category: "Cluster",
			Keywords: []string{"session", "connection", "server", "version", "kubeconfig", "diagnostics"},
			Weight:   65,
			Enabled:  true,
		},
		{
			ID:       "cmd.refresh",
			Action:   paletteRefresh,
			Title:    "Refresh cluster status",
			Category: "Cluster",
			Keywords: []string{"reload", "retry", "reconnect", "probe"},
			Shortcut: m.keys.Key(ActionRefresh),
			Weight:   60,
			Enabled:  true,
		},
		{
			ID:       "cmd.autorefresh",
			Action:   paletteAutoRefresh,
			Title:    autoRefreshTitle(m.autoRefresh),
			Subtitle: autoRefreshSubtitle(m.autoRefresh, m.refreshEvery),
			Category: "Cluster",
			Keywords: []string{"auto", "refresh", "follow", "watch", "poll", "live"},
			Shortcut: m.keys.Key(ActionAutoRefresh),
			Weight:   59,
			Enabled:  true,
		},
		{
			ID:       "cmd.reloadkubeconfig",
			Action:   paletteReloadConfig,
			Title:    "Reload kubeconfig from disk",
			Subtitle: firstOr(m.kubeconfig.Sources, ""),
			Category: "Cluster",
			Keywords: []string{"kubeconfig", "contexts", "rescan"},
			Weight:   40,
			Enabled:  true,
		},
		{
			ID:       "cmd.help",
			Action:   paletteHelp,
			Title:    "Show keyboard shortcuts",
			Category: "Help",
			Keywords: []string{"help", "keys", "shortcuts", "?"},
			Shortcut: m.keys.Key(ActionHelp),
			Weight:   30,
			Enabled:  true,
		},
		{
			ID:       "cmd.quit",
			Action:   paletteQuit,
			Title:    "Quit kubeui",
			Category: "Help",
			Keywords: []string{"exit", "close"},
			Shortcut: m.keys.Key(ActionQuit),
			Weight:   10,
			Enabled:  true,
		},
	}

	if m.view == viewTable || m.view == viewApplications {
		cmds = append(cmds,
			palette.Command{
				ID:       "cmd.wide",
				Action:   paletteToggleWide,
				Title:    wideTitle(m.tableWide),
				Subtitle: m.wideSubject(),
				Category: "View",
				Keywords: []string{"wide", "columns", "-o wide", "details"},
				Shortcut: m.keys.Key(ActionToggleWide),
				Weight:   80,
				Enabled:  true,
			},
		)
	}

	if _, title, ok := m.logSources(); ok && m.view != viewLogs {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.logs",
			Action:   paletteLogs,
			Title:    title,
			Subtitle: "the last " + itoa(int(logs.DefaultTail)) + " lines, then whatever follows",
			Category: "Inspect",
			Keywords: []string{"logs", "log", "tail", "output", "stdout", "follow"},
			Shortcut: m.keys.Key(ActionLogs),
			Weight:   88,
			Enabled:  true,
		})
	}

	if ref, ok := m.scalableTarget(); ok {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.scale",
			Action:   paletteScale,
			Title:    "Scale " + ref.label(),
			Subtitle: m.scaleSubtitle(ref),
			Category: "Change",
			Keywords: []string{"scale", "replicas", "up", "down", "zero", "restart"},
			Shortcut: m.keys.Key(ActionScale),
			Weight:   84,
			Enabled:  true,
		})
	}

	if m.view == viewFleet && len(m.fleetContexts()) < len(m.kubeconfig.Contexts) {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.fleet.everything",
			Action:   paletteFleetEverything,
			Title:    "Add every context in this kubeconfig to the fleet",
			Subtitle: "for this session; kubeui will authenticate against all of them",
			Category: "Navigate",
			Keywords: []string{"fleet", "every", "all contexts", "add"},
			Weight:   50,
			Enabled:  true,
		})
	}

	if m.view == viewObject && !m.objectTarget.empty() {
		cmds = append(cmds, palette.Command{
			ID:             "cmd.edit",
			Action:         paletteEdit,
			Title:          "Edit " + m.objectTarget.label(),
			Subtitle:       "opens " + editorName() + ", then shows what changed",
			Category:       "Change",
			Keywords:       []string{"edit", "change", "yaml", "manifest", "apply", "patch"},
			Shortcut:       m.keys.Key(ActionEdit),
			Weight:         83,
			Enabled:        m.object.State() == async.Ready,
			DisabledReason: "the object is still loading",
		})
	}

	if m.view == viewObject {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.yaml",
			Action:   paletteToggleYAML,
			Title:    yamlTitle(m.objectYAML),
			Subtitle: m.objectTarget.label(),
			Category: "View",
			Keywords: []string{"yaml", "manifest", "document", "source", "describe"},
			Shortcut: m.keys.Key(ActionYAML),
			Weight:   85,
			Enabled:  true,
		})
	}

	if m.view == viewTable {
		cmds = append(cmds,
			palette.Command{
				ID:       "cmd.back",
				Action:   paletteOpenApps,
				Title:    "Back to the applications",
				Category: "Navigate",
				Keywords: []string{"applications", "home", "back"},
				Shortcut: "Esc",
				Weight:   75,
				Enabled:  true,
			},
		)
	}

	// Every resource kind the cluster serves is its own command, so "widgets"
	// is as reachable as "pods" without a submenu. On a fleet screen the same
	// entries browse that kind across every cluster instead.
	inFleet := m.view == viewFleet || m.view == viewFleetResource
	if catalog := m.catalog.Get(); catalog != nil {
		for _, r := range catalog.Resources {
			if m.view == viewTable && r.FullName() == m.resource.FullName() {
				continue
			}
			if m.view == viewFleetResource && r.FullName() == m.fleetResource.FullName() {
				continue
			}
			keywords := append([]string{"resource", r.Plural(), r.Group()}, r.ShortNames...)
			subtitle := r.GroupVersion()
			if !r.Builtin {
				subtitle = "custom resource  " + subtitle
			}
			title := "Open " + r.Kind()
			if inFleet {
				title = "Open " + r.Kind() + " across the fleet"
				subtitle = itoa(len(m.fleetContexts())) + " clusters  " + subtitle
			}
			cmds = append(cmds, palette.Command{
				ID:       "res." + r.FullName(),
				Action:   paletteOpenResource,
				Arg:      r.FullName(),
				Title:    title,
				Subtitle: subtitle,
				Category: "Resources",
				Keywords: keywords,
				Weight:   55,
				Enabled:  true,
			})
		}
	}

	// Every application is its own command, so "open payments" is one keystroke
	// and a search away, without stepping through the dashboard.
	fleetApps := m.applications()
	for i := range fleetApps {
		a := &fleetApps[i]
		if m.view == viewApplication && a.Key() == m.selectedApp {
			continue
		}
		if a.Health != application.Healthy {
			cmds = append(cmds, palette.Command{
				ID:       "why." + a.Key(),
				Action:   paletteExplain,
				Arg:      a.Key(),
				Title:    "Why is " + a.Name + " " + a.Health.String() + "?",
				Subtitle: m.incidentLabel(a),
				Category: "Diagnose",
				Keywords: []string{"why", "diagnose", a.Name, a.Health.String()},
				Weight:   59,
				Enabled:  true,
			})
		}
		cmds = append(cmds, palette.Command{
			ID:       "app." + a.Key(),
			Action:   paletteOpenApp,
			Arg:      a.Key(),
			Title:    "Open " + a.Name,
			Subtitle: a.Health.String() + "  " + a.Summary,
			Category: "Applications",
			Keywords: []string{"application", a.Name, a.Namespace, a.Health.String()},
			Weight:   58,
			Enabled:  true,
		})
	}

	// Direct jumps: every context is its own command.
	for _, c := range m.kubeconfig.Contexts {
		if c.Name == m.contextName {
			continue
		}
		subtitle := c.Server
		if c.Production {
			subtitle = "production  " + subtitle
		}
		cmds = append(cmds, palette.Command{
			ID:       "ctx." + c.Name,
			Action:   paletteSwitchContext,
			Arg:      c.Name,
			Title:    "Switch to cluster " + c.Name,
			Subtitle: subtitle,
			Category: "Cluster",
			Keywords: []string{"context", "cluster", c.Name, c.Cluster},
			Weight:   50,
			Enabled:  true,
		})
	}

	// Direct jumps for namespaces, once they are known.
	if m.namespaces.State() == async.Ready {
		for _, ns := range m.namespaces.Get().Names {
			if ns == m.namespace && !m.allNamespaces {
				continue
			}
			cmds = append(cmds, palette.Command{
				ID:       "ns." + ns,
				Action:   paletteSwitchNamespace,
				Arg:      ns,
				Title:    "Switch to namespace " + ns,
				Category: "Namespace",
				Keywords: []string{"namespace", "ns", ns},
				Weight:   45,
				Enabled:  true,
			})
		}
	}

	m.registry.Set(cmds)
	m.cmdPal.Refresh()
}

// whySubtitle names what the explanation would be about.
func (m *Model) whySubtitle() string {
	if app, ok := m.currentApplication(); ok && m.view != viewApplications {
		return app.Name + " — " + app.Health.String()
	}
	apps := m.applications()
	if m.appPort.Cursor >= 0 && m.appPort.Cursor < len(apps) {
		return apps[m.appPort.Cursor].Name + " — " + apps[m.appPort.Cursor].Health.String()
	}
	return "select an application first"
}

// fleetSubtitleForPalette says what the fleet covers, or that it covers nothing
// yet — which is the default and needs saying.
func (m *Model) fleetSubtitleForPalette() string {
	contexts := m.fleetContexts()
	if len(contexts) == 0 {
		return "no clusters configured yet"
	}
	if len(contexts) <= 3 {
		return strings.Join(contexts, ", ")
	}
	return itoa(len(contexts)) + " clusters, read-only"
}

// catalogSubtitle summarises what discovery found, without ever implying an
// empty cluster while the request is still in flight.
func (m *Model) catalogSubtitle() string {
	switch m.catalog.State() {
	case async.Idle:
		return "not discovered yet"
	case async.Loading:
		return "discovering…"
	case async.Failed:
		return "discovery failed"
	}
	catalog := m.catalog.Get()
	if catalog == nil {
		return ""
	}
	subtitle := itoa(catalog.Len()) + " kinds"
	if custom := len(catalog.CustomResources()); custom > 0 {
		subtitle += ", " + itoa(custom) + " custom"
	}
	if catalog.Partial() {
		subtitle += ", " + itoa(len(catalog.Failures)) + " group(s) unavailable"
	}
	return subtitle
}

// wideSubject names what the wide toggle applies to on the current screen.
func (m *Model) wideSubject() string {
	if m.view == viewTable {
		return m.resource.Kind()
	}
	return "Applications"
}

// scalableTarget reports the workload the current screen points at, when it is
// one that can be scaled.
func (m *Model) scalableTarget() (objectRef, bool) {
	var ref objectRef
	switch m.view {
	case viewObject:
		ref = m.objectTarget
	case viewApplication:
		_, targets := m.applicationView()
		if m.detailPort.Cursor < 0 || m.detailPort.Cursor >= len(targets) {
			return objectRef{}, false
		}
		ref = targets[m.detailPort.Cursor]
	default:
		return objectRef{}, false
	}
	if ref.empty() {
		return objectRef{}, false
	}
	res, ok := m.resourceFor(ref)
	return ref, ok && res.Scalable
}

// scaleSubtitle says what the workload has now, so the palette entry carries
// the number the user is about to change.
func (m *Model) scaleSubtitle(ref objectRef) string {
	if current, known := m.replicasOf(ref); known {
		return "currently " + replicaCount(current)
	}
	return ref.Namespace
}

// yamlTitle names what the command will do next.
func yamlTitle(showing bool) string {
	if showing {
		return "Show what kubeui knows about this object"
	}
	return "Show the document the server holds"
}

func wideTitle(wide bool) string {
	if wide {
		return "Hide wide columns"
	}
	return "Show wide columns"
}

// openResource switches the main view to a resource table.
func (m *Model) openResource(fullName string) tea.Cmd {
	catalog := m.catalog.Get()
	if catalog == nil {
		m.notice("Resource kinds are not discovered yet", theme.StatusWarning)
		return tea.Batch(m.loadCatalog(), m.expireNotice())
	}
	res, ok := catalog.Lookup(fullName)
	if !ok {
		m.notice("Unknown resource "+fullName, theme.StatusWarning)
		return m.expireNotice()
	}

	m.resource = res
	m.view = viewTable
	m.tablePort.Cursor = 0
	m.tablePort.Offset = 0
	m.loadingMore = false
	m.table.Reset()
	m.rebuildCommands()
	return m.loadTable()
}

// backToOverview leaves the resource browser.
func (m *Model) backToOverview() tea.Cmd {
	if m.view == viewOverview {
		return nil
	}
	if m.cancelTable != nil {
		m.cancelTable()
	}
	m.view = viewOverview
	m.table.Reset()
	m.rebuildCommands()
	return nil
}

// toggleWide switches between the compact and the wide column set.
func (m *Model) toggleWide() tea.Cmd {
	m.tableWide = !m.tableWide
	m.rebuildCommands()
	return nil
}

func allNamespacesTitle(active bool) string {
	if active {
		return "Leave all-namespaces scope"
	}
	return "Scope to all namespaces"
}

// runCommand executes a palette entry.
func (m *Model) runCommand(id string) tea.Cmd {
	var cmd palette.Command
	found := false
	for _, c := range m.registry.Commands() {
		if c.ID == id {
			cmd, found = c, true
			break
		}
	}
	if !found || !cmd.Enabled {
		return nil
	}
	m.registry.MarkUsed(id)
	m.closeOverlay()

	switch cmd.Action {
	case paletteOpenContexts:
		return m.openOverlay(overlayContexts)
	case paletteOpenNamespaces:
		return m.openOverlay(overlayNamespaces)
	case paletteOpenApps:
		return m.backToApplications()
	case paletteOpenFleet:
		return m.openFleet()
	case paletteFleetEverything:
		return m.includeEveryContext()
	case paletteOpenApp:
		return m.openApplication(cmd.Arg)
	case paletteExplain:
		if cmd.Arg != "" {
			return m.explainApplication(cmd.Arg)
		}
		return m.explain()
	case paletteOpenResources:
		return m.openOverlay(overlayResources)
	case paletteOpenResource:
		if m.view == viewFleet || m.view == viewFleetResource {
			return m.openFleetResourceByName(cmd.Arg)
		}
		return m.openResource(cmd.Arg)
	case paletteToggleWide:
		return m.toggleWide()
	case paletteToggleYAML:
		m.toggleObjectYAML()
		return nil
	case paletteScale:
		return m.scaleTarget()
	case paletteEdit:
		return m.editObject(m.objectTarget)
	case paletteLogs:
		return m.openLogs()
	case paletteBackToOverview:
		return m.backToOverview()
	case paletteSwitchContext:
		return m.switchContext(cmd.Arg)
	case paletteSwitchNamespace:
		return m.switchNamespace(cmd.Arg)
	case paletteToggleAllNS:
		return m.toggleAllNamespaces()
	case paletteRefresh:
		return m.refresh()
	case paletteAutoRefresh:
		return m.toggleAutoRefresh()
	case paletteReloadConfig:
		m.notice("Reloading kubeconfig…", theme.StatusUnknown)
		return m.reloadKubeconfig()
	case paletteHelp:
		return m.openOverlay(overlayHelp)
	case paletteQuit:
		m.quitting = true
		return tea.Quit
	}
	return nil
}

// switchContext moves the session to another cluster. It never writes to the
// kubeconfig: an external kubectl keeps pointing where the user left it.
func (m *Model) switchContext(name string) tea.Cmd {
	return m.switchContextScoped(name, "")
}

// switchContextScoped is the same move with the namespace decided by the
// caller, which is what arriving from the fleet needs: the cluster *and* the
// namespace the thing you clicked on lives in.
func (m *Model) switchContextScoped(name, namespace string) tea.Cmd {
	if name == "" || name == m.contextName {
		return nil
	}
	kctx, ok := m.kubeconfig.Context(name)
	if !ok {
		m.notice("Context "+name+" is no longer in the kubeconfig", theme.StatusWarning)
		return m.expireNotice()
	}

	m.contextName = name
	m.namespace = kctx.Namespace
	if namespace != "" {
		m.namespace = namespace
	}
	m.allNamespaces = false
	m.cluster.Reset()
	m.namespaces.Reset()
	m.catalog.Reset()
	m.table.Reset()
	m.apps.Reset()
	m.evidence.Reset()
	m.object.Reset()
	m.stopLogs()
	m.stopFleet()
	m.findings = nil
	m.appPort.Cursor, m.appPort.Offset, m.detailPort.Offset, m.detailPort.Cursor = 0, 0, 0, 0
	m.selectedApp = ""
	m.objectTarget, m.objectTrail = objectRef{}, nil
	m.view = viewApplications
	m.nsPicker.Reset()
	m.resPicker.Reset()
	m.rebuildCommands()

	label := "Switched to " + name
	status := theme.StatusUnknown
	if kctx.Production {
		label = "Switched to production cluster " + name
		status = theme.StatusWarning
	}
	m.notice(label, status)

	return tea.Batch(
		m.probeCluster(),
		m.loadNamespaces(),
		m.loadCatalog(),
		m.loadApplications(),
		m.expireNotice(),
	)
}

// switchNamespace changes the active scope.
func (m *Model) switchNamespace(ns string) tea.Cmd {
	if ns == allNamespacesID {
		return m.setAllNamespaces(true)
	}
	if ns == "" {
		return nil
	}
	m.allNamespaces = false
	m.namespace = ns
	m.rebuildCommands()
	m.notice("Scope: "+ns, theme.StatusUnknown)
	return tea.Batch(m.reloadScopedViews(), m.expireNotice())
}

// reloadScopedViews refetches whatever the active scope changes. Rows for the
// previous namespace must never linger under a new heading.
func (m *Model) reloadScopedViews() tea.Cmd {
	// The dashboard is always scoped, so it always reloads.
	m.apps.Reset()
	m.evidence.Reset()
	m.object.Reset()
	m.stopLogs()
	m.stopFleet()
	m.findings = nil
	m.appPort.Cursor, m.appPort.Offset, m.detailPort.Offset, m.detailPort.Cursor = 0, 0, 0, 0
	m.selectedApp = ""
	m.objectTarget, m.objectTrail = objectRef{}, nil
	if m.view == viewApplication || m.view == viewWhy || m.view == viewObject || m.view == viewLogs {
		m.view = viewApplications
	}
	reload := m.loadApplications()

	if m.view != viewTable || !m.resource.Namespaced {
		return reload
	}
	// Reset rather than refresh: the loaded rows belong to the previous scope,
	// and leaving them on screen under a new heading is precisely the kind of
	// lie kubeui must not tell.
	m.table.Reset()
	m.tablePort.Cursor = 0
	m.tablePort.Offset = 0
	m.loadingMore = false
	return tea.Batch(reload, m.loadTable())
}

func (m *Model) toggleAllNamespaces() tea.Cmd {
	return m.setAllNamespaces(!m.allNamespaces)
}

func (m *Model) setAllNamespaces(on bool) tea.Cmd {
	m.allNamespaces = on
	m.rebuildCommands()
	m.notice("Scope: "+m.scopeLabel(), theme.StatusUnknown)
	return tea.Batch(m.reloadScopedViews(), m.expireNotice())
}

// toggleAutoRefresh turns the timed reload on or off.
//
// It is a toggle rather than a setting because the cost is real: every tick is
// a round trip to somebody's production API server, and the user is the one who
// knows whether that is welcome right now.
func (m *Model) toggleAutoRefresh() tea.Cmd {
	m.autoRefresh = !m.autoRefresh
	// A new sequence retires the previous ticker, so toggling twice does not
	// leave two loops refreshing the same screen.
	m.refreshSeq++
	m.refreshFailures = 0
	m.rebuildCommands()

	if !m.autoRefresh {
		m.notice("Auto-refresh off", theme.StatusUnknown)
		return m.expireNotice()
	}
	m.notice("Auto-refresh every "+m.refreshEvery.String(), theme.StatusUnknown)
	return tea.Batch(
		tea.Batch(m.autoReload()...),
		scheduleAutoRefresh(m.refreshSeq, m.refreshEvery),
		m.expireNotice(),
	)
}

func autoRefreshTitle(on bool) string {
	if on {
		return "Stop refreshing automatically"
	}
	return "Refresh automatically"
}

func autoRefreshSubtitle(on bool, every time.Duration) string {
	if on {
		return "every " + every.String() + ", on"
	}
	return "every " + every.String() + ", off"
}

// refresh re-probes the cluster and reloads everything the current screen shows.
func (m *Model) refresh() tea.Cmd {
	m.notice("Refreshing…", theme.StatusUnknown)
	cmds := []tea.Cmd{m.probeCluster(), m.loadNamespaces(), m.loadApplications(), m.expireNotice()}
	if m.view == viewTable {
		cmds = append(cmds, m.loadTable())
	}
	if m.view == viewApplication || m.view == viewWhy {
		cmds = append(cmds, m.loadEvidence())
	}
	return tea.Batch(cmds...)
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}
