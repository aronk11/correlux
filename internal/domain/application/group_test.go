package application

import (
	"testing"
	"time"
)

// The builders below keep a test case readable as the cluster it describes:
// what exists, what owns what, and what state it is in.

func meta(kind, name string, labels map[string]string) Meta {
	return Meta{
		Kind:      kind,
		Name:      name,
		Namespace: "payments",
		UID:       kind + "/" + name,
		Labels:    labels,
		CreatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
}

func ownedBy(m Meta, owner Meta) Meta {
	m.Owners = append(m.Owners, OwnerRef{
		Kind:       owner.Kind,
		Name:       owner.Name,
		UID:        owner.UID,
		Controller: true,
	})
	return m
}

func appLabels(name string) map[string]string {
	return map[string]string{"app.kubernetes.io/instance": name, "app.kubernetes.io/name": name}
}

// deployment builds the full chain a real Deployment has: the Deployment, the
// ReplicaSet it owns, and the pods the ReplicaSet owns.
func deployment(s *Snapshot, name string, desired, ready int32, podReason string) {
	dep := meta("Deployment", name, appLabels(name))
	rs := ownedBy(meta("ReplicaSet", name+"-7d8f", appLabels(name)), dep)

	s.Workloads = append(s.Workloads, Workload{
		Meta: dep, Desired: desired, Ready: ready, Updated: desired, Replicated: true,
		Selector: map[string]string{"app.kubernetes.io/name": name},
	})
	s.Owners = append(s.Owners, rs)

	for i := int32(0); i < desired; i++ {
		pod := ownedBy(meta("Pod", name+"-7d8f-"+itoa(int(i)), appLabels(name)), rs)
		p := Pod{Meta: pod, Phase: "Running", Ready: i < ready}
		if i >= ready {
			p.Phase, p.Reason = "Pending", podReason
		}
		s.Pods = append(s.Pods, p)
	}
}

func find(t *testing.T, apps []Application, name string) Application {
	t.Helper()
	for _, a := range apps {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no application named %q in %v", name, names(apps))
	return Application{}
}

func names(apps []Application) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.Name)
	}
	return out
}

func TestPodsFollowTheirOwnersIntoOneApplication(t *testing.T) {
	var s Snapshot
	deployment(&s, "api", 3, 3, "")

	apps := Group(s)
	if len(apps) != 1 {
		t.Fatalf("one Deployment with its ReplicaSet and pods is one application, got %v", names(apps))
	}
	api := apps[0]
	if len(api.Pods) != 3 || len(api.Workloads) != 1 {
		t.Fatalf("api must hold its workload and all three pods, got %d workloads and %d pods",
			len(api.Workloads), len(api.Pods))
	}
	// The ReplicaSet exists to connect the two; nobody calls it an application.
	if name := api.Name; name != "api" {
		t.Errorf("the application is named after the Deployment, got %q", name)
	}
}

func TestWorkloadsSharingAnInstanceLabelAreOneApplication(t *testing.T) {
	var s Snapshot
	for _, name := range []string{"payments-api", "payments-worker"} {
		dep := meta("Deployment", name, map[string]string{
			"app.kubernetes.io/instance": "payments",
			"app.kubernetes.io/name":     name,
		})
		s.Workloads = append(s.Workloads, Workload{Meta: dep, Desired: 1, Ready: 1, Replicated: true})
	}

	apps := Group(s)
	if len(apps) != 1 {
		t.Fatalf("one Helm release is one application, got %v", names(apps))
	}
	if len(apps[0].Workloads) != 2 || apps[0].Name != "payments" {
		t.Fatalf("expected both workloads under \"payments\", got %q with %d workloads",
			apps[0].Name, len(apps[0].Workloads))
	}
}

func TestUnlabelledWorkloadsStayApart(t *testing.T) {
	var s Snapshot
	for _, name := range []string{"api", "worker"} {
		s.Workloads = append(s.Workloads, Workload{
			Meta: meta("Deployment", name, nil), Desired: 1, Ready: 1, Replicated: true,
		})
	}

	if apps := Group(s); len(apps) != 2 {
		t.Fatalf("without labels each workload is its own application, got %v", names(apps))
	}
}

func TestAPodWhoseOwnerIsMissingStillAppears(t *testing.T) {
	// Listing ReplicaSets can be denied while listing pods is allowed. The pod
	// must not vanish from the dashboard because a link is missing.
	var s Snapshot
	rs := meta("ReplicaSet", "api-7d8f", appLabels("api"))
	s.Pods = append(s.Pods, Pod{
		Meta:  ownedBy(meta("Pod", "api-7d8f-0", appLabels("api")), rs),
		Phase: "Running", Ready: true,
	})

	apps := Group(s)
	if len(apps) != 1 || apps[0].Name != "api" {
		t.Fatalf("the pod's labels must name its application, got %v", names(apps))
	}
}

