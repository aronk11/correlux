package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// revision is one ReplicaSet of the payments Deployment, with the image its
// template runs.
func revision(name string, number int64, image string, desired, ready int32, age time.Duration) application.Revision {
	return application.Revision{
		Meta: application.Meta{
			Kind: "ReplicaSet", Name: name, Namespace: "shop", UID: name + "-uid", CreatedAt: now.Add(-age),
			Owners: []application.OwnerRef{{Kind: "Deployment", Name: "payments", UID: "dep-uid", Controller: true}},
		},
		Number:   number,
		Desired:  desired,
		Ready:    ready,
		Template: map[string]string{"container app image": image, "container app env DB_PASSWORD": "hunter2-" + name},
	}
}

// revisionPod is a pod the ReplicaSet of one revision created.
func revisionPod(name, rs string, ready bool, containers ...application.Container) application.Pod {
	p := pod(name, containers...)
	p.Ready = ready
	p.Owners = []application.OwnerRef{{Kind: "ReplicaSet", Name: rs, UID: rs + "-uid", Controller: true}}
	return p
}

func TestANewRevisionFailingNextToAServingOneIsNamed(t *testing.T) {
	a := app(
		revisionPod("payments-new-1", "payments-new", false, waiting("CrashLoopBackOff", "")),
		revisionPod("payments-old-1", "payments-old", true),
	)
	a.Health = application.Degraded
	in := &Input{App: a, Context: application.Context{Revisions: []application.Revision{
		revision("payments-old", 13, "registry/payments:1.4", 1, 1, 48*time.Hour),
		revision("payments-new", 14, "registry/payments:1.5", 1, 0, 10*time.Minute),
	}}}

	d := find(t, diagnose(t, in), "workload.revisionfailing")
	if d.Confidence != Medium || d.Severity != Warning {
		t.Errorf("a comparison the cluster shows is medium confidence, got %v/%v", d.Confidence, d.Severity)
	}
	if !strings.Contains(d.Problem, "revision 14") || !strings.Contains(d.Problem, "revision 13 still serves") {
		t.Errorf("problem = %q", d.Problem)
	}
	var details []string
	for _, e := range d.Evidence {
		details = append(details, e.Detail)
	}
	joined := strings.Join(details, "\n")
	if !strings.Contains(joined, "container app image: registry/payments:1.4 → registry/payments:1.5") {
		t.Errorf("the change must be quoted:\n%s", joined)
	}
	if !strings.Contains(joined, "container app env DB_PASSWORD changed") || strings.Contains(joined, "hunter2") {
		t.Errorf("an environment value must be named and never quoted:\n%s", joined)
	}
	for _, s := range d.Suggestions {
		if !strings.HasPrefix(s.Command, "kubectl rollout history") {
			t.Errorf("suggestions are read-only commands, got %q", s.Command)
		}
	}

	// The crash loop is the root cause and still leads.
	if first, _ := Primary(diagnose(t, in)); first.Rule != "pod.crashloop" {
		t.Errorf("the pod failure must lead, got %s", first.Rule)
	}
}

func TestARecentChangeIsStatedAsACoincidenceInTime(t *testing.T) {
	a := app(revisionPod("payments-new-1", "payments-new", false, waiting("CrashLoopBackOff", "")))
	a.Health = application.Down
	in := &Input{App: a, Context: application.Context{Revisions: []application.Revision{
		revision("payments-old", 13, "registry/payments:1.4", 0, 0, 48*time.Hour),
		revision("payments-new", 14, "registry/payments:1.5", 1, 0, 4*time.Minute),
	}}}

	d := find(t, diagnose(t, in), "workload.changed")
	if d.Confidence != Low || d.Severity != Info {
		t.Errorf("a coincidence is low confidence information, got %v/%v", d.Confidence, d.Severity)
	}
	if d.Problem != "Deployment/payments was changed 4m ago, in revision 14" {
		t.Errorf("problem = %q", d.Problem)
	}
	if d.Unknown == "" {
		t.Error("what the evidence cannot establish must be said")
	}
}

func TestAnOldChangeIsNotBlamed(t *testing.T) {
	a := app(revisionPod("payments-new-1", "payments-new", false, waiting("CrashLoopBackOff", "")))
	a.Health = application.Down
	in := &Input{App: a, Context: application.Context{Revisions: []application.Revision{
		revision("payments-old", 13, "registry/payments:1.4", 0, 0, 30*24*time.Hour),
		revision("payments-new", 14, "registry/payments:1.5", 1, 0, 9*24*time.Hour),
	}}}
	findings := diagnose(t, in)
	none(t, findings, "workload.changed")
	none(t, findings, "workload.revisionfailing")
}

func TestAHealthyApplicationHasNoChangeToExplain(t *testing.T) {
	a := app(revisionPod("payments-new-1", "payments-new", true))
	a.Health = application.Healthy
	in := &Input{App: a, Context: application.Context{Revisions: []application.Revision{
		revision("payments-old", 13, "registry/payments:1.4", 0, 0, time.Hour),
		revision("payments-new", 14, "registry/payments:1.5", 1, 1, time.Minute),
	}}}
	none(t, diagnose(t, in), "workload.changed")
}

func TestAStalledRolloutQuotesTheControllerAndFollowsThePods(t *testing.T) {
	a := app(pod("payments-1", waiting("ImagePullBackOff", "Back-off pulling image")))
	a.Workloads[0].Stalled = true
	a.Workloads[0].StalledReason = "ProgressDeadlineExceeded"
	a.Workloads[0].StalledMessage = `ReplicaSet "payments-7d8f" has timed out progressing.`

	findings := diagnose(t, &Input{App: a})
	d := find(t, findings, "workload.stalled")
	if d.Cause != a.Workloads[0].StalledMessage || d.Confidence != High {
		t.Errorf("the controller's own words, high confidence: %q %v", d.Cause, d.Confidence)
	}
	if !d.Consequence {
		t.Error("with pods failing, a stalled rollout is their consequence")
	}
	if findings[len(findings)-1].Rule != "workload.stalled" {
		t.Errorf("a consequence sorts after its causes, got %v", ruleNames(findings))
	}
}
