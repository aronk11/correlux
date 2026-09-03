package application

import "testing"

func app(workloads []Workload, pods ...Pod) Application {
	a := Application{Name: "api", Namespace: "payments", Workloads: workloads, Pods: pods}
	a.evaluate()
	return a
}

func replicaSet(desired, ready int32) []Workload {
	return []Workload{{
		Meta:       Meta{Kind: "Deployment", Name: "api"},
		Desired:    desired,
		Ready:      ready,
		Replicated: true,
	}}
}

func pod(ready bool, phase, reason string, restarts int32) Pod {
	return Pod{Meta: Meta{Kind: "Pod", Name: "api-0"}, Phase: phase, Ready: ready, Reason: reason, Restarts: restarts}
}

func TestHealthFollowsTheReplicaCounts(t *testing.T) {
	cases := []struct {
		name    string
		app     Application
		want    Health
		summary string
	}{
		{
			name:    "every replica ready",
			app:     app(replicaSet(3, 3), pod(true, "Running", "", 0), pod(true, "Running", "", 0), pod(true, "Running", "", 0)),
			want:    Healthy,
			summary: "3 of 3 pods ready",
		},
		{
			name:    "one replica missing",
			app:     app(replicaSet(3, 2), pod(true, "Running", "", 0), pod(true, "Running", "", 0), pod(false, "Pending", "CrashLoopBackOff", 7)),
			want:    Degraded,
			summary: "2 of 3 pods ready",
		},
		{
			name:    "nothing ready",
			app:     app(replicaSet(3, 0), pod(false, "Pending", "ImagePullBackOff", 0)),
			want:    Down,
			summary: "0 of 3 pods ready",
		},
		{
			name:    "scaled to zero on purpose",
			app:     app(replicaSet(0, 0)),
			want:    Unknown,
			summary: "scaled to zero",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.app.Health != tc.want {
				t.Errorf("health = %v, want %v", tc.app.Health, tc.want)
			}
			if tc.app.Summary != tc.summary {
				t.Errorf("summary = %q, want %q", tc.app.Summary, tc.summary)
			}
		})
	}
}

func TestAWorkloadThatLooksCompleteButHasAnUnreadyPodSaysSo(t *testing.T) {
	// The Deployment's status claims both replicas are ready; one pod says
	// otherwise. Reporting "2 of 2 pods ready" next to a degraded badge would
	// look like Correlux contradicting itself.
	a := app(replicaSet(2, 2), pod(true, "Running", "", 0), pod(false, "Running", "", 0))

	if a.Health != Degraded {
		t.Errorf("a pod that is not ready is a degraded application, got %v", a.Health)
	}
	if a.Summary != "2 of 2 pods ready, 1 pod not ready" {
		t.Errorf("summary = %q, want the contradiction stated", a.Summary)
	}
}

func TestAPausedRolloutIsNotedButNotAFault(t *testing.T) {
	workloads := replicaSet(2, 2)
	workloads[0].Paused = true
	a := app(workloads, pod(true, "Running", "", 0), pod(true, "Running", "", 0))

	if a.Health != Healthy {
		t.Errorf("a paused rollout whose replicas are all ready is still healthy, got %v", a.Health)
	}
	if a.Summary != "2 of 2 pods ready, rollout paused" {
		t.Errorf("the pause must be stated, got %q", a.Summary)
	}
}

func TestCompletedJobPodsAreNotCountedAsUnready(t *testing.T) {
	a := app([]Workload{{Meta: Meta{Kind: "CronJob", Name: "billing"}}},
		pod(false, "Succeeded", "", 0))

	if a.Health == Degraded || a.Health == Down {
		t.Errorf("a finished job is not a broken application, got %v", a.Health)
	}
}

func TestACronJobBetweenRunsIsNotScaledToZero(t *testing.T) {
	a := app([]Workload{{Meta: Meta{Kind: "CronJob", Name: "billing"}}})

	if a.Summary != "no pods running" {
		t.Errorf("a CronJob has no replicas to be scaled away, got %q", a.Summary)
	}
}

func TestProblemsAreRankedByHowManyPodsAreInThem(t *testing.T) {
	a := app(replicaSet(4, 1),
		pod(true, "Running", "", 0),
		pod(false, "Pending", "CrashLoopBackOff", 12),
		pod(false, "Pending", "CrashLoopBackOff", 9),
		pod(false, "Pending", "Unschedulable", 0),
	)

	if got := a.ProblemSummary(); got != "2 CrashLoopBackOff, 1 Unschedulable" {
		t.Errorf("problems must be ranked, got %q", got)
	}
	if a.Restarts != 21 {
		t.Errorf("restarts are summed across the application, got %d", a.Restarts)
	}
}

func TestAnEmptyApplicationSaysSoRatherThanClaimingHealth(t *testing.T) {
	a := app(nil)
	if a.Health != Unknown || a.Summary != "nothing running" {
		t.Errorf("an application with no workloads and no pods is unknown, got %v/%q", a.Health, a.Summary)
	}
}

func TestSummariseCountsByHealth(t *testing.T) {
	counts := Summarise([]Application{
		{Health: Healthy}, {Health: Healthy}, {Health: Degraded}, {Health: Down}, {Health: Unknown},
	})
	if counts != (Counts{Total: 5, Healthy: 2, Degraded: 1, Down: 1, Unknown: 1}) {
		t.Errorf("unexpected counts: %+v", counts)
	}
}
