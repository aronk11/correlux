package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// scaledMsg reports the outcome of a scale.
type scaledMsg struct {
	ref      objectRef
	replicas int32
	err      error
}

// scaleTarget scales whatever the current screen is pointing at: the open
// object, or the row the cursor is on in an application.
func (m *Model) scaleTarget() tea.Cmd {
	switch m.view {
	case viewObject:
		return m.askScale(m.objectTarget)
	case viewApplication:
		_, targets := m.applicationView()
		if m.detailPort.Cursor >= 0 && m.detailPort.Cursor < len(targets) {
			return m.askScale(targets[m.detailPort.Cursor])
		}
	}
	m.notice("Select a workload to scale", theme.StatusWarning)
	return m.expireNotice()
}

// askScale opens the prompt for a workload's new replica count.
func (m *Model) askScale(ref objectRef) tea.Cmd {
	res, ok := m.resourceFor(ref)
	if !ok {
		m.notice("This cluster does not serve "+ref.Kind, theme.StatusWarning)
		return m.expireNotice()
	}
	if !res.Scalable {
		// Read from discovery, so this is the server's answer rather than a
		// list of kinds kubeui happens to know.
		m.notice(ref.Kind+" has no scale subresource; it cannot be scaled", theme.StatusWarning)
		return m.expireNotice()
	}

	current, known := m.replicasOf(ref)
	m.promptTitle = "Scale " + ref.label()
	m.promptNote = "Currently " + replicaCount(current) + "."
	if !known {
		m.promptNote = "The current replica count is not loaded."
	}
	m.promptError = ""
	m.promptRef = ref
	m.promptInput.SetValue(strconv.Itoa(int(current)))
	m.promptAccept = func(m *Model, value string) tea.Cmd { return m.confirmScale(ref, current, value) }
	m.overlay = overlayPrompt
	m.refreshPrompt()
	return nil
}

// refreshPrompt keeps the note under the input true as the value is typed, so
// the consequence is visible before Enter rather than after it.
func (m *Model) refreshPrompt() {
	if m.overlay != overlayPrompt || m.promptRef.empty() {
		return
	}
	current, _ := m.replicasOf(m.promptRef)
	wanted, err := parseReplicas(m.promptInput.Value())
	if err != nil {
		m.promptNote = "Currently " + replicaCount(current) + "."
		return
	}
	m.promptNote = "Currently " + replicaCount(current) + ". " + blastRadius(current, wanted)
}

// acceptPrompt hands the typed value to whatever asked for it.
func (m *Model) acceptPrompt() tea.Cmd {
	if m.promptAccept == nil {
		m.cancelPrompt()
		return nil
	}
	return m.promptAccept(m, m.promptInput.Value())
}

func (m *Model) cancelPrompt() {
	m.promptAccept = nil
	m.promptRef = objectRef{}
	m.promptTitle, m.promptNote, m.promptError = "", "", ""
	m.promptInput.Reset()
	if m.overlay == overlayPrompt {
		m.closeOverlay()
	}
}

// confirmScale turns a typed number into a confirmation that states what it
// will do to the cluster.
func (m *Model) confirmScale(ref objectRef, current int32, value string) tea.Cmd {
	wanted, err := parseReplicas(value)
	if err != nil {
		m.promptError = err.Error()
		return nil
	}
	if wanted == current {
		m.cancelPrompt()
		m.notice(ref.label()+" already has "+replicaCount(current), theme.StatusUnknown)
		return m.expireNotice()
	}

	m.cancelPrompt()
	return m.confirm(pendingAction{
		Title: "Scale " + ref.label(),
		Lines: []string{
			blastRadius(current, wanted),
			ref.label() + " in " + orNone(ref.Namespace),
			replicaCount(current) + " → " + replicaCount(wanted),
		},
		Challenge: m.productionChallenge(),
		Danger:    wanted < current,
		Run:       func(m *Model) tea.Cmd { return m.scale(ref, wanted) },
	})
}

// scale performs the change.
func (m *Model) scale(ref objectRef, replicas int32) tea.Cmd {
	res, ok := m.resourceFor(ref)
	if !ok {
		return nil
	}
	factory := m.factory
	name := m.contextName
	namespace := ref.Namespace
	if !res.Namespaced {
		namespace = ""
	}

	m.notice("Scaling "+ref.label()+" to "+replicaCount(replicas)+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		err := factory.Scale(ctx, name, res, namespace, ref.Name, replicas)
		return scaledMsg{ref: ref, replicas: replicas, err: err}
	}
}

// applyScaled reports the outcome and refreshes what is on screen: the point of
// scaling is to watch it take effect.
func (m *Model) applyScaled(msg scaledMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Could not scale "+msg.ref.label()+": "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Scaled "+msg.ref.label()+" to "+replicaCount(msg.replicas), theme.StatusHealthy)

	cmds := []tea.Cmd{m.loadApplications(), m.expireNotice()}
	if m.view == viewObject && m.objectTarget == msg.ref {
		cmds = append(cmds, m.loadObject())
	}
	return tea.Batch(cmds...)
}

// blastRadius says what the change does, in the terms the user cares about.
func blastRadius(current, wanted int32) string {
	switch {
	case wanted == 0:
		return "This stops every replica: the workload will serve nothing."
	case wanted < current:
		return "This removes " + replicaCount(current-wanted) + "."
	case current == 0:
		return "This starts " + replicaCount(wanted) + "."
	default:
		return "This adds " + replicaCount(wanted-current) + "."
	}
}

func replicaCount(n int32) string {
	if n == 1 {
		return "1 replica"
	}
	return itoa(int(n)) + " replicas"
}

// parseReplicas reads a typed replica count, refusing what the API would refuse
// anyway — before it costs a round trip.
func parseReplicas(value string) (int32, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errEmptyReplicas
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, errNotANumber
	}
	if n < 0 {
		return 0, errNegativeReplicas
	}
	if n > resources.MaxReplicas {
		return 0, errTooManyReplicas
	}
	// Bounded by the two checks above, so the conversion cannot overflow.
	//nolint:gosec // G109: 0 <= n <= MaxReplicas
	return int32(n), nil
}

// replicasOf reports a workload's current replica count from the loaded
// snapshot.
func (m *Model) replicasOf(ref objectRef) (int32, bool) {
	if w, ok := m.workloadFor(ref); ok {
		return w.Desired, w.Replicated
	}
	return 0, false
}

func (m *Model) workloadFor(ref objectRef) (*application.Workload, bool) {
	workloads := m.apps.Get().Snapshot.Workloads
	for i := range workloads {
		w := &workloads[i]
		if w.Kind == ref.Kind && w.Name == ref.Name && w.Namespace == ref.Namespace {
			return w, true
		}
	}
	return nil, false
}

// resourceFor resolves an object's kind through the discovery catalog, which is
// what lets every action work on a custom resource with no code of its own.
func (m *Model) resourceFor(ref objectRef) (kubediscovery.Resource, bool) {
	catalog := m.catalog.Get()
	if catalog == nil {
		return kubediscovery.Resource{}, false
	}
	return catalog.Lookup(ref.lookup())
}

// The replica counts kubeui refuses before they cost a round trip.
var (
	errEmptyReplicas    = errors.New("type a replica count")
	errNotANumber       = errors.New("that is not a number")
	errNegativeReplicas = errors.New("a replica count cannot be negative")
	errTooManyReplicas  = errors.New("that is more replicas than anybody means to ask for")
)
