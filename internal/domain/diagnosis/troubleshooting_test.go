package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

func warning(about application.ObjectRef, reason, message string) application.Event {
	return application.Event{
		Meta:     application.Meta{Namespace: "shop"},
		Type:     "Warning",
		Reason:   reason,
		Message:  message,
		Count:    4,
		LastSeen: now.Add(-time.Minute),
		About:    about,
	}
}

func podAbout(name string) application.ObjectRef {
	return application.ObjectRef{Kind: "Pod", Name: name, UID: name + "-uid"}
}

func TestCrashLoopReadsTheExitCodeConvention(t *testing.T) {
	cases := map[int32]string{
		127: "not found in the image",
		126: "cannot be executed",
		139: "segmentation fault",
		137: "SIGKILL",
		143: "SIGTERM",
		130: "signal 2",
		1:   "code 1 shortly after starting",
	}
	for code, want := range cases {
		crashing := waiting("CrashLoopBackOff", "")
		crashing.Restarts, crashing.LastExitCode, crashing.LastReason = 5, code, "Error"

		d := find(t, diagnose(t, &Input{App: app(pod("payments-1", crashing))}), "pod.crashloop")
		if !strings.Contains(d.Cause, want) {
			t.Errorf("exit %d: cause = %q, want it to contain %q", code, d.Cause, want)
		}
	}
}

func TestCrashLoopBlamesAFailingLivenessProbe(t *testing.T) {
	crashing := waiting("CrashLoopBackOff", "")
	crashing.Restarts, crashing.LastExitCode, crashing.LastReason = 7, 137, "Error"

	in := &Input{
		App: app(pod("payments-1", crashing)),
		Context: application.Context{Events: []application.Event{
			warning(podAbout("payments-1"), "Unhealthy", "Liveness probe failed: HTTP probe failed with statuscode: 503"),
		}},
	}
	d := find(t, diagnose(t, in), "pod.crashloop")

	if !strings.Contains(d.Cause, "liveness probe fails") || !strings.Contains(d.Cause, "statuscode: 503") {
		t.Errorf("the probe, not the exit code, explains these restarts; cause = %q", d.Cause)
	}
	if d.Confidence != High || d.Unknown != "" {
		t.Errorf("the kubelet stated the cause; confidence = %v, unknown = %q", d.Confidence, d.Unknown)
	}
	if tail := d.Chain[len(d.Chain)-1]; tail != "liveness probe failing" {
		t.Errorf("chain tail = %q", tail)
	}
	var probeCmd bool
	for _, s := range d.Suggestions {
		probeCmd = probeCmd || strings.Contains(s.Command, "reason=Unhealthy")
	}
	if !probeCmd {
		t.Errorf("a probe finding must offer the probe's events, got %+v", d.Suggestions)
	}
}

func TestAReadinessProbeNeverExplainsACrashLoop(t *testing.T) {
	crashing := waiting("CrashLoopBackOff", "")
	crashing.Restarts, crashing.LastExitCode = 3, 1

	in := &Input{
		App: app(pod("payments-1", crashing)),
		Context: application.Context{Events: []application.Event{
			warning(podAbout("payments-1"), "Unhealthy", "Readiness probe failed: connection refused"),
		}},
	}
	d := find(t, diagnose(t, in), "pod.crashloop")
	if strings.Contains(d.Cause, "probe") {
		t.Errorf("a readiness probe does not restart containers; cause = %q", d.Cause)
	}
}

func TestOOMStillWinsOverAProbeEvent(t *testing.T) {
	crashing := waiting("CrashLoopBackOff", "")
	crashing.Restarts, crashing.LastExitCode, crashing.OOMKilled = 3, 137, true

	in := &Input{
		App: app(pod("payments-1", crashing)),
		Context: application.Context{Events: []application.Event{
			warning(podAbout("payments-1"), "Unhealthy", "Liveness probe failed: timeout"),
		}},
	}
	d := find(t, diagnose(t, in), "pod.crashloop")
	if !strings.Contains(d.Cause, "memory limit") {
		t.Errorf("an OOM kill is the stronger fact; cause = %q", d.Cause)
	}
}

