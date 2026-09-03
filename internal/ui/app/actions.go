package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/logs"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// Palette action identifiers. Direct actions carry their target in Command.Arg,
// so "switch to prod-eu" is one keystroke away without opening a submenu.
const (
	paletteOpenContexts     palette.ActionID = "open.contexts"
	paletteOpenNamespaces   palette.ActionID = "open.namespaces"
	paletteSwitchContext    palette.ActionID = "switch.context"
	paletteSwitchNamespace  palette.ActionID = "switch.namespace"
	paletteToggleAllNS      palette.ActionID = "toggle.allnamespaces"
	paletteOpenApps         palette.ActionID = "open.applications"
	paletteOpenFleet        palette.ActionID = "open.fleet"
	paletteFleetEverything  palette.ActionID = "fleet.everything"
	paletteSwitchFleetGroup palette.ActionID = "fleet.group"
	paletteChooseFleet      palette.ActionID = "fleet.choose"
	paletteNewFleetGroup    palette.ActionID = "fleet.group.new"
	paletteDeleteFleetGroup palette.ActionID = "fleet.group.delete"
	paletteOpenApp          palette.ActionID = "open.application"
	paletteExplain          palette.ActionID = "explain"
	paletteGrouping         palette.ActionID = "application.grouping"
	paletteToggleYAML       palette.ActionID = "object.yaml"
	paletteToggleDecode     palette.ActionID = "object.decode"
	paletteScale            palette.ActionID = "scale"
	paletteEdit             palette.ActionID = "edit"
	paletteExec             palette.ActionID = "exec"
	paletteCopy             palette.ActionID = "copy"
	paletteCopyYAML         palette.ActionID = "copy.yaml"
	paletteCopyJSON         palette.ActionID = "copy.json"
	paletteCopyKubectl      palette.ActionID = "copy.kubectl"
	paletteCopyLogs         palette.ActionID = "copy.logs"
	paletteCopyTable        palette.ActionID = "copy.table"
	paletteLogs             palette.ActionID = "logs"
	paletteUsage            palette.ActionID = "usage"
	paletteActivity         palette.ActionID = "activity"
	paletteOpenResources    palette.ActionID = "open.resources"
	paletteOpenResource     palette.ActionID = "open.resource"
	paletteToggleWide       palette.ActionID = "toggle.wide"
	paletteBackToOverview   palette.ActionID = "open.overview"
	paletteRefresh          palette.ActionID = "refresh"
	paletteAutoRefresh      palette.ActionID = "refresh.auto"
	paletteReloadConfig     palette.ActionID = "reload.kubeconfig"
	paletteHelp             palette.ActionID = "help"
	paletteQuit             palette.ActionID = "quit"
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
			ID:       "cmd.usage",
			Action:   paletteUsage,
			Title:    "Show resource usage",
			Subtitle: m.usageSubtitleForPalette(),
			Category: "Cluster",
			Keywords: []string{
				"usage", "resources", "cpu", "memory", "top", "capacity",
				"requests", "limits", "nodes", "where", "full", "quota",
			},
			Shortcut: m.keys.Key(ActionUsage),
			Weight:   89,
			Enabled:  true,
		},
		{
			ID:       "cmd.activity",
			Action:   paletteActivity,
			Title:    "Show recent cluster activity",
			Subtitle: "Kubernetes Events in " + m.scopeLabel() + "; not an audit log",
			Category: "Diagnose",
			Keywords: []string{"events", "recent", "activity", "changes", "timeline", "history"},
			Shortcut: m.keys.Key(ActionActivity),
			Weight:   90,
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
			Title:    "Quit Correlux",
			Category: "Help",
			Keywords: []string{"exit", "close"},
			Shortcut: m.keys.Key(ActionQuit),
			Weight:   10,
			Enabled:  true,
		},
	}

	if m.view == viewApplications || m.view == viewApplication {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.grouping",
			Action:   paletteGrouping,
			Title:    groupingTitle(m.groupingShown),
			Subtitle: "which signal put each object here, and how sure it is",
			Category: "Diagnose",
			Keywords: []string{"why", "grouping", "reason", "owner", "label", "selector", "guess", "certain"},
			Shortcut: m.keys.Key(ActionGrouping),
			Weight:   96,
			Enabled:  true,
		})
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

	if label, value, ok := m.copyTarget(); ok {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.copy",
			Action:   paletteCopy,
			Title:    "Copy " + label,
			Subtitle: clipTo(value, 60, 1),
			Category: "Copy",
			Keywords: []string{"copy", "clipboard", "yank", "name"},
			Shortcut: m.keys.Key(ActionCopy),
			Weight:   65,
			Enabled:  true,
		})
	}

	if target, title, ok := m.execTarget(); ok {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.exec",
			Action:   paletteExec,
			Title:    title,
			Subtitle: target.Label(),
			Category: "Inspect",
			Keywords: []string{"exec", "shell", "terminal", "attach", "bash", "sh", "ssh"},
			Shortcut: m.keys.Key(ActionExec),
			Weight:   82,
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
			Subtitle: "for this session; Correlux will authenticate against all of them",
			Category: "Navigate",
			Keywords: []string{"fleet", "every", "all contexts", "add"},
			Weight:   50,
			Enabled:  true,
		})
	}

	// Choosing the clusters comes before switching between groups, because a
	// fleet nobody has chosen yet is the state every new install is in and the
	// one the old configuration-file-only answer left people stuck in.
	cmds = append(cmds, palette.Command{
		ID:       "cmd.fleet.choose",
		Action:   paletteChooseFleet,
		Arg:      m.fleetGroupLabel(),
		Title:    "Choose the clusters in " + m.fleetGroupLabel(),
		Subtitle: chooseFleetSubtitle(len(m.groupContexts(m.activeFleetGroup))),
		Category: "Navigate",
		Keywords: []string{"fleet", "clusters", "choose", "pick", "edit", "add", "group"},
		Weight:   93,
		Enabled:  len(m.kubeconfig.Contexts) > 0,
	}, palette.Command{
		ID:       "cmd.fleet.group.new",
		Action:   paletteNewFleetGroup,
		Title:    "New fleet group…",
		Subtitle: "keep production, staging or a region apart",
		Category: "Navigate",
		Keywords: []string{"fleet", "group", "new", "create", "environment", "team", "region"},
		Weight:   48,
		Enabled:  len(m.kubeconfig.Contexts) > 0,
	})
	for _, group := range m.cfg.FleetGroups {
		cmds = append(cmds, palette.Command{
			ID:       "fleet.group.delete." + group.Name,
			Action:   paletteDeleteFleetGroup,
			Arg:      group.Name,
			Title:    "Delete fleet group " + group.Name,
			Subtitle: "the grouping only; the clusters stay in your kubeconfig",
			Category: "Navigate",
			Keywords: []string{"fleet", "group", "delete", "remove"},
			Weight:   20,
			Enabled:  true,
		})
	}

	for _, group := range m.fleetGroups() {
		if group.Name == m.fleetGroupLabel() {
			continue
		}
		cmds = append(cmds, palette.Command{
			ID:       "fleet.group." + group.Name,
			Action:   paletteSwitchFleetGroup,
			Arg:      group.Name,
			Title:    "Open fleet group " + group.Name,
			Subtitle: itoa(len(group.Contexts)) + " clusters",
			Category: "Navigate",
			Keywords: []string{"fleet", "group", "environment", "team", "region"},
			Weight:   91,
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
		// Offered only when the document actually has something to decode — a
		// Secret's values, above all — so the palette never lists an action that
		// would just tell you there was nothing to do.
		if m.objectDecodable() {
			cmds = append(cmds, palette.Command{
				ID:       "cmd.decode",
				Action:   paletteToggleDecode,
				Title:    decodeTitle(m.objectDecode),
				Subtitle: m.objectTarget.label(),
				Category: "View",
				Keywords: []string{"base64", "decode", "secret", "reveal", "plain", "value"},
				Shortcut: m.keys.Key(ActionDecode),
				Weight:   84,
				Enabled:  true,
			})
		}
		if m.object.State() == async.Ready {
			cmds = append(cmds,
				palette.Command{
					ID:       "cmd.copy.yaml",
					Action:   paletteCopyYAML,
					Title:    "Copy the YAML document",
					Subtitle: m.objectTarget.label(),
					Category: "Copy",
					Keywords: []string{"copy", "clipboard", "yaml", "manifest"},
					Weight:   64,
					Enabled:  true,
				},
				palette.Command{
					ID:       "cmd.copy.json",
					Action:   paletteCopyJSON,
					Title:    "Copy the JSON the server holds",
					Subtitle: m.objectTarget.label(),
					Category: "Copy",
					Keywords: []string{"copy", "clipboard", "json", "raw"},
					Weight:   63,
					Enabled:  true,
				},
				palette.Command{
					ID:       "cmd.copy.kubectl",
					Action:   paletteCopyKubectl,
					Title:    "Copy the equivalent kubectl command",
					Subtitle: kubectlGet(m, m.objectTarget),
					Category: "Copy",
					Keywords: []string{"copy", "clipboard", "kubectl", "command"},
					Weight:   62,
					Enabled:  true,
				},
			)
		}
	}

	if m.view == viewLogs && len(m.logLines) > 0 {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.copy.logs",
			Action:   paletteCopyLogs,
			Title:    "Copy what is on screen",
			Subtitle: itoa(len(m.logLines)) + " lines",
			Category: "Copy",
			Keywords: []string{"copy", "clipboard", "logs"},
			Weight:   64,
			Enabled:  true,
		})
	}

	if m.view == viewTable && len(m.visibleRows()) > 0 {
		cmds = append(cmds, palette.Command{
			ID:       "cmd.copy.table",
			Action:   paletteCopyTable,
			Title:    "Copy the table as text",
			Subtitle: itoa(len(m.visibleRows())) + " rows, tab-separated",
			Category: "Copy",
			Keywords: []string{"copy", "clipboard", "table", "csv", "tsv"},
			Weight:   64,
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
		return "Show what Correlux knows about this object"
	}
	return "Show the document the server holds"
}

// decodeTitle names what the command will do next.
func decodeTitle(decoding bool) string {
	if decoding {
		return "Show the values as the server stores them"
	}
	return "Decode the base64 values in this document"
}

// groupingTitle names what the command will do next.
func groupingTitle(shown bool) string {
	if shown {
		return "Hide why these objects are grouped together"
	}
	return "Show why these objects are grouped together"
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
	case paletteSwitchFleetGroup:
		return m.switchFleetGroup(cmd.Arg)
	case paletteChooseFleet:
		m.closeOverlay()
		return m.openFleetPicker(m.activeFleetGroup)
	case paletteNewFleetGroup:
		return m.promptNewFleetGroup()
	case paletteDeleteFleetGroup:
		m.closeOverlay()
		return m.deleteFleetGroup(cmd.Arg)
	case paletteOpenApp:
		return m.openApplication(cmd.Arg)
	case paletteExplain:
		if cmd.Arg != "" {
			return m.explainApplication(cmd.Arg)
		}
		return m.explain()
	case paletteGrouping:
		return m.toggleGrouping()
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
	case paletteToggleDecode:
		m.toggleObjectDecode()
		return nil
	case paletteScale:
		return m.scaleTarget()
	case paletteEdit:
		return m.editObject(m.objectTarget)
	case paletteExec:
		return m.openExec()
	case paletteCopy:
		return m.copyPrimary()
	case paletteCopyYAML:
		return m.copyObjectYAML()
	case paletteCopyJSON:
		return m.copyObjectJSON()
	case paletteCopyKubectl:
		return m.copyKubectlCommand()
	case paletteCopyLogs:
		return m.copyVisibleLogs()
	case paletteCopyTable:
		return m.copyVisibleTable()
	case paletteLogs:
		return m.openLogs()
	case paletteUsage:
		return m.openUsage()
	case paletteActivity:
		return m.openActivity()
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
	m.resetUsage()
	m.usageDrilledIn = false
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
	m.resetUsage()
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
	if m.view == viewUsage {
		// The numbers belong to the scope they were read in; a new scope means
		// a new reading, not the old one under a new heading.
		reload = tea.Batch(reload, m.loadUsage())
	}
	if m.view == viewActivity {
		reload = tea.Batch(reload, m.loadEvidence())
	}

	if m.view != viewTable || !m.resource.Namespaced {
		return reload
	}
	// Reset rather than refresh: the loaded rows belong to the previous scope,
	// and leaving them on screen under a new heading is precisely the kind of
	// lie Correlux must not tell.
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
//
// It says so in the header for exactly as long as the reload runs. The notice
// it used to raise instead sat on the status bar for five seconds whatever
// happened — hiding every key hint behind it, and telling anybody whose
// cluster answered in a tenth of a second that refreshing takes five.
func (m *Model) refresh() tea.Cmd {
	cmds := []tea.Cmd{m.beginBusy(true), m.probeCluster(), m.loadNamespaces(), m.loadApplications()}
	if m.view == viewTable {
		cmds = append(cmds, m.loadTable())
	}
	if m.view == viewUsage {
		cmds = append(cmds, m.loadUsage())
	}
	if m.view == viewApplication || m.view == viewWhy || m.view == viewActivity {
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
