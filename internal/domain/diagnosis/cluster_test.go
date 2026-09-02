package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
)

func node(name string, ready bool, pressure ...string) application.Node {
	return application.Node{
		Meta:     application.Meta{Kind: "Node", Name: name, UID: name + "-uid"},
		Ready:    ready,
		Pressure: pressure,
	}
}

func TestANotReadyNodeExplainsThePodsOnIt(t *testing.T) {
	unready := node("node-1", false)
	unready.Reason, unready.Message = "KubeletNotReady", "container runtime is down"

	d := find(t, diagnose(t, &Input{
		App:     app(pod("payments-1"), pod("payments-2")),
		Context: application.Context{Nodes: []application.Node{unready}},
	}), "node.unhealthy")

	if d.Severity != Critical {
		t.Errorf("severity = %v, want critical", d.Severity)
	}
	if d.Cause != "container runtime is down" {
		t.Errorf("the node's own message is the cause, got %q", d.Cause)
	}
	if !strings.Contains(d.Problem, "2 pods") {
		t.Errorf("the problem must count the pods affected, got %q", d.Problem)
	}
}

func TestNodePressureIsAWarningAndCordonedIsInformation(t *testing.T) {
	pressured := find(t, diagnose(t, &Input{
		App:     app(pod("payments-1")),
		Context: application.Context{Nodes: []application.Node{node("node-1", true, "MemoryPressure")}},
	}), "node.unhealthy")
	if pressured.Severity != Warning {
		t.Errorf("pressure is a warning, got %v", pressured.Severity)
	}

	cordoned := node("node-1", true)
	cordoned.Unschedulable = true
	info := find(t, diagnose(t, &Input{
		App:     app(pod("payments-1")),
		Context: application.Context{Nodes: []application.Node{cordoned}},
	}), "node.unhealthy")
	if info.Severity != Info {
		t.Errorf("a cordoned node is not a fault, got %v", info.Severity)
	}
}

func TestAHealthyNodeProducesNothing(t *testing.T) {
	none(t, diagnose(t, &Input{
		App:     app(pod("payments-1")),
		Context: application.Context{Nodes: []application.Node{node("node-1", true)}},
	}), "node.unhealthy")
}

func TestAnUnboundClaimStopsTheApplication(t *testing.T) {
	p := pod("payments-1")
	p.Claims = []string{"payments-data"}
	p.Phase, p.Scheduled = "Pending", true

	in := &Input{
		App: app(p),
		Context: application.Context{
			Claims: []application.Claim{{
				Meta: application.Meta{
					Kind: "PersistentVolumeClaim", Name: "payments-data", Namespace: "shop", UID: "pvc-uid",
				},
				Phase:        "Pending",
				StorageClass: "fast",
			}},
			Events: []application.Event{{
				Type: "Warning", Reason: "ProvisioningFailed", Count: 4,
				Message:  `storageclass.storage.k8s.io "fast" not found`,
				LastSeen: now.Add(-3 * time.Minute),
				About:    application.ObjectRef{Kind: "PersistentVolumeClaim", Name: "payments-data", UID: "pvc-uid"},
			}},
		},
	}

	d := find(t, diagnose(t, in), "storage.unbound")
	if !strings.Contains(d.Cause, "not found") {
		t.Errorf("the provisioner's own complaint is the cause, got %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
	if d.Subject.Kind != "PersistentVolumeClaim" {
		t.Errorf("the finding is about the claim, got %+v", d.Subject)
	}
}

func TestABoundClaimIsNotAProblem(t *testing.T) {
	p := pod("payments-1")
	p.Claims = []string{"payments-data"}

	none(t, diagnose(t, &Input{
		App: app(p),
		Context: application.Context{Claims: []application.Claim{{
			Meta:  application.Meta{Kind: "PersistentVolumeClaim", Name: "payments-data", Namespace: "shop"},
			Phase: "Bound",
		}}},
	}), "storage.unbound")
}

func TestMissingReplicasAreReportedWhenNoPodExplainsThem(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Workloads[0].Desired, a.Workloads[0].Ready = 3, 1

	d := find(t, diagnose(t, &Input{App: a}), "workload.replicas")
	if !strings.Contains(d.Problem, "missing 2 of 3") {
		t.Errorf("problem = %q, want the missing count", d.Problem)
	}
	if d.Confidence != Low {
		t.Errorf("a rollout in progress looks the same; confidence = %v, want low", d.Confidence)
	}
}

func TestMissingReplicasQuoteTheControllerWhenItComplained(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Workloads[0].Desired, a.Workloads[0].Ready = 3, 1

	in := &Input{App: a, Context: application.Context{Events: []application.Event{{
		Type: "Warning", Reason: "FailedCreate", Count: 12,
		Message:  `pods "payments-7d8f-" is forbidden: exceeded quota: cpu`,
		LastSeen: now.Add(-time.Minute),
		About:    application.ObjectRef{Kind: "Deployment", Name: "payments", UID: "dep-uid"},
	}}}}

	d := find(t, diagnose(t, in), "workload.replicas")
	if !strings.Contains(d.Cause, "exceeded quota") {
		t.Errorf("the controller said exactly why; cause = %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
}

func TestBrokenPodsExplainThemselvesRatherThanTheReplicaCount(t *testing.T) {
	// Three pods exist and are crashing: "missing 3 of 3 pods" would be both
	// wrong and less useful than the crash loop.
	crashing := waiting("CrashLoopBackOff", "")
	crashing.LastExitCode = 1
	a := app(pod("payments-1", crashing), pod("payments-2", crashing), pod("payments-3", crashing))
	a.Workloads[0].Desired, a.Workloads[0].Ready = 3, 0

	findings := diagnose(t, &Input{App: a})
	none(t, findings, "workload.replicas")
	find(t, findings, "pod.crashloop")
}

func TestAPausedRolloutIsInformationNotAFault(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Workloads[0].Paused = true

	d := find(t, diagnose(t, &Input{App: a}), "workload.paused")
	if d.Severity != Info {
		t.Errorf("severity = %v, want info", d.Severity)
	}
	if !strings.Contains(d.Suggestions[0].Command, "rollout resume") {
		t.Errorf("the suggestion must say how to undo it, got %q", d.Suggestions[0].Command)
	}
}
