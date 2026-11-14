package diagnosis

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
)

func dependent(health application.Health) (application.Application, []application.Application) {
	p := pod("payments-1")
	p.Ready = health == application.Healthy
	a := app(p)
	a.Health = health
	a.DependsOn = []application.Dependency{{App: "shop/postgres", Name: "postgres", Namespace: "shop",
		Service: "postgres", Via: "env DATABASE_URL"}}
	db := application.Application{Name: "postgres", Namespace: "shop", Health: application.Down,
		Summary: "0 of 1 pods ready", Problems: []application.Problem{{Reason: "CrashLoopBackOff", Count: 1}}}
	return a, []application.Application{a, db}
}

func TestAFailingApplicationNamesTheFailingThingItPointsAt(t *testing.T) {
	a, apps := dependent(application.Degraded)
	d := find(t, diagnose(t, &Input{App: a, Apps: apps}), DependencyRule)
	if d.Severity != Warning || d.Confidence != Medium {
		t.Errorf("both failing and one names the other: a warning at medium confidence, got %v/%v", d.Severity, d.Confidence)
	}
	if !strings.Contains(d.Problem, "depends on postgres, which is down") {
		t.Errorf("problem = %q", d.Problem)
	}
	if !strings.Contains(d.Cause, "0 of 1 pods ready") || !strings.Contains(d.Cause, "CrashLoopBackOff") {
		t.Errorf("the cause is the other application's own state: %q", d.Cause)
	}
	if d.Unknown == "" {
		t.Error("configuration is not traffic, and the finding must say so")
	}
}

func TestAHealthyApplicationIsOnlyToldItsDependencyIsDown(t *testing.T) {
	a, apps := dependent(application.Healthy)
	d := find(t, diagnose(t, &Input{App: a, Apps: apps}), DependencyRule)
	if d.Severity != Info {
		t.Errorf("an application that still serves is not degraded by somebody else's outage, got %v", d.Severity)
	}
}

func TestAHealthyDependencyIsNotAFinding(t *testing.T) {
	a, apps := dependent(application.Degraded)
	apps[1].Health = application.Healthy
	none(t, diagnose(t, &Input{App: a, Apps: apps}), DependencyRule)
}

func zoned(name, zone string) application.Node {
	n := node(name, true)
	n.Labels = map[string]string{"topology.kubernetes.io/zone": zone}
	return n
}

func placed(nodes ...string) application.Application {
	pods := make([]application.Pod, 0, len(nodes))
	for i, n := range nodes {
		p := pod("payments-" + string(rune('a'+i)))
		p.Ready, p.Node = true, n
		pods = append(pods, p)
	}
	return app(pods...)
}

func TestReplicasOnOneNodeAreCalledOut(t *testing.T) {
	nodes := []application.Node{zoned("node-1", "a"), zoned("node-2", "b")}
	d := find(t, diagnose(t, &Input{App: placed("node-1", "node-1", "node-1"),
		Context: application.Context{Nodes: nodes}}), "placement.concentrated")
	if d.Severity != Info || !strings.Contains(d.Problem, "all 3 pods") || !strings.Contains(d.Problem, "on node node-1") {
		t.Errorf("got %v %q", d.Severity, d.Problem)
	}
	if !strings.Contains(d.Cause, "no topology spread constraint") {
		t.Errorf("cause = %q", d.Cause)
	}
}

func TestReplicasInOneZoneAreCalledOutWhenThereIsAnotherZone(t *testing.T) {
	nodes := []application.Node{zoned("node-1", "a"), zoned("node-2", "a"), zoned("node-3", "b")}
	a := placed("node-1", "node-2")
	a.Pods[0].Spread, a.Pods[1].Spread = true, true
	d := find(t, diagnose(t, &Input{App: a, Context: application.Context{Nodes: nodes}}), "placement.concentrated")
	if !strings.Contains(d.Problem, "in zone a") || !strings.Contains(d.Cause, "placed them together anyway") {
		t.Errorf("problem %q cause %q", d.Problem, d.Cause)
	}
}

