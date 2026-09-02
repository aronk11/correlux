package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
)

var now = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// The builders below describe a cluster the way a person would: an application
// with a workload, some pods, and one thing wrong with them.

func app(pods ...application.Pod) application.Application {
	a := application.Application{
		Name:      "payments",
		Namespace: "shop",
		Workloads: []application.Workload{{
			Meta: application.Meta{
				Kind: "Deployment", Name: "payments", Namespace: "shop", UID: "dep-uid",
			},
			Desired:    int32(len(pods)),
			Selector:   map[string]string{"app": "payments"},
			Replicated: true,
		}},
		Pods: pods,
	}
	for i := range a.Pods {
		if a.Pods[i].Ready {
			a.Workloads[0].Ready++
		}
	}
	return a
}

func pod(name string, containers ...application.Container) application.Pod {
	return application.Pod{
		Meta: application.Meta{
			Kind: "Pod", Name: name, Namespace: "shop", UID: name + "-uid",
			Labels: map[string]string{"app": "payments"},
		},
		Phase:      "Running",
		Node:       "node-1",
		Scheduled:  true,
		Containers: containers,
	}
}

func waiting(reason, message string) application.Container {
	return application.Container{Name: "app", Image: "registry/payments:1.4", State: "waiting", Reason: reason, Message: message}
}

func diagnose(t *testing.T, in *Input) []Diagnosis {
	t.Helper()
	in.Now = now
	return Diagnose(in)
}

// find returns the finding produced by one rule.
func find(t *testing.T, findings []Diagnosis, rule string) Diagnosis {
	t.Helper()
	for _, d := range findings {
		if d.Rule == rule {
			return d
		}
	}
	t.Fatalf("no finding from rule %q; got %v", rule, ruleNames(findings))
	return Diagnosis{}
}

func none(t *testing.T, findings []Diagnosis, rule string) {
	t.Helper()
	for _, d := range findings {
		if d.Rule == rule {
			t.Fatalf("rule %q fired when it should not have: %+v", rule, d)
		}
	}
}

func ruleNames(findings []Diagnosis) []string {
	out := make([]string, 0, len(findings))
	for _, d := range findings {
		out = append(out, d.Rule)
	}
	return out
}

func TestCrashLoopExplainsTheLastRunNotTheWait(t *testing.T) {
	crashing := waiting("CrashLoopBackOff", "back-off 5m0s restarting failed container")
	crashing.Restarts = 12
	crashing.LastExitCode = 1
	crashing.LastReason = "Error"

	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", crashing))}), "pod.crashloop")

	if d.Severity != Critical || d.Confidence != High {
		t.Errorf("severity/confidence = %v/%v, want critical/high", d.Severity, d.Confidence)
	}
	if !strings.Contains(d.Cause, "code 1") {
		t.Errorf("the cause must name the exit code the cluster reported, got %q", d.Cause)
	}
	// The logs of the run that failed are the whole point.
	if !strings.Contains(d.Suggestions[0].Command, "--previous") {
		t.Errorf("the first suggestion must reach the failed run's logs, got %q", d.Suggestions[0].Command)
	}
}

func TestCrashLoopPrefersTheMemoryKillAsTheCause(t *testing.T) {
	oom := waiting("CrashLoopBackOff", "")
	oom.LastExitCode = 137
	oom.LastReason = "OOMKilled"
	oom.OOMKilled = true

	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", oom))}), "pod.crashloop")
	if !strings.Contains(d.Cause, "memory limit") {
		t.Errorf("an OOM kill outranks a bare exit code, got %q", d.Cause)
	}
	if got := d.Chain[len(d.Chain)-1]; got != "OOMKilled" {
		t.Errorf("the chain must end at the real failure, got %q", got)
	}
}

func TestCrashLoopWithoutAPreviousRunSaysLess(t *testing.T) {
	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", waiting("CrashLoopBackOff", "")))}), "pod.crashloop")
	if d.Cause != "" {
		t.Errorf("with no evidence the rule must not invent a cause, got %q", d.Cause)
	}
	if d.Confidence == High {
		t.Error("confidence must drop when the cluster did not say why")
	}
}

func TestImagePullTellsTheThreeFailuresApart(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    string
	}{
		{"missing tag", `manifest for registry/payments:1.4 not found`, "does not have that image or tag"},
		{"no credentials", `unauthorized: authentication required`, "no credentials"},
		{"no network", `dial tcp 10.0.0.1:443: i/o timeout`, "cannot reach the registry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := find(t, diagnose(t, &Input{
				App: app(pod("payments-1", waiting("ImagePullBackOff", tc.message))),
			}), "pod.imagepull")

			if !strings.Contains(d.Cause, tc.want) {
				t.Errorf("cause = %q, want it to mention %q", d.Cause, tc.want)
			}
			if d.Confidence != High {
				t.Errorf("the kubelet said why; confidence = %v", d.Confidence)
			}
		})
	}
}

