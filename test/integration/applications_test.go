//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/workloads"
)

// budgetDashboard is what one namespace's application view may cost. It is the
// number that decides whether kubeui opens on something useful or on a spinner.
const budgetDashboard = 5 * time.Second

// seededNamespace is the first namespace `task kind:seed` creates.
const seededNamespace = "kubeui-load-000"

func applicationsIn(t *testing.T, namespace string) ([]application.Application, application.Snapshot) {
	t.Helper()
	apps, snapshot, err := shared.factory.Applications(ctx(t), shared.context,
		workloads.Options{Namespace: namespace})
	if err != nil {
		t.Fatalf("Applications(%s): %v", namespace, err)
	}
	return apps, snapshot
}

func TestApplicationsAreInferredFromARealCluster(t *testing.T) {
	start := time.Now()
	apps, snapshot := applicationsIn(t, seededNamespace)
	elapsed := time.Since(start)

	t.Logf("%d applications in %s in %s", len(apps), seededNamespace, elapsed.Round(time.Millisecond))
	if elapsed > budgetDashboard {
		t.Errorf("the dashboard took %s, budget is %s — this is the first screen", elapsed, budgetDashboard)
	}
	if len(apps) == 0 {
		t.Fatalf("the seeded namespace must produce applications; snapshot: %+v", snapshot.Gaps)
	}
	if len(snapshot.Gaps) != 0 {
		t.Errorf("nothing should be unreadable in a kind cluster, got %+v", snapshot.Gaps)
	}

	for _, a := range apps {
		if len(a.Workloads) == 0 {
			t.Errorf("application %q has no workload", a.Name)
			continue
		}
		for _, w := range a.Workloads {
			if w.Kind == "ReplicaSet" {
				t.Errorf("application %q lists a ReplicaSet as a workload; it is only a link in the chain", a.Name)
			}
		}
		if len(a.Pods) == 0 {
			t.Errorf("application %q has no pods, so ownership was not resolved", a.Name)
		}
	}
}

func TestPodsAreGroupedUnderTheDeploymentThatOwnsThem(t *testing.T) {
	apps, _ := applicationsIn(t, seededNamespace)

	for _, a := range apps {
		for _, p := range a.Pods {
			// The seeder names every pod after its application.
			if !strings.HasPrefix(p.Name, a.Name+"-") {
				t.Errorf("pod %q was grouped under application %q", p.Name, a.Name)
			}
		}
		for _, s := range a.Services {
			if !strings.HasPrefix(s.Name, a.Name) {
				t.Errorf("service %q was grouped under application %q", s.Name, a.Name)
			}
		}
	}
}

func TestHealthMatchesWhatTheClusterReports(t *testing.T) {
	// The seeder deliberately breaks a fraction of the applications, spread
	// across namespaces by a hash, so the whole seeded set is scanned.
	var all []application.Application
	for _, ns := range seededNamespaces(t) {
		apps, _ := applicationsIn(t, ns)
		all = append(all, apps...)
	}
	if len(all) == 0 {
		t.Fatal("no seeded applications found; run `task kind:seed`")
	}

	counts := application.Summarise(all)
	t.Logf("%d applications: %d healthy, %d degraded, %d down, %d unknown",
		counts.Total, counts.Healthy, counts.Degraded, counts.Down, counts.Unknown)

	for _, a := range all {
		switch a.Health {
		case application.Healthy:
			if a.ReadyPods != a.DesiredPods {
				t.Errorf("%s is healthy with %d of %d pods ready", a.Key(), a.ReadyPods, a.DesiredPods)
			}
		case application.Down:
			if a.ReadyPods != 0 {
				t.Errorf("%s is down with %d pods ready", a.Key(), a.ReadyPods)
			}
		case application.Degraded:
			if a.ReadyPods == a.DesiredPods && len(a.Problems) == 0 {
				t.Errorf("%s is degraded with nothing wrong with it", a.Key())
			}
		}
	}
}

func TestTheDashboardRendersTheSeededCluster(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))

	out := frame(m)
	if !strings.Contains(out, "app-00") {
		t.Errorf("the dashboard must list the seeded applications:\n%s", out)
	}
	if strings.Contains(out, "Looking for applications") {
		t.Errorf("the dashboard must be loaded by now:\n%s", out)
	}

	drain(t, m, m.OpenApplicationForTest("app-00"))
	detail := frame(m)
	for _, want := range []string{"WORKLOADS", "PODS", "NETWORK", "Deployment"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail view must show %q:\n%s", want, detail)
		}
	}
}

// seededNamespaces lists the namespaces the load generator created.
func seededNamespaces(t *testing.T) []string {
	t.Helper()
	list, err := shared.factory.ListNamespaces(ctx(t), shared.context)
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	var out []string
	for _, name := range list.Names {
		if strings.HasPrefix(name, "kubeui-load-") {
			out = append(out, name)
		}
	}
	return out
}

func TestOpeningAnObjectFromTheClusterShowsItsDocument(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))
	drain(t, m, m.OpenApplicationForTest("app-00"))
	drain(t, m, m.OpenObjectForTest("Deployment", "app-00", seededNamespace))

	out := frame(m)
	for _, want := range []string{"Deployment/app-00", "IDENTITY", "RELATED"} {
		if !strings.Contains(out, want) {
			t.Errorf("the object view must show %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Loading Deployment") {
		t.Errorf("the object must have been fetched by now:\n%s", out)
	}

	// The ReplicaSet the Deployment owns must be reachable from here, which is
	// what makes walking the ownership chain possible at all.
	if !strings.Contains(out, "ReplicaSet") {
		t.Errorf("the objects it owns must be listed:\n%s", out)
	}

	m.ShowYAMLForTest()
	yaml := frame(m)
	if !strings.Contains(yaml, "apiVersion:") || !strings.Contains(yaml, "kind: Deployment") {
		t.Errorf("the document the server holds must be readable:\n%s", yaml)
	}
}
