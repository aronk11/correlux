package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// cordonedMsg reports the outcome of a cordon or an uncordon.
type cordonedMsg struct {
	ref objectRef
	// unschedulable is what was asked for, so the outcome can be named without
	// reading the node back first.
	unschedulable bool
	err           error
}

// cordonTarget cordons the node the screen is pointing at, or uncordons it when
// it is already cordoned.
//
// One key does both, because taking a machine out of the scheduler's reach and
// putting it back are the same decision seen from either side, and a key that
// offers "cordon" on a node that is already cordoned offers nothing (SPEC 17).
func (m *Model) cordonTarget() tea.Cmd {
	ref, ok := m.cordonSubject()
	if !ok {
		m.notice("Select a node to cordon", theme.StatusWarning)
		return m.expireNotice()
	}
	res, known := m.resourceFor(ref)
	if !known {
		m.notice("This cluster does not serve "+ref.Kind, theme.StatusWarning)
		return m.expireNotice()
	}
	if !isNode(res) {
		m.notice("Only a node can be cordoned, and "+ref.label()+" is not one", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.askCordon(ref)
}

// cordonSubject is the object the current screen points at, whatever kind it
// turns out to be: the open object, or the row under the cursor in a resource
// table. Anywhere else there is nothing in hand to act on.
func (m *Model) cordonSubject() (objectRef, bool) {
	switch m.view {
	case viewObject:
		if m.objectTarget.empty() {
			return objectRef{}, false
		}
		return m.objectTarget, true
	case viewTable:
		return m.selectedRowRef()
	}
	return objectRef{}, false
}

// nodeTarget reports the node in hand, when what is in hand is one. It decides
// whether the key and the palette entry are offered at all.
func (m *Model) nodeTarget() (objectRef, bool) {
	ref, ok := m.cordonSubject()
	if !ok {
		return objectRef{}, false
	}
	res, known := m.resourceFor(ref)
	return ref, known && isNode(res)
}

// isNode answers from the discovery catalog rather than from the word "Node",
// so an operator's own kind that happens to be called Node is not patched as if
// it were a machine (ADR 13).
func isNode(res kubediscovery.Resource) bool {
	return res.Group() == "" && res.Plural() == "nodes"
}

// askCordon opens the gate. Nothing is sent to the cluster until somebody has
// agreed to what it says (ADR 20).
func (m *Model) askCordon(ref objectRef) tea.Cmd {
	cordoned, known := m.cordonState(ref)
	if cordoned {
		return m.confirm(pendingAction{
			Title: "Uncordon " + ref.label(),
			Lines: []string{
				"This lets the scheduler place pods on " + ref.Name + " again.",
				ref.label(),
			},
			Challenge: m.productionChallenge(),
			Run:       func(m *Model) tea.Cmd { return m.cordon(ref, false) },
		})
	}

	lines := []string{
		"This stops new pods from being scheduled onto " + ref.Name + ".",
		podsStayLine(m.podsOnNode(ref.Name)),
		ref.label(),
	}
	if !known {
		// A state Correlux has not read is never presented as one it has. A
		// node that is cordoned already would take this change for no effect,
		// and that is worth knowing before agreeing to it.
		lines = append(lines, "Whether "+ref.Name+" is cordoned already has not been read.")
	}
	return m.confirm(pendingAction{
		Title:     "Cordon " + ref.label(),
		Lines:     lines,
		Challenge: m.productionChallenge(),
		// Not marked dangerous: nothing is evicted and nothing is deleted, and
		// one keystroke puts the node back. It still goes through the gate,
		// production challenge included.
		Danger: false,
		Run:    func(m *Model) tea.Cmd { return m.cordon(ref, true) },
	})
}

// cordonState reports whether a node is unschedulable right now, and whether
// Correlux has read that at all.
func (m *Model) cordonState(ref objectRef) (cordoned, known bool) {
	// The open document first: it is the server's own answer about this node,
	// rather than a rollup taken for another screen.
	if m.view == viewObject && m.objectTarget == ref && m.object.State() == async.Ready {
		if obj := m.object.Get(); obj != nil && obj.Name == ref.Name {
			if unschedulable, ok := resources.Unschedulable(obj.Raw); ok {
				return unschedulable, true
			}
		}
	}
	for _, node := range m.usageLive.Nodes {
		if node.Name == ref.Name {
			return node.Unschedulable, true
		}
	}
	if m.evidence.State() == async.Ready {
		if node, ok := m.evidence.Get().Node(ref.Name); ok {
			return node.Unschedulable, true
		}
	}
	return false, false
}

// podsOnNode counts the pods the loaded snapshot puts on a node, and reports
// whether that count is the whole truth.
//
// A snapshot scoped to one namespace sees only that namespace's pods, and a
// number that is quietly a subset is worse in a confirmation than no number at
// all.
func (m *Model) podsOnNode(name string) (int, bool) {
	if m.apps.State() != async.Ready || !m.allNamespaces {
		return 0, false
	}
	snapshot := m.apps.Get().Snapshot
	if snapshot.Truncated {
		return 0, false
	}
	count := 0
	for i := range snapshot.Pods {
		if snapshot.Pods[i].Node == name {
			count++
		}
	}
	return count, true
}

// podsStayLine says what happens to the pods that are there already, which is
// the question cordon is most often confused about: it is not a drain.
func podsStayLine(count int, known bool) string {
	switch {
	case !known:
		return "Pods already running there are not touched."
	case count == 0:
		return "No pods in scope are running there."
	case count == 1:
		return "The pod already running there keeps running."
	default:
		return "The " + itoa(count) + " pods already running there keep running."
	}
}

// cordon performs the change.
func (m *Model) cordon(ref objectRef, unschedulable bool) tea.Cmd {
	res, ok := m.resourceFor(ref)
	if !ok {
		return nil
	}
	factory := m.factory
	name := m.contextName

	m.notice(cordonProgress(unschedulable)+" "+ref.Name+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		err := factory.CordonNode(ctx, name, res, ref.Name, unschedulable)
		return cordonedMsg{ref: ref, unschedulable: unschedulable, err: err}
	}
}

// applyCordoned reports the outcome and reloads what is on screen, so the new
// state appears where it is counted rather than at the next refresh.
func (m *Model) applyCordoned(msg cordonedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Could not "+cordonVerb(msg.unschedulable)+" "+msg.ref.Name+": "+cordonError(msg.err),
			theme.StatusCritical)
		return m.expireNotice()
	}
	if msg.unschedulable {
		// A cordoned node is a machine deliberately out of service, not a
		// healthy one, and the fleet and usage views already say so.
		m.notice("Cordoned "+msg.ref.Name+"; it takes no new pods", theme.StatusUnknown)
	} else {
		m.notice("Uncordoned "+msg.ref.Name+"; the scheduler can place pods there again", theme.StatusHealthy)
	}

	cmds := []tea.Cmd{m.expireNotice()}
	switch {
	case m.view == viewObject && m.objectTarget == msg.ref:
		cmds = append(cmds, m.loadObject())
	case m.view == viewTable:
		cmds = append(cmds, m.loadTable())
	case m.view == viewUsage:
		cmds = append(cmds, m.loadUsage())
	}
	if m.evidence.State() == async.Ready {
		// Whatever the current screen is, what Correlux holds about this node
		// is now known to be out of date, and the cordoned counts are read
		// from it.
		cmds = append(cmds, m.loadEvidence())
	}
	return tea.Batch(cmds...)
}