func TestImagePullWithoutAMessageStaysHonest(t *testing.T) {
	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", waiting("ErrImagePull", "")))}), "pod.imagepull")
	if d.Confidence != Low {
		t.Errorf("without a message the rule is guessing; confidence = %v", d.Confidence)
	}
	if !strings.Contains(d.Cause, "did not say why") {
		t.Errorf("the cause must admit what it does not know, got %q", d.Cause)
	}
}

func TestConfigErrorQuotesTheKubelet(t *testing.T) {
	message := `secret "payments-db" not found`
	d := find(t, diagnose(t, &Input{
		App: app(pod("payments-1", waiting("CreateContainerConfigError", message))),
	}), "pod.configerror")

	if d.Cause != message {
		t.Errorf("the kubelet's own words must survive, got %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
}

func TestOutOfMemoryFiresForARunningPodAndNotForACrashLoop(t *testing.T) {
	restarted := application.Container{Name: "app", State: "running", Restarts: 3, OOMKilled: true, LastExitCode: 137}
	findings := diagnose(t, &Input{App: app(pod("payments-1", restarted))})
	d := find(t, findings, "pod.oomkilled")
	if d.Severity != Warning {
		t.Errorf("a pod that recovered is a warning, got %v", d.Severity)
	}

	crashing := waiting("CrashLoopBackOff", "")
	crashing.OOMKilled = true
	crashing.LastExitCode = 137
	// The crash-loop rule already explains this one; two findings about the
	// same container would be noise.
	none(t, diagnose(t, &Input{App: app(pod("payments-2", crashing))}), "pod.oomkilled")
}

func TestUnschedulableQuotesTheScheduler(t *testing.T) {
	p := pod("payments-1")
	p.Phase, p.Scheduled = "Pending", false
	p.ScheduledReason = "Unschedulable"
	p.ScheduledMessage = "0/3 nodes are available: 3 Insufficient cpu."
	p.Node = ""

	d := find(t, diagnose(t, &Input{App: app(p)}), "pod.unschedulable")
	if d.Cause != p.ScheduledMessage {
		t.Errorf("the scheduler explains itself precisely; cause = %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
}

func TestARunningPodIsNeverCalledUnschedulable(t *testing.T) {
	// The scheduling condition is dropped from a pod that has been running for
	// a while; a rule that only looked at the flag would accuse it.
	p := pod("payments-1")
	p.Scheduled = false
	p.Phase = "Running"

	none(t, diagnose(t, &Input{App: app(p)}), "pod.unschedulable")
}

func TestNotReadyQuotesTheProbeEventWhenThereIsOne(t *testing.T) {
	p := pod("payments-1", application.Container{Name: "app", State: "running"})
	in := &Input{
		App: app(p),
		Context: application.Context{Events: []application.Event{{
			Type: "Warning", Reason: "Unhealthy", Count: 9,
			Message:  "Readiness probe failed: dial tcp 10.244.0.5:8080: connect: connection refused",
			LastSeen: now.Add(-time.Minute),
			About:    application.ObjectRef{Kind: "Pod", Name: "payments-1", UID: "payments-1-uid"},
		}}},
	}

	d := find(t, diagnose(t, in), "pod.notready")
	if !strings.Contains(d.Cause, "connection refused") {
		t.Errorf("the probe's own message is the answer, got %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}

	// Without events the rule still fires, and says less.
	quiet := find(t, diagnose(t, &Input{App: app(p)}), "pod.notready")
	if quiet.Confidence != Low {
		t.Errorf("with no event to quote, confidence = %v, want low", quiet.Confidence)
	}
}

func TestPodFailedNamesTheNodesVerdict(t *testing.T) {
	p := pod("payments-1")
	p.Phase, p.Reason, p.Ready = "Failed", "Evicted", false

	d := find(t, diagnose(t, &Input{App: app(p)}), "pod.failed")
	if !strings.Contains(d.Cause, "evicted") {
		t.Errorf("cause = %q, want the eviction", d.Cause)
	}
}

func TestCompletedPodsAreNotADiagnosis(t *testing.T) {
	p := pod("import-1")
	p.Phase, p.Ready = "Succeeded", false

	// A Job's pod that ran to completion, which is the only place a Succeeded
	// pod legitimately appears.
	job := application.Application{
		Name:      "import",
		Namespace: "shop",
		Workloads: []application.Workload{{
			Meta: application.Meta{Kind: "Job", Name: "import", Namespace: "shop", UID: "job-uid"},
		}},
		Pods: []application.Pod{p},
	}

	if findings := diagnose(t, &Input{App: job}); len(findings) != 0 {
		t.Errorf("a finished job pod is not a problem: %v", ruleNames(findings))
	}
}
