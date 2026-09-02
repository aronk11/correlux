package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/buildinfo"
	"github.com/aronk11/kubeui/internal/config"
	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/kubeconfig"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/async"
	"github.com/aronk11/kubeui/internal/ui/components"
	"github.com/aronk11/kubeui/internal/ui/layout"
	"github.com/aronk11/kubeui/internal/ui/palette"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// overlayKind identifies the modal currently on screen.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayPalette
	overlayContexts
	overlayNamespaces
	overlayResources
	overlayHelp
)

// viewKind identifies the full-window view behind any overlay.
type viewKind int

const (
	// viewApplications is the dashboard kubeui opens on: what is deployed here
	// and what state it is in. It is the zero value because it is home.
	viewApplications viewKind = iota
	// viewApplication is one application and the objects it is made of.
	viewApplication
	// viewOverview is the session and connection summary.
	viewOverview
	// viewTable lists the objects of one resource kind.
	viewTable
)

// Options configures the application at start-up.
type Options struct {
	Config        config.Config
	Kubeconfig    *kubeconfig.Config
	Factory       *kubeclient.Factory
	Classifier    *kubeconfig.Classifier
	ContextName   string
	Namespace     string
	AllNamespaces bool
	// ConfigWarnings are non-fatal problems found while starting up; they are
	// shown once so the user knows their config was not fully honoured.
	ConfigWarnings []string
	// Env supplies environment lookups for capability detection.
	Env theme.Env
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg        config.Config
	keys       KeyMap
	theme      *theme.Theme
	caps       theme.Capabilities
	kubeconfig *kubeconfig.Config
	factory    *kubeclient.Factory
	classifier *kubeconfig.Classifier

	// Session state: what the next keystroke will act on.
	contextName   string
	namespace     string
	allNamespaces bool

	// Remote state, each with an explicit lifecycle.
	cluster    async.Value[kubeclient.ClusterInfo]
	namespaces async.Value[kubeclient.NamespaceList]
	catalog    async.Value[*kubediscovery.Catalog]
	table      async.Value[*resources.Table]
	apps       async.Value[applicationList]

	// The application dashboard.
	appCursor int
	appOffset int
	// selectedApp is the application the detail view shows, addressed by key
	// rather than by index: a reload re-sorts the list, and a cursor position
	// would then follow the ranking instead of the application.
	selectedApp  string
	detailOffset int

	// The resource browser.
	view        viewKind
	resource    kubediscovery.Resource
	tableCursor int
	tableOffset int
	tableWide   bool
	loadingMore bool

	// The timed reload. It is off until the user turns it on, and it only ever
	// refetches what is on screen.
	autoRefresh     bool
	refreshEvery    time.Duration
	refreshSeq      uint64
	refreshFailures int

	// In-flight markers, so a timed reload never stacks a second request on
	// top of one that has not answered yet.
	appsLoading    bool
	tableLoading   bool
	clusterLoading bool

	// In-flight work, cancelled when it becomes irrelevant.
	cancelCluster    context.CancelFunc
	cancelNamespaces context.CancelFunc
	cancelCatalog    context.CancelFunc
	cancelTable      context.CancelFunc
	cancelApps       context.CancelFunc

	// Geometry.
	screen        layout.Screen
	width, height int
	resize        *layout.Debouncer

	// Overlays.
	overlay   overlayKind
	registry  *palette.Registry
	cmdPal    *components.Selector
	ctxPicker *components.Selector
	nsPicker  *components.Selector
	resPicker *components.Selector

	// Transient status message.
	message       string
	messageStatus theme.Status
	messageSeq    uint64

	// configPath is where the config file lives, whether or not it exists yet,
	// so help can tell the user where to put their keybindings.
	configPath string

	ready    bool
	quitting bool
}