func TestABarePodWithoutLabelsIsNamedByItsController(t *testing.T) {
	var s Snapshot
	rs := meta("ReplicaSet", "api-7d8f", nil)
	s.Pods = append(s.Pods, Pod{
		Meta:  ownedBy(meta("Pod", "api-7d8f-2xk9l", nil), rs),
		Phase: "Running", Ready: true,
	})

	apps := Group(s)
	if len(apps) != 1 || apps[0].Name != "api-7d8f" {
		t.Fatalf("the controller names the group better than the pod does, got %v", names(apps))
	}
}

func TestJobsJoinTheirCronJob(t *testing.T) {
	var s Snapshot
	cron := meta("CronJob", "billing", nil)
	job := ownedBy(meta("Job", "billing-29000", nil), cron)
	s.Workloads = append(s.Workloads,
		Workload{Meta: cron},
		Workload{Meta: job},
	)
	s.Pods = append(s.Pods, Pod{Meta: ownedBy(meta("Pod", "billing-29000-abc", nil), job), Phase: "Succeeded"})

	apps := Group(s)
	if len(apps) != 1 || apps[0].Name != "billing" {
		t.Fatalf("a Job belongs to its CronJob, got %v", names(apps))
	}
	if len(apps[0].Pods) != 1 {
		t.Fatalf("the job's pod belongs to the same application, got %d pods", len(apps[0].Pods))
	}
}

func TestAServiceJoinsThroughItsSelector(t *testing.T) {
	var s Snapshot
	deployment(&s, "api", 1, 1, "")
	s.Services = append(s.Services, Service{
		Meta:     meta("Service", "api-internal", nil), // no app labels at all
		Selector: map[string]string{"app.kubernetes.io/name": "api"},
		Type:     "ClusterIP",
	})

	api := find(t, Group(s), "api")
	if len(api.Services) != 1 {
		t.Fatalf("a selector that matches the application's pods attaches the service, got %d", len(api.Services))
	}
}

func TestAServiceThatSelectsNothingIsLeftOut(t *testing.T) {
	var s Snapshot
	deployment(&s, "api", 1, 1, "")
	s.Services = append(s.Services, Service{
		Meta:     meta("Service", "orphan", nil),
		Selector: map[string]string{"app.kubernetes.io/name": "gone"},
	})

	apps := Group(s)
	if len(apps) != 1 {
		t.Fatalf("an unmatched service must not invent an application, got %v", names(apps))
	}
	if len(apps[0].Services) != 0 {
		t.Fatalf("it must not be attached to an unrelated application either")
	}
}

func TestAnIngressJoinsThroughItsBackendService(t *testing.T) {
	var s Snapshot
	deployment(&s, "api", 1, 1, "")
	s.Services = append(s.Services, Service{
		Meta:     meta("Service", "api", appLabels("api")),
		Selector: map[string]string{"app.kubernetes.io/name": "api"},
	})
	s.Ingresses = append(s.Ingresses, Ingress{
		Meta:     meta("Ingress", "public", nil),
		Hosts:    []string{"api.example.com"},
		Backends: []string{"api"},
	})

	api := find(t, Group(s), "api")
	if len(api.Ingresses) != 1 {
		t.Fatalf("the ingress routes to the application's service, got %d ingresses", len(api.Ingresses))
	}
}

func TestTheWorstApplicationComesFirst(t *testing.T) {
	var s Snapshot
	deployment(&s, "healthy-app", 2, 2, "")
	deployment(&s, "degraded-app", 3, 2, "CrashLoopBackOff")
	deployment(&s, "down-app", 2, 0, "ImagePullBackOff")

	apps := Group(s)
	want := []string{"down-app", "degraded-app", "healthy-app"}
	for i, name := range want {
		if apps[i].Name != name {
			t.Fatalf("applications must be ordered worst first, got %v", names(apps))
		}
	}
}

func TestOwnerCyclesDoNotHang(t *testing.T) {
	// A malformed owner reference must cost lookups, never the whole UI.
	a := meta("Deployment", "a", nil)
	b := meta("Deployment", "b", nil)
	a = ownedBy(a, b)
	b = ownedBy(b, a)

	s := Snapshot{Workloads: []Workload{{Meta: a}, {Meta: b}}}
	if apps := Group(s); len(apps) == 0 {
		t.Fatal("a cyclic chain must still produce applications")
	}
}
