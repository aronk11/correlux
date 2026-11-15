package workloads

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aronk11/correlux/internal/domain/application"
)

func TestServicesOfTheSameNameInTwoNamespacesKeepTheirOwnEndpoints(t *testing.T) {
	yes, no := true, false
	slice := func(namespace string, ready *bool) *discoveryv1.EndpointSlice {
		return &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Labels: map[string]string{discoveryv1.LabelServiceName: "web"}},
			Endpoints:  []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ready}}},
		}
	}
	sets := map[string]*application.EndpointSet{}
	addSlice(sets, slice("a", &yes))
	addSlice(sets, slice("b", &no))

	if len(sets) != 2 {
		t.Fatalf("two services, got %d sets", len(sets))
	}
	if a, b := sets["a/web"], sets["b/web"]; a == nil || b == nil || a.Ready != 1 || b.Ready != 0 || b.NotReady != 1 {
		t.Errorf("a = %+v, b = %+v", a, b)
	}
}

func TestOnlyConditionsThatMeanTroubleCountAgainstANode(t *testing.T) {
	n := fromNode(&corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		{Type: "KernelReady", Status: corev1.ConditionTrue},
		{Type: "NetworkingReady", Status: corev1.ConditionTrue},
		{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue},
		{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
	}}})
	if len(n.Pressure) != 1 || n.Pressure[0] != "DiskPressure" {
		t.Errorf("pressure = %v, want only DiskPressure", n.Pressure)
	}
}

func TestAReconcilingFluxObjectHasNoVerdictYet(t *testing.T) {
	f := fluxItem{}
	f.Status.Conditions = []condition{{Type: "Ready", Status: "Unknown", Reason: "Progressing", Message: "Running 'upgrade' action"}}
	if got := f.toGitOps("HelmRelease"); got.ReadyKnown {
		t.Errorf("Ready=Unknown is work in progress, not a failure: %+v", got)
	}
	f.Status.Conditions[0].Status = "False"
	if got := f.toGitOps("HelmRelease"); !got.ReadyKnown || got.Ready {
		t.Errorf("Ready=False is a verdict: %+v", got)
	}
}

func TestRestartsOfInitContainersAreCounted(t *testing.T) {
	p := fromPod(&corev1.Pod{Status: corev1.PodStatus{
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "migrate", RestartCount: 40}},
		ContainerStatuses:     []corev1.ContainerStatus{{Name: "app", RestartCount: 2}},
	}})
	if p.Restarts != 42 {
		t.Errorf("restarts = %d, want 42", p.Restarts)
	}
}
