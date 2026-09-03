package diagnosis

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
)

func service(name string, selector map[string]string) application.Service {
	return application.Service{
		Meta: application.Meta{
			Kind: "Service", Name: name, Namespace: "shop", UID: name + "-svc-uid",
		},
		Type:     "ClusterIP",
		Selector: selector,
	}
}

func endpoints(service string, ready, notReady int) application.EndpointSet {
	return application.EndpointSet{Service: service, Namespace: "shop", Ready: ready, NotReady: notReady}
}

func TestASelectorThatMatchesNothingIsNamedAsSuch(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Services = []application.Service{service("payments", map[string]string{"app": "payment"})}

	d := find(t, diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 0, 0)}},
	}), "service.noendpoints")

	if !strings.Contains(d.Cause, "matches no pods") {
		t.Errorf("a label typo is the likeliest cause and must be named, got %q", d.Cause)
	}
	if !strings.Contains(d.Cause, "app=payment") {
		t.Errorf("the selector itself must be printed, got %q", d.Cause)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
}

func TestUnreadyPodsBehindAServiceAreNamedAsSuch(t *testing.T) {
	a := app(pod("payments-1"), pod("payments-2"))
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}

	d := find(t, diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 0, 2)}},
	}), "service.noendpoints")

	if !strings.Contains(d.Cause, "not ready") {
		t.Errorf("cause = %q, want the readiness of the pods behind it", d.Cause)
	}
}

func TestReadinessRootCauseSortsBeforeItsServiceConsequence(t *testing.T) {
	p := pod("payments-1", application.Container{Name: "app", State: "running"})
	a := app(p)
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}
	in := &Input{
		App: a,
		Context: application.Context{
			Endpoints: []application.EndpointSet{endpoints("payments", 0, 1)},
			Events: []application.Event{{
				Type: "Warning", Reason: "Unhealthy", Message: "Readiness probe failed: HTTP 404",
				About: application.ObjectRef{Kind: "Pod", Name: p.Name, UID: p.UID},
			}},
		},
	}

	findings := diagnose(t, in)
	if len(findings) < 2 || findings[0].Rule != "pod.notready" || findings[1].Rule != "service.noendpoints" {
		t.Errorf("root cause must precede its service impact, got %v", ruleNames(findings))
	}
}

func TestReadyPodsWithNoPublishedAddressSayWhatIsUnknown(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}

	d := find(t, diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 0, 0)}},
	}), "service.noendpoints")

	if d.Confidence != Low {
		t.Errorf("confidence = %v, want low", d.Confidence)
	}
	if d.Unknown == "" {
		t.Error("the rule cannot explain a ready pod with no endpoint and must say so in Unknown")
	}
}

func TestAServiceWithReadyEndpointsIsNotReported(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}

	none(t, diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 1, 0)}},
	}), "service.noendpoints")
}

func TestAServiceWithoutASelectorIsLeftAlone(t *testing.T) {
	// ExternalName services and manually managed endpoints have no selector,
	// and "no endpoints" says nothing about them.
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Services = []application.Service{service("payments", nil)}

	none(t, diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 0, 0)}},
	}), "service.noendpoints")
}

func TestEndpointsThatCouldNotBeReadProduceNoAccusation(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}

	none(t, diagnose(t, &Input{
		App: a,
		Context: application.Context{
			Gaps: []application.Gap{{Kind: "EndpointSlice", Reason: "not permitted for this user"}},
		},
	}), "service.noendpoints")
}

func TestAnIngressPointingAtNothingIsReported(t *testing.T) {
	a := app(pod("payments-1"))
	a.Pods[0].Ready = true
	a.Ingresses = []application.Ingress{{
		Meta:     application.Meta{Kind: "Ingress", Name: "payments", Namespace: "shop", UID: "ing-uid"},
		Hosts:    []string{"payments.example.com"},
		Backends: []string{"payments-v2"},
	}}
	scope := application.Snapshot{Services: []application.Service{service("payments", nil)}}

	d := find(t, diagnose(t, &Input{App: a, Scope: scope}), "ingress.nobackend")
	if !strings.Contains(d.Problem, "payments-v2") {
		t.Errorf("the missing backend must be named, got %q", d.Problem)
	}
	if d.Confidence != High {
		t.Errorf("confidence = %v, want high", d.Confidence)
	}
}

func TestAnIngressWithItsBackendPresentIsNotReported(t *testing.T) {
	a := app(pod("payments-1"))
	a.Ingresses = []application.Ingress{{
		Meta:     application.Meta{Kind: "Ingress", Name: "payments", Namespace: "shop"},
		Backends: []string{"payments"},
	}}
	scope := application.Snapshot{Services: []application.Service{service("payments", nil)}}

	none(t, diagnose(t, &Input{App: a, Scope: scope}), "ingress.nobackend")
}

func TestWithoutTheScopeAnIngressIsNotAccused(t *testing.T) {
	// The backend may well exist; Correlux simply has not looked. Silence beats
	// a confident lie.
	a := app(pod("payments-1"))
	a.Ingresses = []application.Ingress{{
		Meta:     application.Meta{Kind: "Ingress", Name: "payments", Namespace: "shop"},
		Backends: []string{"payments"},
	}}

	none(t, diagnose(t, &Input{App: a}), "ingress.nobackend")
}

func TestFindingsAreRankedWorstAndSurestFirst(t *testing.T) {
	crashing := waiting("CrashLoopBackOff", "")
	crashing.LastExitCode = 1
	a := app(pod("payments-1", crashing))
	a.Workloads[0].Paused = true

	findings := diagnose(t, &Input{App: a})
	if len(findings) < 2 {
		t.Fatalf("expected several findings, got %v", ruleNames(findings))
	}
	if findings[0].Rule != "pod.crashloop" {
		t.Errorf("the critical finding must lead, got %v", ruleNames(findings))
	}
	primary, ok := Primary(findings)
	if !ok || primary.Rule != findings[0].Rule {
		t.Error("Primary must be the finding the dashboard shows")
	}
}

func TestAHealthyApplicationHasNothingToExplain(t *testing.T) {
	a := app(pod("payments-1"), pod("payments-2"))
	for i := range a.Pods {
		a.Pods[i].Ready = true
		a.Pods[i].Containers = []application.Container{{Name: "app", State: "running", Ready: true}}
	}
	a.Workloads[0].Ready = 2
	a.Services = []application.Service{service("payments", map[string]string{"app": "payments"})}

	findings := diagnose(t, &Input{
		App:     a,
		Context: application.Context{Endpoints: []application.EndpointSet{endpoints("payments", 2, 0)}},
	})
	if len(findings) != 0 {
		t.Errorf("a healthy application must produce no findings, got %v", ruleNames(findings))
	}
}
