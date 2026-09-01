package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	kubeclient "github.com/akiesel/kubeui/internal/kube/client"
	"github.com/akiesel/kubeui/internal/kube/kubeconfig"
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

type kubeconfigReloadedMsg struct {
	cfg *kubeconfig.Config
	err error
}

type resizeSettledMsg struct{}

type messageExpiredMsg struct{ seq uint64 }

// probeCluster starts a connectivity probe for the active context, cancelling
// any probe still in flight for a context the user has left.
func (m *Model) probeCluster() tea.Cmd {
	if m.cancelCluster != nil {
		m.cancelCluster()
	}
	gen := m.cluster.Start()

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
