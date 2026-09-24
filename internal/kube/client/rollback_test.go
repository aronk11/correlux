package client

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func ptrBool(b bool) *bool { return &b }

func template(image, hash string) corev1.PodTemplateSpec {
	labels := map[string]string{"app": "api"}
	if hash != "" {
		labels[appsv1.DefaultDeploymentUniqueLabelKey] = hash
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: image}}},
	}
}

func replicaSet(name, revision, image string, owner types.UID) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "shop",
			Labels:      map[string]string{"app": "api"},
			Annotations: map[string]string{revisionAnnotation: revision},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: owner, Controller: ptrBool(true),
			}},
		},
		Spec: appsv1.ReplicaSetSpec{Template: template(image, name)},
	}
}

func rollbackCluster() *fake.Clientset {
	return fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api", Namespace: "shop", UID: "dep-uid", ResourceVersion: "7",
				Annotations: map[string]string{revisionAnnotation: "14"},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: template("api:1.9", ""),
			},
		},
		replicaSet("api-12", "12", "api:1.7", "dep-uid"),
		replicaSet("api-13", "13", "api:1.8", "dep-uid"),
		replicaSet("api-14", "14", "api:1.9", "dep-uid"),
		// Same labels, another owner: not this Deployment's history.
		replicaSet("other-9", "9", "other:1", "someone-else"),
	)
}

func TestARollbackPlanListsTheEarlierRevisionsNewestFirst(t *testing.T) {
	plan, err := planRollback(context.Background(), rollbackCluster(), "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentRevision != 14 || plan.ResourceVersion != "7" {
		t.Errorf("current = %d at %q", plan.CurrentRevision, plan.ResourceVersion)
	}
	if len(plan.Revisions) != 2 || plan.Revisions[0].Number != 13 || plan.Revisions[1].Number != 12 {
		t.Fatalf("revisions = %+v", plan.Revisions)
	}
	if _, ok := plan.Revisions[0].Template.Labels[appsv1.DefaultDeploymentUniqueLabelKey]; ok {
		t.Error("the pod-template-hash label is the ReplicaSet's, never the Deployment's")
	}
}

func TestARollbackWritesTheEarlierTemplateBack(t *testing.T) {
	cs := rollbackCluster()
	ctx := context.Background()
	plan, err := planRollback(ctx, cs, "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackDeployment(ctx, cs, plan, 13); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	dep, _ := cs.AppsV1().Deployments("shop").Get(ctx, "api", metav1.GetOptions{})
	if got := dep.Spec.Template.Spec.Containers[0].Image; got != "api:1.8" {
		t.Errorf("image after rollback = %q, want api:1.8", got)
	}
}

func TestARollbackDecidedOnAnOldVersionIsRefused(t *testing.T) {
	cs := rollbackCluster()
	ctx := context.Background()
	plan, err := planRollback(ctx, cs, "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	plan.ResourceVersion = "6" // somebody changed it since
	err = rollbackDeployment(ctx, cs, plan, 13)
	if err == nil {
		t.Fatal("a rollback against a changed Deployment must be refused")
	}
	if !IsStale(err) {
		t.Errorf("the refusal must be recognisable as a stale read, got %v", err)
	}
	dep, _ := cs.AppsV1().Deployments("shop").Get(ctx, "api", metav1.GetOptions{})
	if got := dep.Spec.Template.Spec.Containers[0].Image; got != "api:1.9" {
		t.Errorf("a refused rollback must change nothing, image = %q", got)
	}
}

func TestAPausedDeploymentIsNotRolledBack(t *testing.T) {
	plan := &RollbackPlan{Paused: true, Revisions: []DeploymentRevision{{Number: 13}}}
	if err := rollbackDeployment(context.Background(), fake.NewSimpleClientset(), plan, 13); !errors.Is(err, ErrRollbackPaused) {
		t.Errorf("err = %v, want ErrRollbackPaused", err)
	}
}
