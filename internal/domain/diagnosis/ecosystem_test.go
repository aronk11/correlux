package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

func servingApp(target string, declared ...application.ContainerPort) application.Application {
	c := application.Container{Name: "app", State: "running", Ready: true, Ports: declared}
	p := pod("payments-1", c)
	p.Ready = true
	a := app(p)
	a.Services = []application.Service{{
		Meta:     application.Meta{Kind: "Service", Name: "payments", Namespace: "shop", UID: "svc-uid"},
		Selector: map[string]string{"app": "payments"},
		Targets:  []application.ServicePort{{Name: "http", Port: 80, TargetPort: target, Protocol: "TCP"}},
	}}
	return a
}

func TestANamedTargetPortNoContainerDeclaresIsCritical(t *testing.T) {
	a := servingApp("web", application.ContainerPort{Name: "http", Port: 8080})
	d := find(t, diagnose(t, &Input{App: a}), "traffic.targetport")

	if d.Severity != Critical || d.Confidence != High {
		t.Errorf("severity/confidence = %v/%v", d.Severity, d.Confidence)
	}
	if !strings.Contains(d.Cause, `named "web"`) {
		t.Errorf("cause = %q", d.Cause)
	}
}

func TestANumericTargetPortTheContainersDoNotDeclareIsAWarning(t *testing.T) {
	a := servingApp("8080", application.ContainerPort{Name: "http", Port: 3000})
	d := find(t, diagnose(t, &Input{App: a}), "traffic.targetport")

	if d.Severity != Warning || d.Confidence != Medium || d.Unknown == "" {
		t.Errorf("a process may listen on an undeclared port; got %v/%v unknown=%q", d.Severity, d.Confidence, d.Unknown)
	}
	if !strings.Contains(d.Cause, "declare 3000") {
		t.Errorf("cause = %q", d.Cause)
	}
}

func TestMatchingAndUndeclaredPortsAreNotAccused(t *testing.T) {
	none(t, diagnose(t, &Input{App: servingApp("http", application.ContainerPort{Name: "http", Port: 8080})}), "traffic.targetport")
	none(t, diagnose(t, &Input{App: servingApp("8080", application.ContainerPort{Port: 8080})}), "traffic.targetport")
	// Containers that declare nothing say nothing about where they listen.
	none(t, diagnose(t, &Input{App: servingApp("8080")}), "traffic.targetport")
}

func policy(name string, labels map[string]string, ingressRules, egressRules int, ingress, egress bool) application.NetworkPolicy {
	return application.NetworkPolicy{
		Meta:        application.Meta{Kind: "NetworkPolicy", Name: name, Namespace: "shop"},
		MatchLabels: labels, Ingress: ingress, IngressRules: ingressRules, Egress: egress, EgressRules: egressRules,
	}
}

func TestADenyAllPolicyIsolatesTheApplication(t *testing.T) {
	a := servingApp("8080")
	in := &Input{App: a, Context: application.Context{Policies: []application.NetworkPolicy{
		policy("default-deny", map[string]string{}, 0, 0, true, false),
	}}}
	d := find(t, diagnose(t, in), "traffic.ingressdenied")

	if d.Severity != Warning || !strings.Contains(d.Cause, "Service/payments") {
		t.Errorf("a Service nobody can get through is degraded, not down; got %v: %q", d.Severity, d.Cause)
	}
	if !strings.Contains(d.Cause, "NetworkPolicy/default-deny") || !strings.Contains(d.Unknown, "enforces") {
		t.Errorf("cause = %q, unknown = %q", d.Cause, d.Unknown)
	}
}

func TestADenyAllInFrontOfAnIngressIsCritical(t *testing.T) {
	a := servingApp("8080")
	a.Ingresses = []application.Ingress{{Meta: application.Meta{Kind: "Ingress", Name: "payments", Namespace: "shop"}, Backends: []string{"payments"}}}
	in := &Input{App: a, Context: application.Context{Policies: []application.NetworkPolicy{
		policy("default-deny", map[string]string{}, 0, 0, true, false),
	}}}
	if d := find(t, diagnose(t, in), "traffic.ingressdenied"); d.Severity != Critical {
		t.Errorf("users cut off by policy is critical; got %v", d.Severity)
	}
}

