package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/diff"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/kube/workloads"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// rollbackChanges bounds how many changed fields the prompt and the
// confirmation name before the diff takes over.
const rollbackChanges = 4

// rollbackPlannedMsg carries the revisions a rollback can choose between.
type rollbackPlannedMsg struct {
	gen  uint64
	ref  objectRef
	plan *kubeclient.RollbackPlan
	err  error
}

// rolledBackMsg reports the outcome of a rollback.
type rolledBackMsg struct {
	ref objectRef
	to  int64
	err error
}

// rollbackTarget rolls back whatever the current screen points at.
//
// It is the counter-move to the most common cause of an incident — a rollout
// — and it is offered for Deployments only, because they are the kind whose
// history Kubernetes keeps as ReplicaSets with templates Correlux can show
// before it writes one back. StatefulSets and DaemonSets keep theirs as
// ControllerRevisions, and a rollback there is refused by name rather than
// attempted half-understood.
func (m *Model) rollbackTarget() tea.Cmd {
	ref, ok := m.targetRef()
	if !ok {
		m.notice("Select a Deployment to roll back", theme.StatusWarning)
		return m.expireNotice()
	}
	if !isDeployment(ref) {
		m.notice(rollbackRefusal(ref), theme.StatusWarning)
		return m.expireNotice()
	}

	m.rollbackGen++
	gen := m.rollbackGen
	factory := m.factory
	name := m.contextName
	m.notice("Reading the revisions of "+ref.label()+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		plan, err := factory.PlanRollback(ctx, name, ref.Namespace, ref.Name)
		return rollbackPlannedMsg{gen: gen, ref: ref, plan: plan, err: err}
	}
}

// rollbackableTarget reports the Deployment the current screen points at.
func (m *Model) rollbackableTarget() (objectRef, bool) {
	ref, ok := m.targetRef()
	if !ok || !isDeployment(ref) {
		return objectRef{}, false
	}
	return ref, true
}

func isDeployment(ref objectRef) bool {
	if ref.Kind != "Deployment" {
		return false
	}
	return ref.Resource == "" || ref.Resource == "deployments.apps"
}

func rollbackRefusal(ref objectRef) string {
	switch ref.Kind {
	case "StatefulSet", "DaemonSet":
		return ref.Kind + "s keep their history as ControllerRevisions, which Correlux does not roll back yet; use kubectl rollout undo"
	default:
		return ref.label() + " keeps no revisions to roll back to; only a Deployment does"
	}
}

