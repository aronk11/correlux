package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// restartedMsg reports the outcome of a rolling restart.
type restartedMsg struct {
	ref objectRef
	err error
}

// restartProbedMsg carries the document a restart has to see before it can be
// offered: whether the object has a pod template at all.
type restartProbedMsg struct {
	gen    uint64
	ref    objectRef
	object *resources.Object
	err    error
}

// restartTarget rolls whatever the current screen is pointing at.
//
// Whether a rolling restart means anything is a property of the object rather
// than of its kind, so when Correlux has not read the document it reads it
// first and refuses with a reason, instead of sending a patch that would
// silently annotate something that has no pods.
func (m *Model) restartTarget() tea.Cmd {
	ref, ok := m.targetRef()
	if !ok {
		m.notice("Select a workload to restart", theme.StatusWarning)
		return m.expireNotice()
	}
	res, ok := m.resourceFor(ref)
	if !ok {
		m.notice("This cluster does not serve "+ref.Kind, theme.StatusWarning)
		return m.expireNotice()
	}
	if obj := m.loadedObject(ref); obj != nil {
		return m.confirmRestart(ref, obj)
	}
	return m.readForRestart(ref, res)
}

// loadedObject returns the document Correlux holds for a reference, when it is
// this object's and it has arrived.
func (m *Model) loadedObject(ref objectRef) *resources.Object {
	if m.view != viewObject || m.objectTarget != ref {
		return nil
	}
	return m.object.Get()
}

// readForRestart fetches the document behind a row before offering to roll it.
func (m *Model) readForRestart(ref objectRef, res kubediscovery.Resource) tea.Cmd {
	// A newer request retires this one: a document that arrives for an object
	// nobody is pointing at any more must not open a confirmation.
	m.restartGen++
	gen := m.restartGen

	factory := m.factory
	name := m.contextName
	namespace := ref.Namespace
	if !res.Namespaced {
		namespace = ""
	}

	m.notice("Reading "+ref.label()+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		obj, err := factory.GetObject(ctx, name, res, namespace, ref.Name)
		return restartProbedMsg{gen: gen, ref: ref, object: obj, err: err}
	}
}

// applyRestartProbe turns the document into a confirmation, or into the reason
// there is nothing to confirm.
func (m *Model) applyRestartProbe(msg restartProbedMsg) tea.Cmd {
	if msg.gen != m.restartGen {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not read "+msg.ref.label()+": "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	return m.confirmRestart(msg.ref, msg.object)
}

// confirmRestart states what a rollout does to what is running now.
func (m *Model) confirmRestart(ref objectRef, obj *resources.Object) tea.Cmd {
	if !obj.HasPodTemplate() {
		m.notice(ref.label()+" has no pod template; there is nothing to roll", theme.StatusWarning)
		return m.expireNotice()
	}

	lines := []string{"This replaces every pod of " + ref.Name + ", as fast as its rollout allows."}
	if pods, known := m.ownedPodCount(ref); known {
		lines = append(lines, podCount(pods)+" are running now.")
	}
	lines = append(lines, ref.label()+" in "+orNone(ref.Namespace))

	return m.confirm(pendingAction{
		Title: "Restart " + ref.label(),
		Lines: lines,
		// Disruptive rather than destructive: the pods come back. The
		// production challenge still applies, because rolling the wrong
		// cluster's workload is the same mistake as scaling it.
		Challenge: m.productionChallenge(),
		Run:       func(m *Model) tea.Cmd { return m.restart(ref) },
	})
}

// restart performs the change.
func (m *Model) restart(ref objectRef) tea.Cmd {
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
	at := time.Now()

	m.notice("Restarting "+ref.label()+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		err := factory.RestartWorkload(ctx, name, res, namespace, ref.Name, at)
		return restartedMsg{ref: ref, err: err}
	}
}

// applyRestarted reports the outcome and refreshes what is on screen: a
// rollout is something to watch happen.
func (m *Model) applyRestarted(msg restartedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Could not restart "+msg.ref.label()+": "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Restarting "+msg.ref.label()+"; its pods are being replaced", theme.StatusHealthy)

	cmds := []tea.Cmd{m.loadApplications(), m.expireNotice()}
	if m.view == viewObject && m.objectTarget == msg.ref {
		cmds = append(cmds, m.loadObject())
	}
	return tea.Batch(cmds...)
}

// restartableTarget reports the object the current screen points at, unless
// the document Correlux has already read says it carries no pod template.
//
// Not having read it is not an answer: the command stays offered and the
// refusal arrives with the document, which is honest about what is known and
// keeps the entry off objects that are known not to roll (SPEC 17).
func (m *Model) restartableTarget() (objectRef, bool) {
	ref, ok := m.targetRef()
	if !ok {
		return objectRef{}, false
	}
	if _, ok := m.resourceFor(ref); !ok {
		return objectRef{}, false
	}
	if obj := m.loadedObject(ref); obj != nil && !obj.HasPodTemplate() {
		return objectRef{}, false
	}
	return ref, true
}

// knownRestartable reports that Correlux knows, right now, that the selection
// rolls: the document it holds carries a pod template, or the dashboard
// already lists the object as a workload.
//
// It is what the key hints advertise. The palette is less strict on purpose —
// it offers the command where the answer is unknown and refuses with the
// document — but a hint in the status bar is a promise about this keystroke.
func (m *Model) knownRestartable() bool {
	ref, ok := m.restartableTarget()
	if !ok {
		return false
	}
	if obj := m.loadedObject(ref); obj != nil {
		return obj.HasPodTemplate()
	}
	_, isWorkload := m.workloadFor(ref)
	return isWorkload
}
