package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// revisionAnnotation is the Deployment controller's own revision stamp.
const revisionAnnotation = "deployment.kubernetes.io/revision"

// revisionListLimit bounds the ReplicaSets read for one Deployment. Kubernetes
// keeps ten by default; a Deployment with more than this has a history nobody
// rolls back through one at a time.
const revisionListLimit = 200

// ErrRollbackPaused is returned for a paused Deployment, which kubectl refuses
// too: the template would change and nothing would roll, which is a rollback
// in name only.
var ErrRollbackPaused = errors.New("the rollout is paused; resume it before rolling back")

// DeploymentRevision is one earlier pod template a Deployment can go back to.
type DeploymentRevision struct {
	Number      int64
	ReplicaSet  string
	CreatedAt   time.Time
	ChangeCause string
	Template    corev1.PodTemplateSpec
}

// RollbackPlan is what a rollback is decided from: the Deployment as it was
// read, its template now, and the revisions it can return to.
type RollbackPlan struct {
	Namespace string
	Name      string
	// ResourceVersion pins the rollback to the document it was decided on, so
	// a Deployment somebody changed meanwhile is refused rather than
	// overwritten.
	ResourceVersion string
	Paused          bool
	CurrentRevision int64
	Current         corev1.PodTemplateSpec
	// Revisions are the earlier ones, newest first. The revision serving now
	// is not among them.
	Revisions []DeploymentRevision
}

// Revision returns one of the plan's earlier revisions.
func (p *RollbackPlan) Revision(number int64) (DeploymentRevision, bool) {
	for i := range p.Revisions {
		if p.Revisions[i].Number == number {
			return p.Revisions[i], true
		}
	}
	return DeploymentRevision{}, false
}

// PlanRollback reads a Deployment and the ReplicaSets it controls.
func (f *Factory) PlanRollback(ctx context.Context, contextName, namespace, name string) (*RollbackPlan, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	return planRollback(ctx, cs, namespace, name)
}

func planRollback(ctx context.Context, cs kubernetes.Interface, namespace, name string) (*RollbackPlan, error) {
	dep, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("deployment %s has an invalid selector: %w", name, err)
	}
	list, err := cs.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
		Limit:         revisionListLimit,
	})
	if err != nil {
		return nil, err
	}

	plan := &RollbackPlan{
		Namespace:       namespace,
		Name:            name,
		ResourceVersion: dep.ResourceVersion,
		Paused:          dep.Spec.Paused,
		Current:         *dep.Spec.Template.DeepCopy(),
	}
	plan.CurrentRevision, _ = strconv.ParseInt(dep.Annotations[revisionAnnotation], 10, 64)

	for i := range list.Items {
		rs := &list.Items[i]
		if owner := metav1.GetControllerOf(rs); owner == nil || owner.UID != dep.UID {
			continue
		}
		number, err := strconv.ParseInt(rs.Annotations[revisionAnnotation], 10, 64)
		if err != nil || number == 0 || number == plan.CurrentRevision {
			continue
		}
		plan.Revisions = append(plan.Revisions, DeploymentRevision{
			Number:      number,
			ReplicaSet:  rs.Name,
			CreatedAt:   rs.CreationTimestamp.Time,
			ChangeCause: rs.Annotations["kubernetes.io/change-cause"],
			Template:    RollbackTemplate(&rs.Spec.Template),
		})
	}
	sort.Slice(plan.Revisions, func(i, j int) bool { return plan.Revisions[i].Number > plan.Revisions[j].Number })
	return plan, nil
}

// RollbackTemplate is a ReplicaSet's template as the Deployment should carry
// it: without the pod-template-hash label, which the controller adds to its
// ReplicaSets and which a Deployment's own template never has. Leaving it in
// is what makes a hand-rolled rollback create a new ReplicaSet instead of
// reusing the old one.
func RollbackTemplate(t *corev1.PodTemplateSpec) corev1.PodTemplateSpec {
	out := *t.DeepCopy()
	delete(out.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	return out
}

// RollbackDeployment returns a Deployment to one of its earlier revisions, the
// way `kubectl rollout undo` does: by writing that revision's template back,
// after which the controller finds the old ReplicaSet and scales it up.
func (f *Factory) RollbackDeployment(ctx context.Context, contextName string, plan *RollbackPlan, to int64) error {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return err
	}
	return rollbackDeployment(ctx, cs, plan, to)
}

func rollbackDeployment(ctx context.Context, cs kubernetes.Interface, plan *RollbackPlan, to int64) error {
	if plan.Paused {
		return ErrRollbackPaused
	}
	target, ok := plan.Revision(to)
	if !ok {
		return fmt.Errorf("deployment %s has no revision %d to roll back to", plan.Name, to)
	}
	patch, err := rollbackPatch(plan.ResourceVersion, &target.Template)
	if err != nil {
		return err
	}
	_, err = cs.AppsV1().Deployments(plan.Namespace).Patch(ctx, plan.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

// IsStale reports a change refused because the object moved on after it was
// read: a failed JSON-patch test (the API server quotes the patch library's
// own words) or an ordinary resourceVersion conflict.
func IsStale(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.IsConflict(err) || strings.Contains(err.Error(), "testing value /metadata/resourceVersion failed")
}

// rollbackPatch replaces the template, and only if the Deployment is still the
// version the plan was read from. A JSON patch test that fails is a 422 from
// the server, never a partial write.
func rollbackPatch(resourceVersion string, template *corev1.PodTemplateSpec) ([]byte, error) {
	ops := []map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": resourceVersion},
		{"op": "replace", "path": "/spec/template", "value": template},
	}
	return json.Marshal(ops)
}
