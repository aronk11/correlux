package application

import (
	"testing"
	"time"
)

func TestStaticControlPlanePodsAreNotGroupedUnderTheirNode(t *testing.T) {
	node := Meta{Kind: "Node", Name: "cp-1", UID: "Node/cp-1"}
	other := Meta{Kind: "Node", Name: "cp-2", UID: "Node/cp-2"}
	static := func(name, component string, owner Meta) Pod {
		m := ownedBy(meta("Pod", name, map[string]string{"component": component, "tier": "control-plane"}), owner)
		return Pod{Meta: m, Phase: "Running", Ready: true}
	}
	apps := Group(Snapshot{Pods: []Pod{
		static("kube-apiserver-cp-1", "kube-apiserver", node),
		static("kube-apiserver-cp-2", "kube-apiserver", other),
		static("etcd-cp-1", "etcd", node),
	}})

	if len(apps) != 2 {
		t.Fatalf("got %d applications, want the API server and etcd: %+v", len(apps), names(apps))
	}
	api := find(t, apps, "kube-apiserver")
	if len(api.Pods) != 2 {
		t.Errorf("the API servers of two nodes are one application, got %d pods", len(api.Pods))
	}
	if api.Traits.Role.Name != RoleControlPlane {
		t.Errorf("role = %q, want control plane", api.Traits.Role.Name)
	}
}

func TestAStaticPodWithoutAComponentLabelLosesItsNodeSuffix(t *testing.T) {
	node := Meta{Kind: "Node", Name: "worker-3", UID: "Node/worker-3"}
	apps := Group(Snapshot{Pods: []Pod{
		{Meta: ownedBy(meta("Pod", "haproxy-worker-3", nil), node), Phase: "Running", Ready: true},
	}})
	find(t, apps, "haproxy")
}

func TestPodsJoinTheirDeploymentWhenReplicaSetsCannotBeRead(t *testing.T) {
	dep := meta("Deployment", "api", nil)
	rs := meta("ReplicaSet", "api-7d8f", nil) // exists in the cluster, absent from the snapshot
	pod := ownedBy(meta("Pod", "api-7d8f-x2k9l", map[string]string{"pod-template-hash": "7d8f"}), rs)

	apps := Group(Snapshot{
		Workloads: []Workload{{Meta: dep, Desired: 1, Ready: 1, Replicated: true}},
		Pods:      []Pod{{Meta: pod, Phase: "Running", Ready: true}},
	})
	if len(apps) != 1 {
		t.Fatalf("got %v, want the pod under its Deployment", names(apps))
	}
	if got := len(find(t, apps, "api").Pods); got != 1 {
		t.Errorf("api has %d pods, want 1", got)
	}
}

func TestOperatorLabelsKeepRevisionsAndRunsTogether(t *testing.T) {
	knative := func(revision string) Workload {
		return Workload{Meta: meta("Deployment", "hello-"+revision+"-deployment", map[string]string{
			"app": "hello-" + revision, "serving.knative.dev/service": "hello",
		}), Desired: 1, Ready: 1, Replicated: true}
	}
	run := func(name string) Pod {
		owner := meta("TaskRun", name, nil)
		return Pod{Meta: ownedBy(meta("Pod", name+"-pod", map[string]string{"tekton.dev/pipeline": "build"}), owner),
			Phase: "Succeeded"}
	}
	apps := Group(Snapshot{
		Workloads: []Workload{knative("00001"), knative("00002")},
		Pods:      []Pod{run("build-run-1-fetch"), run("build-run-2-fetch")},
	})
	if len(apps) != 2 {
		t.Fatalf("got %v, want hello and build", names(apps))
	}
	hello := find(t, apps, "hello")
	if len(hello.Workloads) != 2 {
		t.Errorf("both revisions belong to hello, got %d", len(hello.Workloads))
	}
	if got := hello.Workloads[0].GroupedBy.Signal; got != SignalOperatorLabel {
		t.Errorf("signal = %v, want the operator label", got)
	}
	if got := len(find(t, apps, "build").Pods); got != 2 {
		t.Errorf("both runs belong to the pipeline, got %d pods", got)
	}
}

func TestAServiceJoinsAnApplicationThatHasNoPodsLeft(t *testing.T) {
	dep := meta("Deployment", "api", nil)
	apps := Group(Snapshot{
		Workloads: []Workload{{Meta: dep, Desired: 3, Ready: 0, Replicated: true,
			Selector: map[string]string{"app": "api", "tier": "web"}}},
		Services: []Service{
			{Meta: meta("Service", "api-svc", nil), Selector: map[string]string{"app": "api"}},
			{Meta: meta("Service", "other", nil), Selector: map[string]string{"app": "api", "track": "canary"}},
		},
	})
	api := find(t, apps, "api")
	if len(api.Services) != 1 || api.Services[0].Name != "api-svc" {
		t.Errorf("services = %+v, want only the one whose selector the workload's pods must match", api.Services)
	}
}

