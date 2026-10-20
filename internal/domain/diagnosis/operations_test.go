package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

func hpa(conditions map[string]application.Condition, current, max int32) application.Autoscaler {
	return application.Autoscaler{
		Meta:       application.Meta{Kind: "HorizontalPodAutoscaler", Name: "payments", Namespace: "shop"},
		TargetKind: "Deployment", TargetName: "payments", Min: 2, Max: max, Current: current, Desired: current,
		Conditions: conditions,
	}
}

func TestAnAutoscalerThatCannotReadItsMetricsIsBlind(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Autoscalers: []application.Autoscaler{hpa(map[string]application.Condition{
		"ScalingActive": {Status: "False", Reason: "FailedGetResourceMetric", Message: "failed to get cpu utilization: missing request for cpu in container app"},
	}, 2, 10)}}}
	d := find(t, diagnose(t, in), "hpa.blind")
	if !strings.Contains(d.Cause, "missing request for cpu") || !strings.Contains(d.Suggestions[0].Text, "request") {
		t.Errorf("cause %q, first suggestion %q", d.Cause, d.Suggestions[0].Text)
	}
}

func TestAnAutoscalerAtItsCeilingIsCalledOut(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Autoscalers: []application.Autoscaler{hpa(map[string]application.Condition{
		"ScalingActive":  {Status: "True", Reason: "ValidMetricFound"},
		"ScalingLimited": {Status: "True", Reason: "TooManyReplicas", Message: "the desired replica count is more than the maximum replica count"},
	}, 10, 10)}}}
	if d := find(t, diagnose(t, in), "hpa.maxed"); !strings.Contains(d.Problem, "maximum of 10") {
		t.Errorf("problem %q", d.Problem)
	}
}

func TestAHealthyAutoscalerOrSomebodyElsesSaysNothing(t *testing.T) {
	healthy := hpa(map[string]application.Condition{"ScalingActive": {Status: "True"}, "ScalingLimited": {Status: "False"}}, 3, 10)
	other := hpa(map[string]application.Condition{"ScalingActive": {Status: "False", Message: "x"}}, 1, 10)
	other.TargetName = "someone-else"
	in := &Input{App: servingApp("8080"), Context: application.Context{Autoscalers: []application.Autoscaler{healthy, other}}}
	none(t, diagnose(t, in), "hpa.blind")
	none(t, diagnose(t, in), "hpa.maxed")
}

func budget(allowed, healthy, desired, expected int32) application.DisruptionBudget {
	return application.DisruptionBudget{
		Meta:        application.Meta{Kind: "PodDisruptionBudget", Name: "payments", Namespace: "shop"},
		MatchLabels: map[string]string{"app": "payments"}, DisruptionsAllowed: allowed,
		CurrentHealthy: healthy, DesiredHealthy: desired, ExpectedPods: expected,
	}
}

func TestABudgetThatAllowsNothingBlocksDrains(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Budgets: []application.DisruptionBudget{budget(0, 1, 1, 1)}}}
	d := find(t, diagnose(t, in), "pdb.blocking")
	if d.Severity != Info || !strings.Contains(d.Cause, "drain") {
		t.Errorf("%v: %q", d.Severity, d.Cause)
	}

	unhealthy := &Input{App: servingApp("8080"), Context: application.Context{Budgets: []application.DisruptionBudget{budget(0, 1, 2, 3)}}}
	if d := find(t, diagnose(t, unhealthy), "pdb.blocking"); d.Severity != Warning {
		t.Errorf("an unhealthy application behind a blocking budget is a warning; got %v", d.Severity)
	}
}

func TestABudgetWithRoomOrWithoutASelectorIsNotBlocking(t *testing.T) {
	room := &Input{App: servingApp("8080"), Context: application.Context{Budgets: []application.DisruptionBudget{budget(1, 3, 2, 3)}}}
	none(t, diagnose(t, room), "pdb.blocking")
	b := budget(0, 1, 1, 1)
	b.NoSelector, b.MatchLabels = true, nil
	empty := &Input{App: servingApp("8080"), Context: application.Context{Budgets: []application.DisruptionBudget{b}}}
	none(t, diagnose(t, empty), "pdb.blocking")
}

func TestAFailedJobSaysWhyItStopped(t *testing.T) {
	a := servingApp("8080")
	a.Workloads = append(a.Workloads, application.Workload{
		Meta:   application.Meta{Kind: "Job", Name: "migrate", Namespace: "shop"},
		Failed: true, FailedReason: "BackoffLimitExceeded", FailedMessage: "Job has reached the specified backoff limit",
	})
	d := find(t, diagnose(t, &Input{App: a}), "job.failed")
	if d.Severity != Critical || !strings.Contains(d.Cause, "backoffLimit") || !strings.Contains(d.Suggestions[0].Command, "job/migrate") {
		t.Errorf("%v %q %q", d.Severity, d.Cause, d.Suggestions[0].Command)
	}
}

func TestAPodStuckTerminatingNamesItsFinalizer(t *testing.T) {
	a := servingApp("8080")
	a.Pods[0].DeletingSince = now.Add(-20 * time.Minute)
	a.Pods[0].Grace = 30 * time.Second
	a.Pods[0].Finalizers = []string{"example.com/cleanup"}
	d := find(t, diagnose(t, &Input{App: a}), "pod.terminating")
	if d.Confidence != High || !strings.Contains(d.Cause, "example.com/cleanup") {
		t.Errorf("%v %q", d.Confidence, d.Cause)
	}
}

func TestAPodOnALostNodeIsExplainedByTheNode(t *testing.T) {
	a := servingApp("8080")
	a.Pods[0].DeletingSince = now.Add(-20 * time.Minute)
	in := &Input{App: a, Context: application.Context{Nodes: []application.Node{{Meta: application.Meta{Name: "node-1"}, Ready: false}}}}
	if d := find(t, diagnose(t, in), "pod.terminating"); !strings.Contains(d.Cause, "node node-1 is not ready") {
		t.Errorf("cause %q", d.Cause)
	}
}

func TestAPodWithinItsGracePeriodIsNotStuck(t *testing.T) {
	a := servingApp("8080")
	a.Pods[0].DeletingSince = now.Add(-2 * time.Minute)
	a.Pods[0].Grace = 30 * time.Second
	none(t, diagnose(t, &Input{App: a}), "pod.terminating")
}

func TestARateLimitedPullIsNotCalledACredentialProblem(t *testing.T) {
	c := waiting("ErrImagePull", `failed to pull image "nginx:1.27": toomanyrequests: You have reached your pull rate limit`)
	d := find(t, diagnose(t, &Input{App: app(pod("payments-1", c))}), "pod.imagepull")
	if !strings.Contains(d.Cause, "rate-limiting") {
		t.Errorf("cause %q", d.Cause)
	}
}
