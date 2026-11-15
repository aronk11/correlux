package workloads

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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

func TestAPodsEnvironmentAndSpreadAreRead(t *testing.T) {
	p := fromPod(&corev1.Pod{Spec: corev1.PodSpec{
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "topology.kubernetes.io/zone"}},
		Containers: []corev1.Container{{Name: "app", Env: []corev1.EnvVar{
			{Name: "DATABASE_URL", Value: "postgres://app:s3cr3t@postgres:5432/shop"},
			{Name: "API_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "token"}}},
			{Name: "MODE", Value: "api"},
		}}},
	}})
	if !p.Spread {
		t.Error("a topology spread constraint is a request to spread")
	}
	if len(p.References) != 1 || p.References[0].Host != "postgres" || p.References[0].Env != "DATABASE_URL" {
		t.Errorf("references = %+v, want the one host and never the password", p.References)
	}
}

func TestAnArgoCDApplicationIsReducedToItsOwnVerdict(t *testing.T) {
	var a argoItem
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"shop","namespace":"argocd"},
		"spec":{"syncPolicy":{"automated":{"prune":true}}},
		"status":{"sync":{"status":"OutOfSync","revision":"abc"},"health":{"status":"Degraded"},
		"operationState":{"phase":"Failed","message":"boom"},
		"conditions":[{"type":"SyncError","message":"x"},{"type":"OrphanedResourceWarning","message":"y"}]}}`), &a); err != nil {
		t.Fatal(err)
	}
	g := a.toGitOps()
	if g.Kind != "Application" || g.Sync != "OutOfSync" || g.Health != "Degraded" || g.Operation != "Failed" || !g.Automated {
		t.Errorf("got %+v", g)
	}
	if len(g.Errors) != 1 || g.Errors[0] != "SyncError: x" {
		t.Errorf("only the *Error conditions are complaints: %v", g.Errors)
	}
}

func TestAGatewayRouteIsReadAsWhereTrafficComesIn(t *testing.T) {
	var r routeItem
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"shop","namespace":"payments"},
		"spec":{"hostnames":["shop.example.com"],"parentRefs":[{"name":"public"}],
		"rules":[{"backendRefs":[{"name":"checkout","port":80},{"name":"elsewhere","namespace":"other"},{"kind":"ServiceImport","name":"x"}]},
		         {"backendRefs":[{"name":"checkout"}]}]},
		"status":{"parents":[{"parentRef":{"name":"public"},"conditions":[
			{"type":"Accepted","status":"False","reason":"NotAllowedByListeners","message":"no listener allows routes from namespace payments"},
			{"type":"ResolvedRefs","status":"True"}]}]}}`), &r); err != nil {
		t.Fatal(err)
	}
	got := r.toIngress("HTTPRoute")
	if got.Kind != "HTTPRoute" || len(got.Hosts) != 1 || len(got.Gateways) != 1 {
		t.Errorf("got %+v", got)
	}
	if len(got.Backends) != 1 || got.Backends[0] != "checkout" {
		t.Errorf("backends = %v, want the one Service next door, once", got.Backends)
	}
	if len(got.Rejected) != 1 || got.Rejected[0] != "Gateway/public: Accepted=False (NotAllowedByListeners) — no listener allows routes from namespace payments" {
		t.Errorf("rejected = %v", got.Rejected)
	}
}

func TestAnArgoRolloutIsAWorkloadWithAReplicaCount(t *testing.T) {
	var r rolloutItem
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"checkout","namespace":"shop","uid":"ro-1"},
		"spec":{"replicas":5,"selector":{"matchLabels":{"app":"checkout"}}},
		"status":{"readyReplicas":2,"updatedReplicas":1,"phase":"Degraded","message":"RolloutAborted: metric \"success-rate\" assessed Failed"}}`), &r); err != nil {
		t.Fatal(err)
	}
	w := r.toWorkload()
	if w.Kind != "Rollout" || w.Desired != 5 || w.Ready != 2 || !w.Replicated || w.Selector["app"] != "checkout" {
		t.Errorf("got %+v", w)
	}
	if !w.Stalled || w.StalledMessage == "" {
		t.Errorf("a Degraded rollout is the controller giving up, in its words: %+v", w)
	}

	r.Status.Phase, r.Status.Message = "Paused", "CanaryPauseStep"
	if w := r.toWorkload(); !w.Paused || w.Stalled {
		t.Errorf("a canary waiting to be promoted is paused, not broken: %+v", w)
	}

	// The pods of a Rollout hang off its ReplicaSet, and the walk up the
	// owners must end at the Rollout like it ends at a Deployment.
	rs := application.Meta{Kind: "ReplicaSet", Name: "checkout-abc", Namespace: "shop", UID: "rs-1",
		Owners: []application.OwnerRef{{Kind: "Rollout", Name: "checkout", UID: "ro-1", Controller: true}}}
	pod := application.Pod{Meta: application.Meta{Kind: "Pod", Name: "checkout-abc-1", Namespace: "shop", UID: "p-1",
		Owners: []application.OwnerRef{{Kind: "ReplicaSet", Name: "checkout-abc", UID: "rs-1", Controller: true}}},
		Phase: "Running", Ready: true}
	apps := application.Group(application.Snapshot{
		Workloads: []application.Workload{r.toWorkload()}, Owners: []application.Meta{rs}, Pods: []application.Pod{pod}})
	if len(apps) != 1 || apps[0].DesiredPods != 5 || len(apps[0].Pods) != 1 {
		t.Errorf("apps = %+v", apps)
	}
}

func TestConfigMapsAreReducedToTheHostsTheyName(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "shop-config", Namespace: "shop"},
		Data: map[string]string{
			"DATABASE_URL": "postgres://app:s3cr3t@postgres:5432/shop",
			"LOG_LEVEL":    "debug",
		},
	})
	p := fromPod(&corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "shop-config"}}}},
		Env:     []corev1.EnvVar{{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "t"}}}},
	}}}})
	if len(p.ConfigSources) != 1 || p.ConfigSources[0].ConfigMap != "shop-config" {
		t.Fatalf("config sources = %+v, want the ConfigMap and never the Secret", p.ConfigSources)
	}

	evidence, err := CollectContext(context.Background(), cs, Options{Namespace: "shop",
		ConfigMaps: []string{"shop/shop-config", "shop/gone"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.ConfigReferences) != 1 {
		t.Fatalf("references = %+v", evidence.ConfigReferences)
	}
	if r := evidence.ConfigReferences[0]; r.Host != "postgres" || r.Key != "DATABASE_URL" {
		t.Errorf("reference = %+v, want the key and the host, nothing of the value", r)
	}
}