// cordonError says what a refusal means. A node is the object most often
// reached for with a namespace-scoped token, and "nodes \"node-1\" is
// forbidden: User cannot patch resource" is a sentence about client-go rather
// than about what to do next.
func cordonError(err error) string {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return "you are not permitted to change nodes in this cluster"
	}
	return shortError(err)
}

// cordonTitle names what the action will do to the node in hand right now.
func (m *Model) cordonTitle(ref objectRef) string {
	if cordoned, _ := m.cordonState(ref); cordoned {
		return "Uncordon " + ref.Name
	}
	return "Cordon " + ref.Name
}

// cordonSubtitle says what the node is doing now, so the palette entry carries
// the state the user is about to change.
func (m *Model) cordonSubtitle(ref objectRef) string {
	cordoned, known := m.cordonState(ref)
	switch {
	case !known:
		return "its current state has not been read"
	case cordoned:
		return "it takes no new pods; this puts it back in service"
	default:
		return "it stops taking new pods; the ones running there keep running"
	}
}

// cordonHint is the word for the footer, which has room for one.
func (m *Model) cordonHint(ref objectRef) string {
	if cordoned, _ := m.cordonState(ref); cordoned {
		return "Uncordon"
	}
	return "Cordon"
}

// cordonVerb names the action in a sentence that reports a failure to do it.
func cordonVerb(unschedulable bool) string {
	if unschedulable {
		return "cordon"
	}
	return "uncordon"
}

// cordonProgress names the action while it is in flight.
func cordonProgress(unschedulable bool) string {
	if unschedulable {
		return "Cordoning"
	}
	return "Uncordoning"
}
