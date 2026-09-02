package resources

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func deploymentTarget() Target {
	return Target{
		GVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Namespaced: true,
	}
}

func TestScalePatchesTheScaleSubresource(t *testing.T) {
	s := newStub(t, `{"kind":"Scale","spec":{"replicas":5}}`)

	if err := Scale(context.Background(), s.client, deploymentTarget(), "shop", "payments", 5); err != nil {
		t.Fatalf("Scale: %v", err)
	}

	if want := "/apis/apps/v1/namespaces/shop/deployments/payments/scale"; s.lastURL != want {
		t.Errorf("addressed %q, want %q", s.lastURL, want)
	}
	if s.method != "PATCH" {
		t.Errorf("method = %s, want PATCH", s.method)
	}
	if !strings.Contains(s.contentType, "merge-patch") {
		t.Errorf("content type = %q, want a merge patch", s.contentType)
	}
	if !strings.Contains(s.lastBody, `"replicas":5`) {
		t.Errorf("body = %q", s.lastBody)
	}
}

func TestScaleToZeroIsAllowed(t *testing.T) {
	// Scaling to zero is a legitimate thing to do on purpose, and the
	// confirmation is where the user is warned about it, not the API layer.
	s := newStub(t, `{}`)
	if err := Scale(context.Background(), s.client, deploymentTarget(), "shop", "payments", 0); err != nil {
		t.Fatalf("Scale to zero: %v", err)
	}
	if !strings.Contains(s.lastBody, `"replicas":0`) {
		t.Errorf("body = %q", s.lastBody)
	}
}

func TestScaleRefusesAnAbsurdCount(t *testing.T) {
	s := newStub(t, `{}`)
	for _, replicas := range []int32{-1, MaxReplicas + 1} {
		if err := Scale(context.Background(), s.client, deploymentTarget(), "shop", "payments", replicas); err == nil {
			t.Errorf("scaling to %d must be refused before it reaches the cluster", replicas)
		}
	}
}

func TestScaleWithoutANameIsRefused(t *testing.T) {
	s := newStub(t, `{}`)
	if err := Scale(context.Background(), s.client, deploymentTarget(), "shop", "", 3); err == nil {
		t.Error("an empty name would address the collection, not a workload")
	}
}
