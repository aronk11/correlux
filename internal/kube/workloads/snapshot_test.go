package workloads

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/aronk11/kubeui/internal/domain/application"
)

func ptr[T any](v T) *T { return &v }

func deployment(name string, replicas, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "payments",
			UID:       uid(name),
			Labels:    map[string]string{"app.kubernetes.io/instance": name},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: replicas},
	}
}

// uid keeps the fixtures readable: a UID is only ever a unique string here.
func uid(name string) types.UID { return types.UID(name + "-uid") }

func replicaSetFor(dep *appsv1.Deployment) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep.Name + "-7d8f",
			Namespace: dep.Namespace,
			UID:       uid(dep.Name + "-rs"),
			Labels:    dep.Labels,
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "Deployment", Name: dep.Name, UID: dep.UID, Controller: ptr(true),
			}},
		},
	}
}

func podFor(rs *appsv1.ReplicaSet, name string, ready bool, waiting string, restarts int32) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rs.Namespace,
			UID:       uid(name),
			Labels:    rs.Labels,
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr(true),
			}},
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: conditionStatus(ready),
			}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "app", RestartCount: restarts, Ready: ready}},
		},
	}
	if waiting != "" {
		pod.Status.Phase = corev1.PodPending
		pod.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: waiting}
	}
	return pod
}

func conditionStatus(ready bool) corev1.ConditionStatus {
	if ready {
		return corev1.ConditionTrue
	}
	return corev1.ConditionFalse
}

func collect(t *testing.T, cs kubernetes.Interface) application.Snapshot {
	t.Helper()
	snap, err := Collect(context.Background(), cs, Options{Namespace: "payments"})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return snap
}

func TestCollectBuildsTheOwnershipChain(t *testing.T) {
	dep := deployment("api", 2, 1)
	rs := replicaSetFor(dep)
	cs := fake.NewSimpleClientset(
		dep, rs,
		podFor(rs, "api-7d8f-a", true, "", 0),
		podFor(rs, "api-7d8f-b", false, "CrashLoopBackOff", 4),
	)

	snap := collect(t, cs)
	apps := application.Group(snap)
	if len(apps) != 1 {
		t.Fatalf("expected one application, got %d", len(apps))
	}
	api := apps[0]
	if api.Health != application.Degraded {
		t.Errorf("one of two replicas ready is degraded, got %v", api.Health)
	}
	if got := api.ProblemSummary(); got != "1 CrashLoopBackOff" {
		t.Errorf("the container's waiting reason must reach the dashboard, got %q", got)
	}
	if api.Restarts != 4 {
		t.Errorf("restarts = %d, want 4", api.Restarts)
	}
	// The ReplicaSet connects the pods to the Deployment and is not a workload.
	if len(api.Workloads) != 1 || api.Workloads[0].Kind != "Deployment" {
		t.Errorf("a ReplicaSet must never appear as a workload, got %+v", api.Workloads)
	}
}

func TestCollectRecordsWhatItMayNotRead(t *testing.T) {
	dep := deployment("api", 1, 1)
	cs := fake.NewSimpleClientset(dep)
	cs.PrependReactor("list", "ingresses", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "ingresses"}, "", errors.New("nope"))
	})

	snap := collect(t, cs)
	if len(snap.Gaps) != 1 || snap.Gaps[0].Kind != "Ingress" {
		t.Fatalf("a denied kind must be recorded as a gap, got %+v", snap.Gaps)
	}
	if snap.Gaps[0].Reason != "not permitted for this user" {
		t.Errorf("the gap must say why, got %q", snap.Gaps[0].Reason)
	}
	if len(application.Group(snap)) != 1 {
		t.Error("one unreadable kind must not cost the whole application list")
	}
}

func TestCollectFailsOnlyWhenNothingIsReadable(t *testing.T) {
	cs := fake.NewSimpleClientset()
	for _, resource := range []string{
		"deployments", "statefulsets", "daemonsets", "replicasets",
		"jobs", "cronjobs", "pods", "services", "ingresses",
	} {
		cs.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("connection refused")
		})
	}

	if _, err := Collect(context.Background(), cs, Options{}); err == nil {
		t.Fatal("a cluster that answers nothing must be an error, not an empty dashboard")
	}
}

func TestCollectStopsPagingAndSaysSo(t *testing.T) {
	// Every page carries a continue token, so the server always claims more.
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "more"}}, nil
	})

	snap, err := Collect(context.Background(), cs, Options{PageSize: 1, MaxPages: 3})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !snap.Truncated {
		t.Error("a scope larger than the page budget must be reported as truncated")
	}
}

func TestIngressBackendsAndHostsAreCollected(t *testing.T) {
	cs := fake.NewSimpleClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "public", Namespace: "payments", UID: uid("ing")},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: "api.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{Name: "api"},
							},
						}},
					},
				},
			}},
		},
	})

	snap := collect(t, cs)
	if len(snap.Ingresses) != 1 {
		t.Fatalf("expected the ingress, got %+v", snap.Ingresses)
	}
	ing := snap.Ingresses[0]
	if len(ing.Hosts) != 1 || ing.Hosts[0] != "api.example.com" {
		t.Errorf("hosts = %v", ing.Hosts)
	}
	if len(ing.Backends) != 1 || ing.Backends[0] != "api" {
		t.Errorf("backends = %v", ing.Backends)
	}
}