// New builds the root model. It performs no I/O: the first Kubernetes call
// happens in Init, so the UI paints immediately even against a dead cluster.
func New(opts Options) *Model {
	env := opts.Env
	if env == nil {
		env = theme.OSEnv
	}
	caps := theme.DetectCapabilities(env)
	keys, unknown := NewKeyMap(opts.Config.Keybindings)

	m := &Model{
		cfg:           opts.Config,
		keys:          keys,
		caps:          caps,
		theme:         theme.New(caps, opts.Config.Theme),
		kubeconfig:    opts.Kubeconfig,
		factory:       opts.Factory,
		classifier:    opts.Classifier,
		contextName:   opts.ContextName,
		namespace:     opts.Namespace,
		allNamespaces: opts.AllNamespaces,
		resize:        layout.NewDebouncer(layout.DefaultResizeDebounce),
		registry:      palette.NewRegistry(),
	}

	// A malformed interval is reported by the caller as a config warning; the
	// UI keeps working on the default.
	m.refreshEvery, _ = opts.Config.Refresh.Interval()
	m.autoRefresh = opts.Config.Refresh.Auto

	m.configPath = opts.Config.SourcePath
	if m.configPath == "" {
		if path, err := config.Path(); err == nil {
			m.configPath = path
		}
	}

	if m.namespace == "" {
		if kctx, ok := m.kubeconfig.Context(m.contextName); ok {
			m.namespace = kctx.Namespace
		}
	}

	m.cmdPal = components.NewSelector("Command", "Type a command…", m.filterCommands)
	m.cmdPal.EmptyMessage = "No command matches."
	m.cmdPal.Footer = "Enter run   Esc cancel"

	m.ctxPicker = components.NewSelector("Clusters", "Filter clusters…", m.filterContexts)
	m.ctxPicker.EmptyMessage = "No context matches."
	m.ctxPicker.Footer = "Enter switch   Esc cancel"

	m.nsPicker = components.NewSelector("Namespaces", "Filter namespaces…", m.filterNamespaces)
	m.nsPicker.Footer = "Enter switch   Esc cancel"

	m.resPicker = components.NewSelector("Resources", "Filter resource kinds…", m.filterResources)
	m.resPicker.EmptyMessage = "No resource kind matches."
	m.resPicker.Footer = "Enter open   Esc cancel"

	warnings := opts.ConfigWarnings
	for _, action := range unknown {
		warnings = append(warnings, "unknown keybinding action in config: "+action)
	}
	if len(warnings) > 0 {
		m.message = warnings[0]
		m.messageStatus = theme.StatusWarning
	}

	m.rebuildCommands()
	return m
}

// Init starts the first round of work: measure the terminal, learn its
// background colour, and probe the cluster.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.RequestBackgroundColor,
		m.probeCluster(),
		m.loadNamespaces(),
		m.loadCatalog(),
		m.loadApplications(),
		expireMessage(m.messageSeq, 8*time.Second),
	}
	if m.autoRefresh {
		cmds = append(cmds, scheduleAutoRefresh(m.refreshSeq, m.refreshEvery))
	}
	return tea.Batch(cmds...)
}

// Context reports the active kubeconfig context.
func (m *Model) Context() string { return m.contextName }

// Namespace reports the active namespace scope.
func (m *Model) Namespace() string { return m.namespace }

// currentContext returns the kubeconfig entry for the active context.
func (m *Model) currentContext() kubeconfig.Context {
	kctx, _ := m.kubeconfig.Context(m.contextName)
	return kctx
}

// scopeLabel renders the active scope for the header.
func (m *Model) scopeLabel() string {
	if m.allNamespaces {
		return "all namespaces"
	}
	if m.namespace == "" {
		return "default"
	}
	return m.namespace
}

// version renders the build version for the header.
func (m *Model) version() string { return buildinfo.Get().Version }

// OpenResourceForTest opens a resource kind by its kubectl-style name. It
// exists so integration tests can drive the application the way a user does,
// without exporting the whole action surface.
func (m *Model) OpenResourceForTest(name string) tea.Cmd { return m.openResource(name) }

// SwitchNamespaceForTest changes the active scope from an integration test.
func (m *Model) SwitchNamespaceForTest(namespace string) tea.Cmd { return m.switchNamespace(namespace) }

// ShowSessionForTest switches to the session and connection view.
func (m *Model) ShowSessionForTest() tea.Cmd { return m.backToOverview() }

// OpenApplicationForTest opens one application's detail view by name.
func (m *Model) OpenApplicationForTest(name string) tea.Cmd { return m.openApplication(name) }
