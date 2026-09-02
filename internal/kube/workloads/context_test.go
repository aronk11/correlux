package workloads

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func slice(service, suffix string, ready, notReady int) *discoveryv1.EndpointSlice {
	s := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service + "-" + suffix,
			Namespace: "payments",
			Labels:    map[string]string{discoveryv1.LabelServiceName: service},
		},
	}
	for i := 0; i < ready; i++ {
		s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.0." + itoa(i)},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)},
		})
	}
	for i := 0; i < notReady; i++ {
		s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.1." + itoa(i)},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr(false)},
		})
	}
	return s
}

func itoa(n int) string { return string(rune('0' + n%10)) }

func TestEndpointSlicesAreFoldedIntoOneCountPerService(t *testing.T) {
	cs := fake.NewSimpleClientset(
		slice("api", "abcde", 2, 0),
		slice("api", "fghij", 1, 1), // a second slice for the same service
	)

	got, err := CollectContext(context.Background(), cs, Options{Namespace: "payments"})
	if err != nil {
		t.Fatalf("CollectContext: %v", err)
	}
	set, ok := got.EndpointsFor("payments", "api")
	if !ok {
		t.Fatalf("no endpoints for api: %+v", got.Endpoints)
	}
	if set.Ready != 3 || set.NotReady != 1 {
		t.Errorf("ready/not ready = %d/%d, want 3/1", set.Ready, set.NotReady)
	}
}

func TestAnEndpointWithoutAReadyConditionCounts(t *testing.T) {
	// The API defines a nil Ready as ready; treating it as not ready would
	// report a working service as having no endpoints at all.
	cs := fake.NewSimpleClientset(&discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-xyz",
			Namespace: "payments",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.9"}}},
	})

	got, err := CollectContext(context.Background(), cs, Options{Namespace: "payments"})
	if err != nil {
		t.Fatalf("CollectContext: %v", err)
	}
	if set, _ := got.EndpointsFor("payments", "api"); set.Ready != 1 {
		t.Errorf("ready = %d, want 1", set.Ready)
	}
}

func TestNodeConditionsBecomeReadinessAndPressure(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse,
				Reason: "KubeletNotReady", Message: "container runtime is down"},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
			{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
		}},
	})

	got, err := CollectContext(context.Background(), cs, Options{})
	if err != nil {
		t.Fatalf("CollectContext: %v", err)
	}
	node, ok := got.Node("node-1")
	if !ok {
		t.Fatal("node-1 not collected")
	}
	if node.Ready {
		t.Error("a node whose Ready condition is false is not ready")
	}
	if node.Message != "container runtime is down" {
		t.Errorf("the node's own words must survive, got %q", node.Message)
	}
	if len(node.Pressure) != 1 || node.Pressure[0] != "MemoryPressure" {
		t.Errorf("pressure = %v, want [MemoryPressure]", node.Pressure)
	}
	if !node.Unschedulable {
		t.Error("a cordoned node must be reported as such")
	}
}

func TestEventsKeepTheirCountAndTime(t *testing.T) {
	last := time.Now().Add(-2 * time.Minute)
	cs := fake.NewSimpleClientset(&corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "api.17f", Namespace: "payments"},
		Type:           "Warning",
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: connection refused",
		Count:          7,
		LastTimestamp:  metav1.NewTime(last),
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-1", UID: "api-1-uid"},
	})

	got, err := CollectContext(context.Background(), cs, Options{Namespace: "payments"})
	if err != nil {
		t.Fatalf("CollectContext: %v", err)
	}
	events := got.EventsAbout("api-1-uid", "api-1")
	if len(events) != 1 {
		t.Fatalf("expected the event to be found by its object, got %+v", got.Events)
	}
	if events[0].Count != 7 {
		t.Errorf("count = %d, want 7", events[0].Count)
	}
	// The API stores event times at second precision.
	if drift := events[0].LastSeen.Sub(last); drift > time.Second || drift < -time.Second {
		t.Errorf("last seen = %v, want %v", events[0].LastSeen, last)
	}
}

func TestAnUnreadableKindIsAGapNotAFailure(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "nodes"}, "", errors.New("nope"))
	})

	got, err := CollectContext(context.Background(), cs, Options{Namespace: "payments"})
	if err != nil {
		t.Fatalf("one denied kind must not fail the pass: %v", err)
	}
	if len(got.Gaps) != 1 || got.Gaps[0].Kind != "Node" {
		t.Errorf("gaps = %+v, want the node listing", got.Gaps)
	}
}

func TestPodDetailIsCollected(t *testing.T) {
	dep := deployment("api", 1, 0)
	rs := replicaSetFor(dep)
	pod := podFor(rs, "api-7d8f-a", false, "CrashLoopBackOff", 12)
	pod.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137},
	}
	pod.Spec.Volumes = []corev1.Volume{{
		Name: "data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"},
		},
	}}
	cs := fake.NewSimpleClientset(dep, rs, pod)

	snap := collect(t, cs)
	if len(snap.Pods) != 1 {
		t.Fatalf("expected one pod, got %d", len(snap.Pods))
	}
	got := snap.Pods[0]
	if len(got.Containers) != 1 {
		t.Fatalf("container detail is what a diagnosis reasons about, got %+v", got.Containers)
	}
	c := got.Containers[0]
	if c.Reason != "CrashLoopBackOff" || c.LastReason != "OOMKilled" || c.LastExitCode != 137 {
		t.Errorf("container = %+v, want the crash loop and how the last run ended", c)
	}
	if !c.OOMKilled {
		t.Error("a container killed for its memory limit must be marked as such")
	}
	if len(got.Claims) != 1 || got.Claims[0] != "api-data" {
		t.Errorf("claims = %v, want [api-data]", got.Claims)
	}
}
