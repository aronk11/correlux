package workloads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestTheEvidencePassReadsTheEcosystemAndSkipsWhatIsNotInstalled(t *testing.T) {
	var secretsAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/apis/kustomize.toolkit.fluxcd.io/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
		case r.URL.Path == "/apis/cert-manager.io/v1/namespaces/shop/certificates":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"payments-tls","namespace":"shop"},
				"spec":{"secretName":"payments-tls","dnsNames":["pay.example.com"],"issuerRef":{"kind":"ClusterIssuer","name":"le"}},
				"status":{"notAfter":"2026-11-01T00:00:00Z","conditions":[{"type":"Ready","status":"False","reason":"Failed","message":"challenge failed"}]}}]}`))
		case r.URL.Path == "/apis/helm.toolkit.fluxcd.io/v2/namespaces/flux-system/helmreleases":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"payments","namespace":"flux-system"},"spec":{"suspend":true},
				"status":{"lastAppliedRevision":"1.4.0","conditions":[{"type":"Ready","status":"True"}]}}]}`))
		case r.URL.Path == "/api/v1/namespaces/shop/secrets":
			secretsAccept = r.Header.Get("Accept") + " " + r.URL.Query().Get("labelSelector")
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"sh.helm.release.v1.payments.v6","namespace":"shop","labels":{"name":"payments","status":"superseded","version":"6"}}},
				{"metadata":{"name":"sh.helm.release.v1.payments.v7","namespace":"shop","labels":{"name":"payments","status":"pending-upgrade","version":"7"}}}]}`))
		case r.URL.Path == "/apis/networking.k8s.io/v1/namespaces/shop/networkpolicies":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"default-deny","namespace":"shop"},"spec":{"podSelector":{},"policyTypes":["Ingress"]}}]}`))
		default:
			_, _ = w.Write([]byte(`{"items":[]}`))
		}
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	out, err := CollectContext(context.Background(), cs, Options{Namespace: "shop", GitOpsNamespaces: []string{"flux-system"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Gaps) != 0 {
		t.Errorf("an API that is not installed is not a gap: %+v", out.Gaps)
	}
	if len(out.Certificates) != 1 || out.Certificates[0].Ready || out.Certificates[0].Message != "challenge failed" || out.Certificates[0].Issuer != "ClusterIssuer/le" {
		t.Errorf("certificates = %+v", out.Certificates)
	}
	if len(out.GitOps) != 1 || out.GitOps[0].Kind != "HelmRelease" || !out.GitOps[0].Suspended || out.GitOps[0].Revision != "1.4.0" {
		t.Errorf("gitops = %+v", out.GitOps)
	}
	if len(out.Releases) != 1 || out.Releases[0].Revision != 7 || out.Releases[0].Status != "pending-upgrade" {
		t.Errorf("only the newest revision counts: %+v", out.Releases)
	}
	if !strings.Contains(secretsAccept, "PartialObjectMetadataList") || !strings.Contains(secretsAccept, "owner=helm") {
		t.Errorf("Secrets must be read as metadata only, got %q", secretsAccept)
	}
	if len(out.Policies) != 1 || !out.Policies[0].Ingress || out.Policies[0].IngressRules != 0 {
		t.Errorf("policies = %+v", out.Policies)
	}
}
