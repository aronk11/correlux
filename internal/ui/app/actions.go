package app

import (
	tea "charm.land/bubbletea/v2"

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
	paletteOpenResources   palette.ActionID = "open.resources"
	paletteOpenResource    palette.ActionID = "open.resource"
	paletteToggleWide      palette.ActionID = "toggle.wide"
	paletteBackToOverview  palette.ActionID = "open.overview"
	paletteRefresh         palette.ActionID = "refresh"
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

	if m.view == viewTable {
		cmds = append(cmds,
			palette.Command{
				ID:       "cmd.wide",
				Action:   paletteToggleWide,
				Title:    wideTitle(m.tableWide),
				Subtitle: m.resource.Kind(),
				Category: "View",
				Keywords: []string{"wide", "columns", "-o wide", "details"},
				Shortcut: m.keys.Key(ActionToggleWide),
				Weight:   80,
				Enabled:  true,
			},
			palette.Command{
				ID:       "cmd.overview",
				Action:   paletteBackToOverview,
				Title:    "Back to the overview",
				Category: "Navigate",
				Keywords: []string{"overview", "home", "back"},
				Shortcut: "Esc",
				Weight:   75,
				Enabled:  true,
			},
		)
	}

	// Every resource kind the cluster serves is its own command, so "widgets"
	// is as reachable as "pods" without a submenu.
	if catalog := m.catalog.Get(); catalog != nil {
		for _, r := range catalog.Resources {
			if m.view == viewTable && r.FullName() == m.resource.FullName() {
				continue
			}
			keywords := append([]string{"resource", r.Plural(), r.Group()}, r.ShortNames...)
			subtitle := r.GroupVersion()
			if !r.Builtin {
				subtitle = "custom resource  " + subtitle
			}
			cmds = append(cmds, palette.Command{
				ID:       "res." + r.FullName(),
				Action:   paletteOpenResource,
				Arg:      r.FullName(),
				Title:    "Open " + r.Kind(),
				Subtitle: subtitle,
				Category: "Resources",
				Keywords: keywords,
				Weight:   55,
				Enabled:  true,
			})
		}
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
	m.tableCursor = 0
	m.tableOffset = 0
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
	case paletteOpenResources:
		return m.openOverlay(overlayResources)
	case paletteOpenResource:
		return m.openResource(cmd.Arg)
	case paletteToggleWide:
		return m.toggleWide()
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
	m.allNamespaces = false
	m.cluster.Reset()
	m.namespaces.Reset()
	m.catalog.Reset()
	m.table.Reset()
	m.view = viewOverview
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

	return tea.Batch(m.probeCluster(), m.loadNamespaces(), m.loadCatalog(), m.expireNotice())
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
	if m.view != viewTable || !m.resource.Namespaced {
		return nil
	}
	// Reset rather than refresh: the loaded rows belong to the previous scope,
	// and leaving them on screen under a new heading is precisely the kind of
	// lie kubeui must not tell.
	m.table.Reset()
	m.tableCursor = 0
	m.tableOffset = 0
	m.loadingMore = false
	return m.loadTable()
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

// refresh re-probes the cluster and reloads namespaces.
func (m *Model) refresh() tea.Cmd {
	m.notice("Refreshing…", theme.StatusUnknown)
	return tea.Batch(m.probeCluster(), m.loadNamespaces(), m.expireNotice())
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}