func TestPlacementStaysQuietWhereThereWasNoChoice(t *testing.T) {
	oneZone := []application.Node{zoned("node-1", "a"), zoned("node-2", "a")}
	none(t, diagnose(t, &Input{App: placed("node-1", "node-2"), Context: application.Context{Nodes: oneZone}}), "placement.concentrated")

	oneNode := []application.Node{zoned("node-1", "a")}
	none(t, diagnose(t, &Input{App: placed("node-1", "node-1"), Context: application.Context{Nodes: oneNode}}), "placement.concentrated")

	spread := []application.Node{zoned("node-1", "a"), zoned("node-2", "b")}
	none(t, diagnose(t, &Input{App: placed("node-1", "node-2"), Context: application.Context{Nodes: spread}}), "placement.concentrated")
}

func argoApp(g *application.GitOpsObject) *Input {
	p := pod("payments-1")
	p.Ready = true
	a := app(p)
	a.Manager = application.Manager{Tool: "Argo CD", Kind: "Application", Name: "shop"}
	g.Meta = application.Meta{Kind: "Application", Name: "shop", Namespace: "argocd", UID: "argo-uid"}
	return &Input{App: a, Context: application.Context{GitOps: []application.GitOpsObject{*g}}}
}

func TestAnArgoCDSyncThatFailedIsQuoted(t *testing.T) {
	d := find(t, diagnose(t, argoApp(&application.GitOpsObject{Sync: "OutOfSync", Health: "Healthy",
		Operation: "Failed", OperationMessage: "one or more objects failed to apply: Deployment.apps \"api\" is invalid"})), "gitops.syncfailed")
	if d.Severity != Warning || !strings.Contains(d.Cause, "failed to apply") {
		t.Errorf("got %v %q", d.Severity, d.Cause)
	}
}

func TestAnArgoCDApplicationOutOfSyncSaysWhetherItWillFixItself(t *testing.T) {
	manual := find(t, diagnose(t, argoApp(&application.GitOpsObject{Sync: "OutOfSync", Health: "Healthy"})), "gitops.outofsync")
	if manual.Severity != Info || !strings.Contains(manual.Cause, "automated sync is off") {
		t.Errorf("got %v %q", manual.Severity, manual.Cause)
	}
	auto := find(t, diagnose(t, argoApp(&application.GitOpsObject{Sync: "OutOfSync", Health: "Healthy", Automated: true})), "gitops.outofsync")
	if !strings.Contains(auto.Cause, "automated sync is on") {
		t.Errorf("cause = %q", auto.Cause)
	}
}

func TestAnArgoCDComparisonErrorMeansGitIsNotBeingApplied(t *testing.T) {
	d := find(t, diagnose(t, argoApp(&application.GitOpsObject{Sync: "Unknown", Health: "Healthy",
		Errors: []string{"ComparisonError: failed to generate manifests: helm template failed"}})), "gitops.notready")
	if !strings.Contains(d.Cause, "helm template failed") {
		t.Errorf("cause = %q", d.Cause)
	}
}

func TestAHealthyArgoCDApplicationIsNotAFinding(t *testing.T) {
	findings := diagnose(t, argoApp(&application.GitOpsObject{Sync: "Synced", Health: "Degraded", Operation: "Succeeded"}))
	for _, rule := range []string{"gitops.syncfailed", "gitops.outofsync", "gitops.notready", "gitops.missing"} {
		none(t, findings, rule)
	}
}

func TestARouteItsGatewayRefusedIsTheFirstThingToRead(t *testing.T) {
	p := pod("payments-1")
	p.Ready = true
	a := app(p)
	a.Ingresses = []application.Ingress{{
		Meta:     application.Meta{Kind: "HTTPRoute", Name: "shop", Namespace: "shop"},
		Gateways: []string{"public"},
		Rejected: []string{"Gateway/public: Accepted=False (NotAllowedByListeners) — no listener allows routes from namespace shop"},
	}}
	d := find(t, diagnose(t, &Input{App: a}), "route.rejected")
	if !strings.Contains(d.Problem, "HTTPRoute/shop") || !strings.Contains(d.Cause, "NotAllowedByListeners") {
		t.Errorf("problem %q cause %q", d.Problem, d.Cause)
	}

	a.Ingresses[0].Rejected = nil
	none(t, diagnose(t, &Input{App: a}), "route.rejected")
}
