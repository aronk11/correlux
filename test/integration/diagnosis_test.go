//go:build integration

package integration

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/diagnosis"
	"github.com/aronk11/correlux/internal/kube/workloads"
)

// budgetEvidence is what the evidence behind one explanation may cost. It is
// fetched while a user waits for an answer during an incident.
const budgetEvidence = 5 * time.Second

func evidenceIn(t *testing.T, namespace string) application.Context {
	t.Helper()
	start := time.Now()
	evidence, err := shared.factory.ApplicationContext(ctx(t), shared.context,
		workloads.Options{Namespace: namespace})
	if err != nil {
		t.Fatalf("ApplicationContext(%s): %v", namespace, err)
	}
	if elapsed := time.Since(start); elapsed > budgetEvidence {
		t.Errorf("reading the evidence took %s, budget is %s", elapsed, budgetEvidence)
	}
	return evidence
}

func TestTheEvidenceIsReadableFromARealCluster(t *testing.T) {
	evidence := evidenceIn(t, seededNamespace)

	if len(evidence.Gaps) != 0 {
		t.Errorf("nothing should be unreadable in a kind cluster, got %+v", evidence.Gaps)
	}
	if len(evidence.Nodes) == 0 {
		t.Error("a cluster always has nodes; the node listing must have produced some")
	}
	// The seeder gives every application a service, and the control plane
	// publishes an EndpointSlice for each of them.
	if len(evidence.Endpoints) == 0 {
		t.Error("no endpoints were read, so no service can ever be explained")
	}
}

func TestABrokenSeededApplicationIsExplained(t *testing.T) {
	apps, snapshot := applicationsIn(t, seededNamespace)
	evidence := evidenceIn(t, seededNamespace)

	explained := 0
	for i := range apps {
		app := apps[i]
		findings := diagnosis.Diagnose(&diagnosis.Input{
			App: app, Context: evidence, Scope: snapshot, Now: time.Now(),
		})

		if app.Health == application.Healthy {
			// A healthy application may still carry an informational finding
			// (the seeder pauses every Deployment), but never a critical one.
			for _, f := range findings {
				if f.Severity == diagnosis.Critical {
					t.Errorf("%s is healthy but got a critical finding: %s", app.Key(), f.Problem)
				}
			}
			continue
		}

		primary, ok := diagnosis.Primary(findings)
		if !ok {
			t.Errorf("%s is %s and nothing explained it", app.Key(), app.Health)
			continue
		}
		explained++
		t.Logf("%s (%s): %s — %s [%s]", app.Key(), app.Health, primary.Problem, primary.Cause, primary.Confidence)

		if len(primary.Evidence) == 0 {
			t.Errorf("%s: a finding without evidence is an assertion, not a diagnosis", app.Key())
		}
		if len(primary.Chain) == 0 {
			t.Errorf("%s: the finding must show how the failure is connected", app.Key())
		}
	}

	if explained == 0 {
		t.Skip("no unhealthy applications in this namespace; the seeder spreads breakage by hash")
	}
}

// TestTheSeededBreakageIsReadCorrectly checks the rules against the states the
// seeder writes, which are the states a kubelet writes: a container waiting on
// an image pull, and one in a crash loop after being OOM-killed. Which of them
// exist depends on the seeder's hash, so each is asserted where it is found.
func TestTheSeededBreakageIsReadCorrectly(t *testing.T) {
	seen := map[string]bool{}

	for _, ns := range seededNamespaces(t) {
		apps, snapshot := applicationsIn(t, ns)
		evidence := evidenceIn(t, ns)

		for i := range apps {
			findings := diagnosis.Diagnose(&diagnosis.Input{
				App: apps[i], Context: evidence, Scope: snapshot, Now: time.Now(),
			})
			for _, f := range findings {
				switch f.Rule {
				case "pod.imagepull":
					seen[f.Rule] = true
					if len(f.Evidence) == 0 || !strings.Contains(f.Evidence[0].Detail, "registry.k8s.io") {
						t.Errorf("%s: the evidence must name the image, got %+v", apps[i].Key(), f.Evidence)
					}
					if !strings.Contains(f.Problem, "image") {
						t.Errorf("%s: problem = %q", apps[i].Key(), f.Problem)
					}
				case "pod.crashloop":
					seen[f.Rule] = true
					// The seeder's crash loop ends in an OOM kill; the rule must
					// read that from the previous run rather than from the
					// back-off the pod is currently in.
					if !strings.Contains(f.Cause, "memory limit") {
						t.Errorf("%s: cause = %q, want the OOM kill", apps[i].Key(), f.Cause)
					}
					if f.Confidence != diagnosis.High {
						t.Errorf("%s: confidence = %v, want high", apps[i].Key(), f.Confidence)
					}
				}
			}
		}
	}

	// The seeder always breaks a fraction of the applications, so at least one
	// of the two must have been exercised; if neither was, the seeded cluster
	// is not what these tests assume.
	if len(seen) == 0 {
		t.Error("no broken application was found; run `task kind:seed`")
	}
	t.Logf("rules exercised against the cluster: %v", keys(seen))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTheWhyScreenRendersAgainstTheCluster(t *testing.T) {
	m := newModelFor(t)
	drain(t, m, m.Init())
	drain(t, m, m.SwitchNamespaceForTest(seededNamespace))
	drain(t, m, m.ExplainForTest("app-00"))

	out := frame(m)
	if !strings.Contains(out, "app-00") {
		t.Errorf("the explanation must name its application:\n%s", out)
	}
	if strings.Contains(out, "have not been read yet") {
		t.Errorf("the evidence must have been fetched by now:\n%s", out)
	}
}