func TestADenyAllOnAClientThatTakesNoConnectionsIsOnlyANote(t *testing.T) {
	// A queue worker or an operator: default-deny is the hardening it should
	// have, and the pods are fine.
	a := servingApp("8080")
	a.Services = nil
	in := &Input{App: a, Context: application.Context{Policies: []application.NetworkPolicy{
		policy("default-deny-all", map[string]string{}, 0, 0, true, false),
	}}}
	d := find(t, diagnose(t, in), "traffic.ingressdenied")
	if d.Severity != Info || !strings.Contains(d.Cause, "deliberate") {
		t.Errorf("isolating a client is not a fault; got %v: %q", d.Severity, d.Cause)
	}
}

func TestAnAllowPolicyBesideADenyAllIsNotIsolation(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Policies: []application.NetworkPolicy{
		policy("default-deny", map[string]string{}, 0, 0, true, false),
		policy("allow-ingress-controller", map[string]string{"app": "payments"}, 1, 0, true, false),
	}}}
	none(t, diagnose(t, in), "traffic.ingressdenied")
}

func TestAnUnreadableSelectorKeepsTheRuleQuiet(t *testing.T) {
	p := policy("by-expression", nil, 0, 0, true, false)
	p.HasExpressions = true
	in := &Input{App: servingApp("8080"), Context: application.Context{Policies: []application.NetworkPolicy{p}}}
	none(t, diagnose(t, in), "traffic.ingressdenied")
}

func TestAnEgressDenyAllSaysDNSIsGoneToo(t *testing.T) {
	in := &Input{App: servingApp("8080"), Context: application.Context{Policies: []application.NetworkPolicy{
		policy("lockdown", map[string]string{"app": "payments"}, 0, 0, false, true),
	}}}
	d := find(t, diagnose(t, in), "traffic.egressdenied")
	if !strings.Contains(d.Cause, "DNS") {
		t.Errorf("cause = %q", d.Cause)
	}
}

func tlsApp() application.Application {
	a := servingApp("8080")
	a.Ingresses = []application.Ingress{{
		Meta:       application.Meta{Kind: "Ingress", Name: "payments", Namespace: "shop"},
		Hosts:      []string{"pay.example.com"},
		Backends:   []string{"payments"},
		TLSSecrets: []string{"payments-tls"},
	}}
	return a
}

func cert(ready bool, notAfter time.Time, message string) application.Certificate {
	return application.Certificate{
		Meta:       application.Meta{Kind: "Certificate", Name: "payments-tls", Namespace: "shop", UID: "cert-uid"},
		SecretName: "payments-tls", DNSNames: []string{"pay.example.com"}, Issuer: "ClusterIssuer/letsencrypt",
		ReadyKnown: true, Ready: ready, Reason: "Failed", Message: message, NotAfter: notAfter,
	}
}

func TestACertificateThatCannotBeIssuedBreaksHTTPS(t *testing.T) {
	in := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{
		cert(false, time.Time{}, "Issuing certificate as Secret does not exist"),
	}}}
	d := find(t, diagnose(t, in), "cert.notready")
	if d.Severity != Critical || !strings.Contains(d.Problem, "HTTPS") || !strings.Contains(d.Cause, "Secret does not exist") {
		t.Errorf("%v: %q / %q", d.Severity, d.Problem, d.Cause)
	}
}

func TestAFailingRenewalWithAValidCertificateIsAWarning(t *testing.T) {
	in := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{
		cert(false, now.Add(20*24*time.Hour), "ACME challenge failed"),
	}}}
	d := find(t, diagnose(t, in), "cert.notready")
	if d.Severity != Warning || !strings.Contains(d.Cause, "still valid for 20 days") {
		t.Errorf("%v: %q", d.Severity, d.Cause)
	}
}

