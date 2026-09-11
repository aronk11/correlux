package describe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFluxLinksPreserveNamespaceAndAPIGroup(t *testing.T) {
	raw := []byte(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"namespace":"apps"},"spec":{"chart":{"spec":{"sourceRef":{"kind":"HelmRepository","name":"charts","namespace":"sources"}}},"valuesFrom":[{"kind":"Secret","name":"settings"}],"dependsOn":[{"name":"database","namespace":"db"}]},"status":{"helmChart":"sources/generated"}}`)
	links := Links("HelmRelease", raw)
	expected := map[string]string{"charts": "sources|helmrepositories.source.toolkit.fluxcd.io", "settings": "apps|secrets", "database": "db|helmreleases.helm.toolkit.fluxcd.io", "generated": "sources|helmcharts.source.toolkit.fluxcd.io"}
	for _, link := range links {
		if want, ok := expected[link.Name]; ok {
			if link.Namespace+"|"+link.Resource != want {
				t.Fatalf("wrong target: %+v", link)
			}
			delete(expected, link.Name)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing targets: %v", expected)
	}
	if got := Links("HelmRelease", []byte(strings.ReplaceAll(string(raw), "helm.toolkit.fluxcd.io/v2", "other.example/v1"))); len(got) != 0 {
		t.Fatal("treated unrelated CRD as Flux")
	}
}

func TestRemoteFluxInventoryDoesNotNavigateLocalCluster(t *testing.T) {
	raw := []byte(`{"apiVersion":"kustomize.toolkit.fluxcd.io/v1","kind":"Kustomization","metadata":{"namespace":"flux-system"},"spec":{"kubeConfig":{"secretRef":{"name":"remote"}}},"status":{"inventory":{"entries":[{"id":"apps_api_apps_Deployment","v":"v1"}]}}}`)
	links := Links("Kustomization", raw)
	if len(links) != 1 || links[0].Name != "remote" {
		t.Fatalf("remote resource exposed as local: %+v", links)
	}
	raw = []byte(strings.ReplaceAll(string(raw), `"kubeConfig":{"secretRef":{"name":"remote"}}`, `"prune":true`))
	links = Links("Kustomization", raw)
	if len(links) != 1 || links[0].Resource != "Deployment.apps" || links[0].Namespace != "apps" {
		t.Fatalf("local inventory: %+v", links)
	}
}

func TestFluxDescriptionsTolerateMalformedFields(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"apiVersion":"helm.toolkit.fluxcd.io/v2","metadata":null,"spec":{"chart":42,"dependsOn":[null]},"status":{"history":[null,42],"conditions":[null]}}`} {
		_ = Object("HelmRelease", []byte(raw))
		_ = Links("HelmRelease", []byte(raw))
	}
	raw := []byte(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","metadata":{"generation":2},"spec":{"suspend":true},"status":{"conditions":[{"type":"Ready","status":"False","reason":"InstallFailed","message":"chart failed"}],"history":[{"version":3,"chartName":"api","chartVersion":"1.2","status":"failed"}]}}`)
	sections := Object("HelmRelease", raw)
	b, _ := json.Marshal(sections)
	for _, want := range []string{"Flux reconciliation", "Suspended", "yes", "Helm release history", "InstallFailed", "chart failed"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s: %s", want, b)
		}
	}
}