func TestTraitsAreReadFromLabelsAndNeverInvented(t *testing.T) {
	labels := map[string]string{
		"app.kubernetes.io/name":      "checkout",
		"app.kubernetes.io/part-of":   "shop",
		"app.kubernetes.io/component": "api",
		"app.kubernetes.io/version":   "2.4.1",
		"team":                        "payments",
		"env":                         "staging",
	}
	m := meta("Deployment", "checkout", labels)
	m.Annotations = map[string]string{"a8r.io/runbook": "https://runbooks.example/checkout"}
	apps := Group(Snapshot{Workloads: []Workload{
		{Meta: m, Desired: 1, Ready: 1, Replicated: true},
		{Meta: meta("Deployment", "plain", nil), Desired: 1, Ready: 1, Replicated: true},
	}})

	got := find(t, apps, "checkout").Traits
	if got.System != "shop" || got.Version != "2.4.1" || got.Team != "payments" || got.Environment != "staging" {
		t.Errorf("traits = %+v", got)
	}
	if len(got.Components) != 1 || got.Components[0] != "api" {
		t.Errorf("components = %v", got.Components)
	}
	if got.Runbook != "https://runbooks.example/checkout" {
		t.Errorf("runbook = %q", got.Runbook)
	}
	if got.Role.Known() {
		t.Errorf("checkout is nobody's platform, got role %+v", got.Role)
	}
	if plain := find(t, apps, "plain").Traits; plain.System != "" || plain.Team != "" || plain.Version != "" || plain.Role.Known() {
		t.Errorf("an unlabelled application has no traits, got %+v", plain)
	}
}

func TestTheVersionFallsBackToTheImageTheMainContainerRuns(t *testing.T) {
	pod := func(name, image string) Pod {
		return Pod{Meta: meta("Pod", name, map[string]string{"app": "api"}), Phase: "Running", Ready: true,
			Containers: []Container{
				{Name: "init", Init: true, Image: "busybox:1.36"},
				{Name: "api", Image: image},
				{Name: "proxy", Image: "envoyproxy/envoy:v1.31"},
			}}
	}
	apps := Group(Snapshot{Pods: []Pod{pod("a", "registry.example:5000/shop/api:1.8"), pod("b", "registry.example:5000/shop/api:1.9")}})
	if got := find(t, apps, "api").Traits.Version; got != "1.8, 1.9" {
		t.Errorf("version = %q, want both tags of a rollout in progress", got)
	}
}

