package gitops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFluxActionsAreGroupAndStateSpecific(t *testing.T) {
	tests := []struct{ api, kind, spec, want string }{
		{"helm.toolkit.fluxcd.io/v2", "HelmRelease", "{}", "reconcile,suspend,reset,force"},
		{"helm.toolkit.fluxcd.io/v2", "HelmRelease", `{"suspend":true}`, "resume"},
		{"other.example/v1", "HelmRelease", "{}", ""},
		{"source.toolkit.fluxcd.io/v1", "GitRepository", "{}", "reconcile,suspend"},
		{"source.toolkit.fluxcd.io/v1", "HelmRepository", `{"type":"oci"}`, ""},
		{"image.toolkit.fluxcd.io/v1", "ImagePolicy", "{}", ""},
		{"notification.toolkit.fluxcd.io/v1", "Receiver", "{}", ""},
	}
	for _, tt := range tests {
		t.Run(tt.api+tt.kind+tt.spec, func(t *testing.T) {
			raw := []byte(`{"apiVersion":"` + tt.api + `","kind":"` + tt.kind + `","spec":` + tt.spec + `}`)
			if got := strings.Join(Actions(raw), ","); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestPatchUsesConcurrencyAndMatchingTokens(t *testing.T) {
	raw := []byte(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"resourceVersion":"42"},"spec":{"values":{"doNotChange":true}}}`)
	for _, action := range []string{"reconcile", "force", "reset", "suspend"} {
		patch, err := Patch(raw, action, time.Unix(123, 456))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err = json.Unmarshal(patch, &doc); err != nil {
			t.Fatal(err)
		}
		meta := doc["metadata"].(map[string]any)
		if meta["resourceVersion"] != "42" {
			t.Fatal(string(patch))
		}
		if action == "force" || action == "reset" {
			annotations := meta["annotations"].(map[string]any)
			if annotations["reconcile.fluxcd.io/requestedAt"] != annotations["reconcile.fluxcd.io/"+action+"At"] {
				t.Fatal(string(patch))
			}
		}
		if strings.Contains(string(patch), "values") {
			t.Fatal("patch included unrelated spec")
		}
	}
	if _, err := Patch(raw, "resume", time.Now()); err == nil {
		t.Fatal("resumed an object which is not suspended")
	}
	if _, err := Patch([]byte(strings.ReplaceAll(string(raw), `"42"`, `""`)), "reconcile", time.Now()); err == nil {
		t.Fatal("allowed missing concurrency precondition")
	}
}
