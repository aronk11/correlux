package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// The tests here pin the states that used to raise an alarm and must not:
// each one is something a healthy cluster does every day.

func TestAServiceBehindAWorkloadScaledToZeroIsNotASelectorTypo(t *testing.T) {
	a := app()
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}
	in := &Input{App: a, Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 0, 0)}}}
	none(t, diagnose(t, in), "service.noendpoints")
}

func TestAnAutoscalerStandingDownAtZeroReplicasIsNotBlind(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Autoscalers: []application.Autoscaler{hpa(map[string]application.Condition{
		"ScalingActive": {Status: "False", Reason: "ScalingDisabled", Message: "scaling is disabled since the replica count of the target is zero"},
	}, 0, 10)}}}
	none(t, diagnose(t, in), "hpa.blind")
}

func TestAnOldFailedRunOfACronJobIsHistoryOnceALaterRunWorked(t *testing.T) {
	job := func(name string, age time.Duration, failed bool) application.Workload {
		return application.Workload{
			Meta:   application.Meta{Kind: "Job", Name: name, Namespace: "shop", UID: name, CreatedAt: now.Add(-age)},
			Failed: failed, FailedReason: "BackoffLimitExceeded",
		}
	}
	a := application.Application{Name: "backup", Namespace: "shop"}
	a.Workloads = []application.Workload{job("backup-old", 72*time.Hour, true), job("backup-new", time.Hour, false)}
	none(t, diagnose(t, &Input{App: a}), "job.failed")

	a.Workloads = []application.Workload{job("backup-old", 72*time.Hour, false), job("backup-new", time.Hour, true)}
	if d := find(t, diagnose(t, &Input{App: a}), "job.failed"); !strings.Contains(d.Problem, "backup-new") {
		t.Errorf("the newest run failed and must be named: %q", d.Problem)
	}
}

func TestEvictedPodsThatWereReplacedAreNotAFailure(t *testing.T) {
	alive := pod("payments-2")
	alive.Ready = true
	dead := pod("payments-1", application.Container{Name: "app", State: "terminated", ExitCode: 137, Restarts: 3})
	dead.Phase, dead.Reason = "Failed", "Evicted"

	a := app(alive, dead)
	a.Workloads[0].Desired = 1 // one replica wanted, one ready: the corpse was replaced
	findings := diagnose(t, &Input{App: a})
	none(t, findings, "pod.failed")
	none(t, findings, "pod.crashloop")
}

func TestAProbeIsNotBlamedForAProcessThatExitedByItself(t *testing.T) {
	c := waiting("CrashLoopBackOff", "")
	c.Restarts, c.LastExitCode, c.LastReason = 6, 1, "Error"
	p := pod("payments-1", c)
	events := []application.Event{warning(podAbout("payments-1"), "Unhealthy",
		"Liveness probe failed: Get \"http://10.0.0.4:8080/healthz\": connection refused")}

	d := find(t, diagnose(t, &Input{App: app(p), Context: application.Context{Events: events}}), "pod.crashloop")
	if strings.Contains(d.Cause, "probe fails, so the kubelet kills") {
		t.Errorf("exit code 1 is the process's own exit, not the kubelet's kill: %q", d.Cause)
	}

	c.LastExitCode = 137
	killed := find(t, diagnose(t, &Input{App: app(pod("payments-1", c)), Context: application.Context{Events: events}}), "pod.crashloop")
	if !strings.Contains(killed.Cause, "liveness probe fails") {
		t.Errorf("a SIGKILL after failed liveness probes is the probe's doing: %q", killed.Cause)
	}
}

func TestImagePullCausesAreToldApart(t *testing.T) {
	cause := func(c application.Container, events ...application.Event) Diagnosis {
		return find(t, diagnose(t, &Input{App: app(pod("payments-1", c)), Context: application.Context{Events: events}}), "pod.imagepull")
	}

	ambiguous := cause(waiting("ErrImagePull", "pull access denied for acme/payments, repository does not exist or may require authorization"))
	if !strings.Contains(ambiguous.Cause, "does not exist, or the node has no credentials") || ambiguous.Confidence != Medium {
		t.Errorf("the registry's deliberately ambiguous answer must stay ambiguous: %q (%v)", ambiguous.Cause, ambiguous.Confidence)
	}

	never := cause(waiting("ErrImageNeverPull", "Container image is not present with pull policy of Never"))
	if !strings.Contains(never.Cause, "imagePullPolicy is Never") {
		t.Errorf("cause = %q", never.Cause)
	}

	fromEvent := cause(waiting("ImagePullBackOff", "Back-off pulling image \"registry/payments:1.4\""),
		warning(podAbout("payments-1"), "Failed", "Failed to pull image: manifest unknown"))
	if !strings.Contains(fromEvent.Cause, "does not have that image or tag") {
		t.Errorf("the registry's answer is in the event and must be read: %q", fromEvent.Cause)
	}
}

