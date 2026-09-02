package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/kubeconfig"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/kube/workloads"
)

// Messages carrying the result of asynchronous work. Every one of them is
// tagged with the generation of the request that produced it, so a reply that
// belongs to a cluster the user has already left is dropped instead of
// overwriting fresh state.

type clusterProbedMsg struct {
	gen  uint64
	info kubeclient.ClusterInfo
}

type namespacesLoadedMsg struct {
	gen  uint64
	list kubeclient.NamespaceList
	err  error
}

type catalogLoadedMsg struct {
	gen     uint64
	catalog *kubediscovery.Catalog
	err     error
}

type tableLoadedMsg struct {
	gen   uint64
	table *resources.Table
	// append is true when this page extends the table instead of replacing it.
	append bool
	err    error
}

type applicationsLoadedMsg struct {
	gen  uint64
	list applicationList
	err  error
}

type evidenceLoadedMsg struct {
	gen     uint64
	context application.Context
	err     error
}

type kubeconfigReloadedMsg struct {
	cfg *kubeconfig.Config
	err error
}

type autoRefreshTickMsg struct{ seq uint64 }

type resizeSettledMsg struct{}

type messageExpiredMsg struct{ seq uint64 }

// probeCluster starts a connectivity probe for the active context, cancelling
// any probe still in flight for a context the user has left.
func (m *Model) probeCluster() tea.Cmd {
	if m.cancelCluster != nil {
		m.cancelCluster()
	}
	gen := m.cluster.Start()
	m.clusterLoading = true

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelCluster = cancel

	factory := m.factory
	name := m.contextName
	return func() tea.Msg {
		defer cancel()
		return clusterProbedMsg{gen: gen, info: factory.Probe(ctx, name)}
	}
}

// loadNamespaces fetches the namespace list for the active context.
func (m *Model) loadNamespaces() tea.Cmd {
	if m.cancelNamespaces != nil {
		m.cancelNamespaces()
	}
	gen := m.namespaces.Start()

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelNamespaces = cancel

	factory := m.factory
	name := m.contextName
	return func() tea.Msg {
		defer cancel()
		list, err := factory.ListNamespaces(ctx, name)
		return namespacesLoadedMsg{gen: gen, list: list, err: err}
	}
}

// loadCatalog discovers which resource kinds this cluster serves, custom
// resources included.
func (m *Model) loadCatalog() tea.Cmd {
	if m.cancelCatalog != nil {
		m.cancelCatalog()
	}
	gen := m.catalog.Start()

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelCatalog = cancel

	factory := m.factory
	name := m.contextName
	return func() tea.Msg {
		defer cancel()
		catalog, err := factory.Catalog(ctx, name)
		return catalogLoadedMsg{gen: gen, catalog: catalog, err: err}
	}
}

// loadTable fetches the first page of the active resource.
func (m *Model) loadTable() tea.Cmd {
	if m.cancelTable != nil {
		m.cancelTable()
	}
	gen := m.table.Start()
	m.tableLoading = true
	return m.fetchTable(gen, "", false)
}

// loadMoreRows fetches the next page and appends it, so scrolling through a
// large resource never blocks on a single unbounded list.
func (m *Model) loadMoreRows() tea.Cmd {
	current := m.table.Get()
	if current == nil || !current.HasMore() || m.loadingMore {
		return nil
	}
	m.loadingMore = true
	return m.fetchTable(m.table.Generation(), current.Continue, true)
}

func (m *Model) fetchTable(gen uint64, continueToken string, appendPage bool) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	if !appendPage {
		m.cancelTable = cancel
	}

	factory := m.factory
	name := m.contextName
	res := m.resource
	opts := resources.ListOptions{Continue: continueToken}
	if res.Namespaced && !m.allNamespaces {
		opts.Namespace = m.namespace
	}

	return func() tea.Msg {
		defer cancel()
		table, err := factory.ListTable(ctx, name, res, opts)
		return tableLoadedMsg{gen: gen, table: table, append: appendPage, err: err}
	}
}

// loadApplications reads the active scope and groups it into applications.
//
// It is one command rather than nine, because the user asked one question —
// "what is deployed here and is it healthy?" — and a dashboard that fills in
// kind by kind is a dashboard that is never quite finished.
func (m *Model) loadApplications() tea.Cmd {
	if m.cancelApps != nil {
		m.cancelApps()
	}
	gen := m.apps.Start()
	m.appsLoading = true

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelApps = cancel

	factory := m.factory
	name := m.contextName
	opts := workloads.Options{}
	if !m.allNamespaces {
		opts.Namespace = m.namespace
	}

	return func() tea.Msg {
		defer cancel()
		apps, snapshot, err := factory.Applications(ctx, name, opts)
		return applicationsLoadedMsg{
			gen:  gen,
			list: applicationList{Apps: apps, Snapshot: snapshot},
			err:  err,
		}
	}
}

// loadEvidence fetches what a diagnosis reasons about: events, endpoints, nodes
// and volume claims for the active scope.
//
// It is deliberately not part of the dashboard's load. The dashboard refreshes
// on a timer and has to stay cheap; this runs when somebody opens an
// application or asks why it is broken (ADR 18).
func (m *Model) loadEvidence() tea.Cmd {
	if m.cancelEvidence != nil {
		m.cancelEvidence()
	}
	gen := m.evidence.Start()
	m.evidenceLoading = true

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelEvidence = cancel

	factory := m.factory
	name := m.contextName
	opts := workloads.Options{}
	if !m.allNamespaces {
		opts.Namespace = m.namespace
	}

	return func() tea.Msg {
		defer cancel()
		evidence, err := factory.ApplicationContext(ctx, name, opts)
		return evidenceLoadedMsg{gen: gen, context: evidence, err: err}
	}
}

// reloadKubeconfig re-reads the kubeconfig from disk, picking up contexts added
// since kubeui started.
func (m *Model) reloadKubeconfig() tea.Cmd {
	classifier := m.classifier
	explicit := ""
	if len(m.kubeconfig.Sources) == 1 {
		explicit = m.kubeconfig.Sources[0]
	}
	return func() tea.Msg {
		cfg, err := kubeconfig.Load(kubeconfig.LoadOptions{
			ExplicitPath: explicit,
			Classifier:   classifier,
		})
		return kubeconfigReloadedMsg{cfg: cfg, err: err}
	}
}

// scheduleAutoRefresh asks to be woken for the next timed reload. The sequence
// number retires the ticker of an earlier toggle, so turning the refresh off
// and on again leaves one loop running rather than two.
func scheduleAutoRefresh(seq uint64, every time.Duration) tea.Cmd {
	if every <= 0 {
		return nil
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return autoRefreshTickMsg{seq: seq} })
}

// scheduleResize asks to be woken once the resize burst has settled.
func scheduleResize(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg { return resizeSettledMsg{} })
}

// expireMessage clears a transient status message after a delay, unless a newer
// message has replaced it in the meantime.
func expireMessage(seq uint64, after time.Duration) tea.Cmd {
	if after <= 0 {
		return nil
	}
	return tea.Tick(after, func(time.Time) tea.Msg { return messageExpiredMsg{seq: seq} })
}