func TestExpiryIsCalledOut(t *testing.T) {
	soon := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{cert(true, now.Add(5*24*time.Hour), "")}}}
	if d := find(t, diagnose(t, soon), "cert.expiring"); !strings.Contains(d.Problem, "5 days") {
		t.Errorf("problem = %q", d.Problem)
	}
	gone := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{cert(true, now.Add(-time.Hour), "")}}}
	if d := find(t, diagnose(t, gone), "cert.expired"); d.Severity != Critical {
		t.Errorf("severity = %v", d.Severity)
	}
	fine := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{cert(true, now.Add(60*24*time.Hour), "")}}}
	for _, r := range []string{"cert.notready", "cert.expiring", "cert.expired"} {
		none(t, diagnose(t, fine), r)
	}
}

func TestACertificateNoIngressUsesIsNotThisApplications(t *testing.T) {
	c := cert(false, time.Time{}, "nope")
	c.SecretName = "someone-else"
	in := &Input{App: tlsApp(), Context: application.Context{Certificates: []application.Certificate{c}}}
	none(t, diagnose(t, in), "cert.notready")
}

func fluxApp() application.Application {
	a := servingApp("8080")
	a.Manager = application.Manager{Tool: "Flux", Kind: "Kustomization", Name: "apps", Namespace: "flux-system"}
	return a
}

func TestAFailingKustomizationIsQuoted(t *testing.T) {
	in := &Input{App: fluxApp(), Context: application.Context{GitOps: []application.GitOpsObject{{
		Meta:       application.Meta{Kind: "Kustomization", Name: "apps", Namespace: "flux-system"},
		ReadyKnown: true, Ready: false, Reason: "BuildFailed",
		Message: "kustomize build failed: accumulating resources: missing file deployment.yaml",
	}}}}
	d := find(t, diagnose(t, in), "gitops.notready")
	if !strings.Contains(d.Cause, "missing file deployment.yaml") || d.Chain[len(d.Chain)-1] != "BuildFailed" {
		t.Errorf("cause = %q, chain = %v", d.Cause, d.Chain)
	}
}

func TestASuspendedKustomizationIsInformation(t *testing.T) {
	in := &Input{App: fluxApp(), Context: application.Context{GitOps: []application.GitOpsObject{{
		Meta: application.Meta{Kind: "Kustomization", Name: "apps", Namespace: "flux-system"}, Suspended: true, ReadyKnown: true, Ready: true,
	}}}}
	if d := find(t, diagnose(t, in), "gitops.suspended"); d.Severity != Info {
		t.Errorf("severity = %v", d.Severity)
	}
}

func helmApp() application.Application {
	a := servingApp("8080")
	a.Manager = application.Manager{Tool: "Helm", Name: "payments", Namespace: "shop"}
	return a
}

func TestAStuckHelmReleaseNamesTheLock(t *testing.T) {
	in := &Input{App: helmApp(), Context: application.Context{Releases: []application.HelmRelease{{
		Name: "payments", Namespace: "shop", Revision: 7, Status: "pending-upgrade", UpdatedAt: now.Add(-time.Hour),
	}}}}
	d := find(t, diagnose(t, in), "helm.stuck")
	if !strings.Contains(d.Cause, "another operation is in progress") || !strings.HasPrefix(d.Suggestions[0].Command, "helm history payments -n shop") {
		t.Errorf("cause = %q, suggestion = %q", d.Cause, d.Suggestions[0].Command)
	}
}

func TestAnUpgradeInProgressIsNotStuck(t *testing.T) {
	in := &Input{App: helmApp(), Context: application.Context{Releases: []application.HelmRelease{{
		Name: "payments", Namespace: "shop", Revision: 7, Status: "pending-upgrade", UpdatedAt: now.Add(-2 * time.Minute),
	}}}}
	none(t, diagnose(t, in), "helm.stuck")
}

func TestAFailedHelmReleaseIsReported(t *testing.T) {
	in := &Input{App: helmApp(), Context: application.Context{Releases: []application.HelmRelease{{
		Name: "payments", Namespace: "shop", Revision: 3, Status: "failed",
	}}}}
	if d := find(t, diagnose(t, in), "helm.failed"); !strings.Contains(d.Problem, "revision 3 failed") {
		t.Errorf("problem = %q", d.Problem)
	}
	ok := &Input{App: helmApp(), Context: application.Context{Releases: []application.HelmRelease{{Name: "payments", Namespace: "shop", Revision: 4, Status: "deployed"}}}}
	none(t, diagnose(t, ok), "helm.failed")
}