func TestAClaimWaitingForItsPodIsNotWhatBlocksThePod(t *testing.T) {
	p := pod("db-0")
	p.Phase, p.Scheduled, p.Node, p.Claims = "Pending", false, "", []string{"data"}
	claim := application.Claim{Meta: application.Meta{Kind: "PersistentVolumeClaim", Name: "data", Namespace: "shop", UID: "pvc"}, Phase: "Pending"}
	waitingEvent := application.Event{Type: "Normal", Reason: "WaitForFirstConsumer",
		Message: "waiting for first consumer to be created before binding", LastSeen: now,
		About: application.ObjectRef{Kind: "PersistentVolumeClaim", Name: "data", UID: "pvc"}}

	in := &Input{App: app(p), Context: application.Context{Claims: []application.Claim{claim}, Events: []application.Event{waitingEvent}}}
	none(t, diagnose(t, in), "storage.unbound")

	in.Context.Events = nil
	find(t, diagnose(t, in), "storage.unbound")
}

func TestARolloutIsComparedWithTheRevisionStillServing(t *testing.T) {
	a := app(
		revisionPod("payments-c-1", "payments-c", false, waiting("CrashLoopBackOff", "")),
		revisionPod("payments-a-1", "payments-a", true),
	)
	a.Health = application.Degraded
	in := &Input{App: a, Context: application.Context{Revisions: []application.Revision{
		revision("payments-a", 3, "registry/payments:1.3", 1, 1, 72*time.Hour),
		revision("payments-b", 4, "registry/payments:1.4", 0, 0, 20*time.Minute),
		revision("payments-c", 5, "registry/payments:1.5", 1, 0, 5*time.Minute),
	}}}
	d := find(t, diagnose(t, in), "workload.revisionfailing")
	if !strings.Contains(d.Problem, "revision 3 still serves") {
		t.Errorf("two bad pushes in a row: the comparison is with the revision that works, got %q", d.Problem)
	}
}

func TestACertificateIsOnlyLateOnceItsRenewalTimeHasPassed(t *testing.T) {
	c := cert(true, now.Add(5*24*time.Hour), "")
	c.RenewalTime = now.Add(24 * time.Hour) // short-lived, renewal still ahead
	in := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{c}}}
	none(t, diagnose(t, in), "cert.expiring")

	c.RenewalTime = now.Add(-48 * time.Hour)
	in.Context.Certificates = []application.Certificate{c}
	find(t, diagnose(t, in), "cert.expiring")
}

func TestHealthyWhenTrueNodeConditionsAreNotPressure(t *testing.T) {
	p := pod("payments-1")
	p.Ready = true
	n := node("node-1", true, "NetworkUnavailable")
	d := find(t, diagnose(t, &Input{App: app(p), Context: application.Context{Nodes: []application.Node{n}}}), "node.unhealthy")
	if strings.Contains(d.Cause, "short of a resource") {
		t.Errorf("a node without pod networking is not short of a resource: %q", d.Cause)
	}
}

func TestAnOOMKillWithoutALimitIsNotBlamedOnALimit(t *testing.T) {
	c := waiting("CrashLoopBackOff", "")
	c.Restarts, c.LastExitCode, c.LastReason, c.OOMKilled = 4, 137, "OOMKilled", true
	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", c))}), "pod.crashloop")
	if strings.Contains(d.Cause, "exceeded its memory limit") || !strings.Contains(d.Cause, "no memory limit of its own") {
		t.Errorf("cause = %q", d.Cause)
	}
}

func TestAnEvictionQuotesWhatTheKubeletSaid(t *testing.T) {
	p := pod("payments-1")
	p.Phase, p.Reason, p.Message = "Failed", "Evicted", "Pod ephemeral local storage usage exceeds the total limit of containers 1Gi."
	d := find(t, diagnose(t, &Input{App: app(p)}), "pod.failed")
	if !strings.Contains(d.Cause, "ephemeral local storage") {
		t.Errorf("the kubelet says which resource; cause = %q", d.Cause)
	}
}
