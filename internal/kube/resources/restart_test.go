package resources

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// restartedAt is a fixed moment, so the patch a test asserts on is the patch
// the code built rather than the clock's.
var restartedAt = time.Date(2026, 9, 5, 8, 30, 0, 0, time.UTC)

func TestRestartStampsThePodTemplate(t *testing.T) {
	s := newStub(t, `{"kind":"Deployment"}`)

	if err := Restart(context.Background(), s.client, deploymentTarget(), "shop", "payments", restartedAt); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	if want := "/apis/apps/v1/namespaces/shop/deployments/payments"; s.lastURL != want {
		t.Errorf("addressed %q, want %q", s.lastURL, want)
	}
	if s.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", s.method)
	}
	// A merge patch, not a strategic one: a custom resource with a pod
	// template has no Go type for the strategic machinery to merge against.
	if !strings.Contains(s.contentType, "merge-patch") {
		t.Errorf("content type = %q, want a merge patch", s.contentType)
	}
	// The template, not the object's own annotations: stamping the object
	// would record the intention without replacing a single pod.
	want := `{"spec":{"template":{"metadata":{"annotations":` +
		`{"kubectl.kubernetes.io/restartedAt":"2026-09-05T08:30:00Z"}}}}}`
	if s.lastBody != want {
		t.Errorf("body = %q, want %q", s.lastBody, want)
	}
}

func TestRestartAddressesACustomResourceTheSameWay(t *testing.T) {
	s := newStub(t, `{}`)
	target := Target{
		GVR:        schema.GroupVersionResource{Group: "acme.example.com", Version: "v1alpha1", Resource: "widgets"},
		Namespaced: true,
	}

	if err := Restart(context.Background(), s.client, target, "shop", "spinner", restartedAt); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if want := "/apis/acme.example.com/v1alpha1/namespaces/shop/widgets/spinner"; s.lastURL != want {
		t.Errorf("addressed %q, want %q", s.lastURL, want)
	}
}

func TestRestartWithoutANameIsRefused(t *testing.T) {
	s := newStub(t, `{}`)
	if err := Restart(context.Background(), s.client, deploymentTarget(), "shop", "", restartedAt); err == nil {
		t.Error("an empty name would patch the collection, not a workload")
	}
	if s.method != "" {
		t.Errorf("the request reached the server as %s; it must not leave Correlux", s.method)
	}
}

func TestRestartReportsWhatTheServerRefused(t *testing.T) {
	s := newStub(t, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403}`)
	s.status = http.StatusForbidden

	if err := Restart(context.Background(), s.client, deploymentTarget(), "shop", "payments", restartedAt); err == nil {
		t.Error("a refusal must be returned, not swallowed into a successful rollout")
	}
}

func TestRestartStopsWithItsContext(t *testing.T) {
	s := newStub(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Restart(ctx, s.client, deploymentTarget(), "shop", "payments", restartedAt); err == nil {
		t.Error("a cancelled request must not be sent")
	}
}
