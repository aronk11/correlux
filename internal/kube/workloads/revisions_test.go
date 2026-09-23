package workloads

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/aronk11/correlux/internal/domain/application"
)

func revisionOf(owner types.UID, name, revision, image string, replicas int32) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "shop", UID: types.UID(name),
			Annotations: map[string]string{revisionAnnotation: revision},
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "Deployment", Name: "api", UID: owner, Controller: ptr(true),
			}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: ptr(replicas),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api", templateHashLabel: name}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "api", Image: image,
					Env: []corev1.EnvVar{{Name: "DB_PASSWORD", Value: "hunter2"}},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
					},
				}}},
			},
		},
	}
}

func TestTheEvidenceCarriesEachDeploymentsRevisions(t *testing.T) {
	cs := fake.NewSimpleClientset(
		revisionOf("dep-1", "api-13", "13", "registry/api:1.8", 0),
		revisionOf("dep-1", "api-14", "14", "registry/api:1.9", 3),
		// A ReplicaSet nobody controls is not a revision of anything.
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "loose", Namespace: "shop"}},
	)
	got, err := CollectContext(context.Background(), cs, Options{Namespace: "shop"})
	if err != nil {
		t.Fatalf("CollectContext: %v", err)
	}
	revs := got.RevisionsOf("dep-1")
	if len(revs) != 2 || revs[0].Number != 14 || revs[1].Number != 13 {
		t.Fatalf("revisions newest first, got %+v", revs)
	}
	if len(got.Revisions) != 2 {
		t.Errorf("an uncontrolled ReplicaSet must not be a revision, got %d", len(got.Revisions))
	}

	changes := application.TemplateChanges(revs[1].Template, revs[0].Template)
	if len(changes) != 1 || changes[0].String() != "container api image: registry/api:1.8 → registry/api:1.9" {
		t.Errorf("the only change is the image, and the template hash is not one, got %v", changes)
	}
}

func TestTemplatesFlattenWhatChangesBehaviour(t *testing.T) {
	rs := revisionOf("dep-1", "api-14", "14", "registry/api:1.9", 3)
	flat := FlattenTemplate(&rs.Spec.Template)
	for key, want := range map[string]string{
		"container api image":           "registry/api:1.9",
		"container api limits memory":   "256Mi",
		"container api env DB_PASSWORD": "hunter2",
		"label app":                     "api",
	} {
		if flat[key] != want {
			t.Errorf("%s = %q, want %q", key, flat[key], want)
		}
	}
	for key := range flat {
		if strings.Contains(key, templateHashLabel) {
			t.Errorf("the template hash differs on every revision and must not be compared: %s", key)
		}
	}
}

func TestOldRevisionsArePrunedButServingOnesAreKept(t *testing.T) {
	var in []application.Revision
	for n := int64(1); n <= 8; n++ {
		desired := int32(0)
		if n == 2 {
			desired = 1 // an old revision that is still serving
		}
		in = append(in, application.Revision{
			Meta:    application.Meta{Owners: []application.OwnerRef{{Kind: "Deployment", UID: "d", Controller: true}}},
			Number:  n,
			Desired: desired,
		})
	}
	out := pruneRevisions(in)
	var numbers []int64
	for _, r := range out {
		numbers = append(numbers, r.Number)
	}
	if len(numbers) != 4 || numbers[0] != 8 || numbers[1] != 7 || numbers[2] != 6 || numbers[3] != 2 {
		t.Errorf("kept %v, want the newest three and the one still serving", numbers)
	}
}

func TestAStalledRolloutIsTheControllersVerdict(t *testing.T) {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "shop", Generation: 4},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 4,
			Conditions: []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
				Reason: "ProgressDeadlineExceeded", Message: `ReplicaSet "api-14" has timed out progressing.`,
			}},
		},
	}
	w := fromDeployment(d)
	if !w.Stalled || w.StalledReason != "ProgressDeadlineExceeded" {
		t.Errorf("stalled = %v %q", w.Stalled, w.StalledReason)
	}

	// A verdict about an earlier generation is not about this template.
	d.Generation = 5
	if fromDeployment(d).Stalled {
		t.Error("a condition the controller has not re-evaluated must not be quoted")
	}
}
