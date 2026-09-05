package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// deletedMsg reports the outcome of a delete.
type deletedMsg struct {
	ref objectRef
	err error
}

// deleteTarget deletes whatever the current screen is pointing at, after the
// same gate every other change goes through (ADR 20).
func (m *Model) deleteTarget() tea.Cmd {
	ref, ok := m.targetRef()
	if !ok {
		m.notice("Select an object to delete", theme.StatusWarning)
		return m.expireNotice()
	}
	if _, ok := m.resourceFor(ref); !ok {
		m.notice("This cluster does not serve "+ref.Kind, theme.StatusWarning)
		return m.expireNotice()
	}

	// Read while the object is still on screen: once the user has confirmed,
	// the screen may be somewhere else entirely.
	opts := m.deletePreconditions(ref)
	return m.confirm(pendingAction{
		Title: "Delete " + ref.label(),
		Lines: []string{
			m.deleteBlastRadius(ref),
			ref.label() + " in " + orNone(ref.Namespace),
			"Kubernetes deletes what it owns as well; this cannot be undone.",
		},
		Challenge: m.productionChallenge(),
		Danger:    true,
		Run:       func(m *Model) tea.Cmd { return m.deleteObject(ref, opts) },
	})
}

// deletableTarget reports the object the current screen points at, when this
// cluster serves its kind at all. Anything the cluster serves can be deleted:
// there is no kind for which the action is meaningless, only kinds a user is
// not permitted to remove, and that is the server's answer to give.
func (m *Model) deletableTarget() (objectRef, bool) {
	ref, ok := m.targetRef()
	if !ok {
		return objectRef{}, false
	}
	if _, ok := m.resourceFor(ref); !ok {
		return objectRef{}, false
	}
	return ref, true
}

// deleteBlastRadius says what goes, in the terms the user cares about: the
// object, and whatever Correlux can actually see going with it.
func (m *Model) deleteBlastRadius(ref objectRef) string {
	line := "This deletes " + ref.label() + "."
	if ref.Kind == "Namespace" {
		// The one count that follows from the kind rather than from a
		// snapshot: everything in a namespace is in the namespace.
		return line + " Everything in it goes too."
	}
	if pods, known := m.ownedPodCount(ref); known {
		return line + " Its " + podCount(pods) + " go with it."
	}
	return line
}

// deletePreconditions pins the delete to the object Correlux read.
//
// Without the uid a delete decided from what is on screen can land on a
// namesake recreated in between — the classic way a controller's replacement
// is destroyed by a confirmation somebody read a minute earlier. The
// resourceVersion is deliberately not sent: a Deployment's status changes on
// its own, and a delete refused because the ready count moved would be a
// safety measure nobody could satisfy.
func (m *Model) deletePreconditions(ref objectRef) resources.DeleteOptions {
	obj := m.object.Get()
	if obj == nil || m.objectTarget != ref || obj.UID == "" {
		return resources.DeleteOptions{}
	}
	return resources.DeleteOptions{UID: obj.UID}
}

// deleteObject performs the change.
func (m *Model) deleteObject(ref objectRef, opts resources.DeleteOptions) tea.Cmd {
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

	m.notice("Deleting "+ref.label()+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		err := factory.DeleteObject(ctx, name, res, namespace, ref.Name, opts)
		return deletedMsg{ref: ref, err: err}
	}
}

// applyDeleted reports the outcome and leaves the screen showing something
// that still exists.
func (m *Model) applyDeleted(msg deletedMsg) tea.Cmd {
	if msg.err != nil {
		text, status := deleteFailure(msg.ref, msg.err)
		m.notice(text, status)
		return m.expireNotice()
	}
	m.notice("Deleted "+msg.ref.label(), theme.StatusHealthy)

	var cmds []tea.Cmd
	if m.view == viewObject && m.objectTarget == msg.ref {
		// The document on screen describes an object the cluster no longer
		// has; back out the way the user came in.
		cmds = append(cmds, m.backFromObject())
	}
	cmds = append(cmds, m.loadApplications(), m.expireNotice())
	if m.view == viewTable {
		cmds = append(cmds, m.loadTable())
	}
	return tea.Batch(cmds...)
}

// deleteFailure classifies what came back. Two outcomes are worth naming
// rather than dumping: the object was already gone, which is what the user
// wanted anyway, and the user may not remove it, which no retry fixes.
func deleteFailure(ref objectRef, err error) (string, theme.Status) {
	switch {
	case apierrors.IsNotFound(err):
		return ref.label() + " is already gone", theme.StatusUnknown
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return "Not permitted to delete " + ref.label() + " in this cluster", theme.StatusWarning
	case apierrors.IsConflict(err):
		return ref.label() + " has been replaced since it was read; open it again and check what it is now",
			theme.StatusWarning
	}
	return "Could not delete " + ref.label() + ": " + shortError(err), theme.StatusCritical
}

func podCount(n int) string {
	if n == 1 {
		return "1 pod"
	}
	return itoa(n) + " pods"
}