func creatingPod(name string) application.Pod {
	p := pod(name, waiting("ContainerCreating", ""))
	p.Phase = "Pending"
	return p
}

func TestStuckCreatingNamesTheMissingConfigMap(t *testing.T) {
	in := &Input{
		App: app(creatingPod("payments-1")),
		Context: application.Context{Events: []application.Event{
			warning(podAbout("payments-1"), "FailedMount",
				`MountVolume.SetUp failed for volume "config" : configmap "payments-config" not found`),
		}},
	}
	d := find(t, diagnose(t, in), "pod.creating")

	if d.Severity != Critical || d.Confidence != High {
		t.Errorf("severity/confidence = %v/%v", d.Severity, d.Confidence)
	}
	if !strings.Contains(d.Cause, "ConfigMap") || !strings.Contains(d.Cause, "payments-config") {
		t.Errorf("cause = %q, want the ConfigMap named", d.Cause)
	}
	if d.Suggestions[0].Command != "kubectl get configmaps -n shop" {
		t.Errorf("first suggestion = %q", d.Suggestions[0].Command)
	}
}

func TestStuckCreatingTellsVolumeAndNetworkFailuresApart(t *testing.T) {
	cases := []struct{ reason, message, want string }{
		{"FailedMount", `MountVolume.SetUp failed for volume "tls" : secret "payments-tls" not found`, "Secret"},
		{"FailedAttachVolume", `Multi-Attach error for volume "pvc-1" Volume is already exclusively attached to one node`, "another node"},
		{"FailedCreatePodSandBox", `plugin type="aws-cni" failed (add): add cmd: failed to assign an IP address to container`, "IP address"},
		{"FailedCreatePodSandBox", `rpc error: code = Unknown desc = failed to setup network for sandbox`, "network sandbox"},
		{"FailedAttachVolume", `AttachVolume.Attach failed for volume "pvc-2" : rpc error: timed out`, "storage driver"},
	}
	for _, c := range cases {
		in := &Input{
			App:     app(creatingPod("payments-1")),
			Context: application.Context{Events: []application.Event{warning(podAbout("payments-1"), c.reason, c.message)}},
		}
		d := find(t, diagnose(t, in), "pod.creating")
		if !strings.Contains(d.Cause, c.want) {
			t.Errorf("%s: cause = %q, want %q", c.reason, d.Cause, c.want)
		}
	}
}

func TestAPodThatIsMerelyStartingIsNotStuck(t *testing.T) {
	none(t, diagnose(t, &Input{App: app(creatingPod("payments-1"))}), "pod.creating")
}

func TestQuotaRejectionOnTheReplicaSetExplainsMissingReplicas(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Workloads[0].Desired, a.Workloads[0].Ready = 3, 1

	in := &Input{App: a, Context: application.Context{Events: []application.Event{
		warning(application.ObjectRef{Kind: "ReplicaSet", Name: "payments-7d8f9c", UID: "rs-uid"}, "FailedCreate",
			`pods "payments-7d8f9c-x1" is forbidden: exceeded quota: compute, requested: cpu=500m, used: cpu=4, limited: cpu=4`),
		// A different Deployment whose name starts the same way.
		warning(application.ObjectRef{Kind: "ReplicaSet", Name: "payments-api-5c6d", UID: "rs2-uid"}, "FailedCreate",
			`admission webhook "deny.example.com" denied the request`),
	}}}

	d := find(t, diagnose(t, in), "workload.replicas")
	if !strings.Contains(d.Cause, "exceeded quota") || d.Confidence != High {
		t.Errorf("the ReplicaSet said why; cause = %q, confidence = %v", d.Cause, d.Confidence)
	}
	if d.Suggestions[0].Command != "kubectl describe resourcequota -n shop" {
		t.Errorf("first suggestion = %q, want the quota", d.Suggestions[0].Command)
	}
	for _, e := range d.Evidence {
		if strings.Contains(e.Name, "payments-api") {
			t.Errorf("another Deployment's ReplicaSet was quoted: %+v", e)
		}
	}
}
