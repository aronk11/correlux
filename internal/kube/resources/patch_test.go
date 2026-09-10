package resources

import (
	"context"
	"net/http"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFluxMergePatchPreservesScopeAndConflicts(t *testing.T) {
	target := Target{GVR: schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}, Namespaced: true}
	s := newStub(t, `{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"name":"api","namespace":"team","resourceVersion":"43"}}`)
	patch := []byte(`{"metadata":{"resourceVersion":"42","annotations":{"reconcile.fluxcd.io/requestedAt":"token"}}}`)
	obj, err := MergePatch(context.Background(), s.client, target, "team", "api", patch)
	if err != nil {
		t.Fatal(err)
	}
	if s.lastURL != "/apis/helm.toolkit.fluxcd.io/v2/namespaces/team/helmreleases/api" || s.method != "PATCH" || !strings.Contains(s.contentType, "merge-patch") || s.lastBody != string(patch) || obj.ResourceVersion != "43" {
		t.Fatalf("incorrect patch request: %+v", s)
	}
	s.status = http.StatusConflict
	s.body = `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409,"message":"object changed"}`
	if _, err = MergePatch(context.Background(), s.client, target, "team", "api", patch); !apierrors.IsConflict(err) {
		t.Fatalf("conflict was not preserved: %v", err)
	}
}