// applyRollbackPlan turns the revisions into a question: which one?
func (m *Model) applyRollbackPlan(msg *rollbackPlannedMsg) tea.Cmd {
	if msg.gen != m.rollbackGen {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not read the revisions of "+msg.ref.label()+": "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	plan := msg.plan
	if plan.Paused {
		m.notice(msg.ref.label()+": "+kubeclient.ErrRollbackPaused.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	if len(plan.Revisions) == 0 {
		m.notice(msg.ref.label()+" has no earlier revision to roll back to", theme.StatusWarning)
		return m.expireNotice()
	}

	ref := msg.ref
	m.message = ""
	m.promptTitle = "Roll back " + ref.label()
	m.promptError = ""
	m.promptRef = ref
	m.promptInput.SetValue(strconv.FormatInt(plan.Revisions[0].Number, 10))
	m.promptAccept = func(m *Model, value string) tea.Cmd { return m.confirmRollback(ref, plan, value) }
	m.promptRefresh = func(m *Model) { m.promptNote = rollbackNote(plan, m.promptInput.Value(), time.Now()) }
	m.overlay = overlayPrompt
	m.refreshPrompt()
	return nil
}

// rollbackNote says, while the number is typed, what that revision would
// change back — the same sentence the confirmation will lead with.
func rollbackNote(plan *kubeclient.RollbackPlan, value string, now time.Time) string {
	numbers := make([]string, 0, len(plan.Revisions))
	for i := range plan.Revisions {
		numbers = append(numbers, strconv.FormatInt(plan.Revisions[i].Number, 10))
	}
	head := "Now at revision " + strconv.FormatInt(plan.CurrentRevision, 10) +
		". Kept: " + strings.Join(numbers, ", ") + "."

	target, err := pickRevision(plan, value)
	if err != nil {
		return head
	}
	changes := revisionChanges(plan, &target)
	line := "Revision " + strconv.FormatInt(target.Number, 10) + ", from " + formatAge(target.CreatedAt, now) + " ago"
	if target.ChangeCause != "" {
		line += " (" + target.ChangeCause + ")"
	}
	if len(changes) == 0 {
		return head + "\n" + line + ": the same template as now."
	}
	return head + "\n" + line + ": " + summariseChanges(changes)
}

// confirmRollback states what going back does, with the template diff under
// it, and passes through the one gate every change passes through (ADR 20).
func (m *Model) confirmRollback(ref objectRef, plan *kubeclient.RollbackPlan, value string) tea.Cmd {
	target, err := pickRevision(plan, value)
	if err != nil {
		m.promptError = err.Error()
		return nil
	}
	m.cancelPrompt()

	changes := revisionChanges(plan, &target)
	if len(changes) == 0 {
		m.notice(ref.label()+" already runs the template of revision "+strconv.FormatInt(target.Number, 10), theme.StatusUnknown)
		return m.expireNotice()
	}

	lines := []string{
		"This writes revision " + strconv.FormatInt(target.Number, 10) + "'s pod template back; every pod is replaced as the rollout strategy allows.",
	}
	if pods, known := m.ownedPodCount(ref); known {
		lines = append(lines, podCount(pods)+" are running now.")
	}
	lines = append(lines, ref.label()+" in "+orNone(ref.Namespace)+", revision "+
		strconv.FormatInt(plan.CurrentRevision, 10)+" → "+strconv.FormatInt(target.Number, 10))
	for i, c := range changes {
		if i == rollbackChanges {
			lines = append(lines, "  …"+itoa(len(changes)-rollbackChanges)+" more fields, in the diff below")
			break
		}
		lines = append(lines, "  "+c.String())
	}
	lines = append(lines, "A GitOps controller managing it may restore the current template from its source.")

	return m.confirm(pendingAction{
		Title:     "Roll back " + ref.label(),
		Lines:     lines,
		Diff:      templateDiff(&plan.Current, &target),
		Challenge: m.productionChallenge(),
		Run:       func(m *Model) tea.Cmd { return m.rollback(ref, plan, target.Number) },
	})
}

// rollback performs the change.
func (m *Model) rollback(ref objectRef, plan *kubeclient.RollbackPlan, to int64) tea.Cmd {
	factory := m.factory
	name := m.contextName
	m.notice("Rolling "+ref.label()+" back to revision "+strconv.FormatInt(to, 10)+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		err := factory.RollbackDeployment(ctx, name, plan, to)
		return rolledBackMsg{ref: ref, to: to, err: err}
	}
}

// applyRolledBack reports the outcome and reloads: a rollback is something to
// watch take effect.
func (m *Model) applyRolledBack(msg rolledBackMsg) tea.Cmd {
	if msg.err != nil {
		reason := shortError(msg.err)
		if kubeclient.IsStale(msg.err) {
			reason = "it changed after the revisions were read; nothing was written. Try again to see the new state"
		}
		m.notice("Could not roll back "+msg.ref.label()+": "+reason, theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Rolling "+msg.ref.label()+" back to revision "+strconv.FormatInt(msg.to, 10)+"; its pods are being replaced", theme.StatusHealthy)
	cmds := []tea.Cmd{m.loadApplications(), m.expireNotice()}
	if m.view == viewObject && m.objectTarget == msg.ref {
		cmds = append(cmds, m.loadObject())
	}
	return tea.Batch(cmds...)
}

// pickRevision reads a typed revision number against the plan.
func pickRevision(plan *kubeclient.RollbackPlan, value string) (kubeclient.DeploymentRevision, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return kubeclient.DeploymentRevision{}, errNoRevision
	}
	n, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return kubeclient.DeploymentRevision{}, errNotANumber
	}
	if n == plan.CurrentRevision {
		return kubeclient.DeploymentRevision{}, errCurrentRevision
	}
	r, ok := plan.Revision(n)
	if !ok {
		return kubeclient.DeploymentRevision{}, errUnknownRevision
	}
	return r, nil
}

// revisionChanges compares the template now with the one a rollback would
// write, field by field, through the same flattening WHY uses — so the change
// WHY blamed is the change the rollback names.
func revisionChanges(plan *kubeclient.RollbackPlan, target *kubeclient.DeploymentRevision) []application.Change {
	now := kubeclient.RollbackTemplate(&plan.Current)
	return application.TemplateChanges(workloads.FlattenTemplate(&now), workloads.FlattenTemplate(&target.Template))
}

func summariseChanges(changes []application.Change) string {
	parts := make([]string, 0, 2)
	for i, c := range changes {
		if i == 2 {
			parts = append(parts, itoa(len(changes)-2)+" more")
			break
		}
		parts = append(parts, c.String())
	}
	return strings.Join(parts, "; ")
}

// templateDiff is the line diff of the two templates, as YAML, which is the
// form somebody will check it in.
func templateDiff(current *corev1.PodTemplateSpec, target *kubeclient.DeploymentRevision) []diff.Line {
	before, errBefore := yaml.Marshal(kubeclient.RollbackTemplate(current))
	after, errAfter := yaml.Marshal(target.Template)
	if errBefore != nil || errAfter != nil {
		return nil
	}
	return diff.Hunks(diff.Lines(splitDocument(string(before)), splitDocument(string(after))), 1)
}

// The revision numbers Correlux refuses before they cost a round trip.
var (
	errNoRevision      = errors.New("type the revision to roll back to")
	errCurrentRevision = errors.New("that is the revision running now")
	errUnknownRevision = errors.New("the Deployment keeps no such revision")
)