func TestImageReferencesAreTakenApart(t *testing.T) {
	cases := []struct{ image, tag, repo string }{
		{"nginx", "latest", "nginx"},
		{"nginx:1.27", "1.27", "nginx"},
		{"registry.k8s.io/ingress-nginx/controller:v1.11.2@sha256:abcdef0123456789", "v1.11.2", "ingress-nginx/controller"},
		{"localhost:5000/api", "latest", "api"},
		{"quay.io/jetstack/cert-manager-controller@sha256:abcdef0123456789abcdef", "@abcdef012345", "jetstack/cert-manager-controller"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := ImageTag(c.image); got != c.tag {
			t.Errorf("ImageTag(%q) = %q, want %q", c.image, got, c.tag)
		}
		if got := ImageRepository(c.image); got != c.repo {
			t.Errorf("ImageRepository(%q) = %q, want %q", c.image, got, c.repo)
		}
	}
}

func TestRolesAreRecognisedByWholeNamesOnly(t *testing.T) {
	workload := func(name string, labels map[string]string) Snapshot {
		return Snapshot{Workloads: []Workload{{Meta: meta("Deployment", name, labels), Desired: 1, Ready: 1, Replicated: true}}}
	}
	cases := []struct {
		name string
		snap Snapshot
		app  string
		role string
	}{
		{"name label", workload("x", map[string]string{"app.kubernetes.io/name": "ingress-nginx"}), "ingress-nginx", RoleIngress},
		{"k8s-app", workload("coredns", map[string]string{"k8s-app": "kube-dns"}), "kube-dns", RoleDNS},
		{"family prefix", workload("argocd-repo-server", nil), "argocd-repo-server", RoleGitOps},
		{"csi driver", workload("ebs-csi-controller", nil), "ebs-csi-controller", RoleStorage},
		{"operator label", workload("pg", map[string]string{"cnpg.io/cluster": "orders-db"}), "orders-db", RoleDatabase},
		{"a substring is not a match", workload("kongregate-api", nil), "kongregate-api", ""},
		{"somebody's own app", workload("checkout", map[string]string{"app": "checkout"}), "checkout", ""},
	}
	for _, c := range cases {
		got := find(t, Group(c.snap), c.app).Traits.Role
		if got.Name != c.role {
			t.Errorf("%s: role = %q (by %q), want %q", c.name, got.Name, got.By, c.role)
		}
		if got.Known() && got.By == "" {
			t.Errorf("%s: a role must say what it was recognised from", c.name)
		}
	}
}

func TestARoleIsRecognisedFromTheImageWhenNothingElseSaysSo(t *testing.T) {
	apps := Group(Snapshot{Pods: []Pod{{
		Meta: meta("Pod", "edge-0", map[string]string{"app": "edge"}), Phase: "Running", Ready: true,
		Containers: []Container{{Name: "c", Image: "registry.k8s.io/ingress-nginx/controller:v1.11.2"}},
	}}})
	role := find(t, apps, "edge").Traits.Role
	if role.Name != RoleIngress || role.By != "image ingress-nginx/controller" {
		t.Errorf("role = %+v", role)
	}
	if role.Impact() == "" || !role.Platform() {
		t.Errorf("an ingress controller is platform and has an impact: %+v", role)
	}
	if (Role{Name: RoleDatabase}).Platform() {
		t.Error("a database is a backing service, not the platform")
	}
}

func TestEvictedPodsThatWereReplacedDoNotDegradeTheApplication(t *testing.T) {
	a := app(replicaSet(2, 2),
		pod(true, "Running", "", 0), pod(true, "Running", "", 0),
		pod(false, "Failed", "Evicted", 0), pod(false, "Failed", "Evicted", 0))
	if a.Health != Healthy {
		t.Errorf("health = %v (%s), want healthy: both replicas are ready", a.Health, a.Summary)
	}
	if a.Leftover != 2 {
		t.Errorf("leftover = %d, want the two corpses counted", a.Leftover)
	}

	// Without a controller to replace it, a failed pod is the outcome.
	bare := app(nil, pod(false, "Failed", "Error", 0))
	if bare.Health == Healthy {
		t.Error("a bare failed pod must not read as healthy")
	}
}

func TestOnlyTheNewestJobDecidesWhetherACronJobIsFailing(t *testing.T) {
	at := func(hours int) time.Time { return time.Date(2026, 9, 1, hours, 0, 0, 0, time.UTC) }
	job := func(name string, created time.Time, failed bool) Workload {
		w := Workload{Meta: Meta{Kind: "Job", Name: name, CreatedAt: created}, Failed: failed}
		if failed {
			w.FailedReason = "BackoffLimitExceeded"
		}
		return w
	}
	cron := Workload{Meta: Meta{Kind: "CronJob", Name: "backup"}}

	failing := app([]Workload{cron, job("backup-1", at(1), false), job("backup-2", at(2), true)})
	if failing.Health != Degraded || failing.Summary != "Job backup-2 failed: BackoffLimitExceeded" {
		t.Errorf("got %v %q, want degraded by the newest run", failing.Health, failing.Summary)
	}

	recovered := app([]Workload{cron, job("backup-1", at(1), true), job("backup-2", at(2), false)})
	if recovered.Health == Degraded {
		t.Errorf("an old failed run kept for reference is not the state of the job: %q", recovered.Summary)
	}
}

func TestNodePlacementReadsEveryCloudsSpelling(t *testing.T) {
	cases := []struct {
		labels map[string]string
		zone   string
		place  string
	}{
		{map[string]string{"topology.kubernetes.io/zone": "eu-central-1a", "eks.amazonaws.com/nodegroup": "general",
			"node.kubernetes.io/instance-type": "m6i.large", "eks.amazonaws.com/capacityType": "SPOT"},
			"eu-central-1a", "general · m6i.large · spot"},
		{map[string]string{"cloud.google.com/gke-nodepool": "default-pool", "cloud.google.com/gke-spot": "true"},
			"", "default-pool · spot"},
		{map[string]string{"node-role.kubernetes.io/control-plane": ""}, "", "control-plane"},
		{nil, "", ""},
	}
	for _, c := range cases {
		n := Node{Meta: Meta{Labels: c.labels}}
		if n.Zone() != c.zone || n.Placement() != c.place {
			t.Errorf("%v: zone %q placement %q, want %q %q", c.labels, n.Zone(), n.Placement(), c.zone, c.place)
		}
	}
}

func TestArgoCDIsRecognisedHoweverItTracks(t *testing.T) {
	cases := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		app, ns     string
	}{
		{"tracking id", nil, map[string]string{"argocd.argoproj.io/tracking-id": "shop:apps/Deployment:payments/api"}, "shop", ""},
		{"application in another namespace", nil, map[string]string{"argocd.argoproj.io/tracking-id": "team-a_shop:apps/Deployment:payments/api"}, "shop", "team-a"},
		{"instance label", map[string]string{"argocd.argoproj.io/instance": "shop"}, nil, "shop", ""},
		{"instance annotation", nil, map[string]string{"argocd.argoproj.io/instance": "shop"}, "shop", ""},
	}
	for _, c := range cases {
		m := meta("Deployment", "api", c.labels)
		m.Annotations = c.annotations
		got := managerOf(m)
		if got.Tool != "Argo CD" || got.Name != c.app || got.Namespace != c.ns {
			t.Errorf("%s: manager = %+v, want Argo CD %s in %q", c.name, got, c.app, c.ns)
		}
	}
}
