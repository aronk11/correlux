package resources

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func widgetTarget() Target {
	return Target{
		GVR:        schema.GroupVersionResource{Group: "acme.example.com", Version: "v1alpha1", Resource: "widgets"},
		Namespaced: true,
	}
}

func TestDeleteAddressesTheObjectAndTakesItsDependentsWithIt(t *testing.T) {
	s := newStub(t, `{"kind":"Status","status":"Success"}`)

	if err := Delete(context.Background(), s.client, deploymentTarget(), "shop", "payments", DeleteOptions{}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if want := "/apis/apps/v1/namespaces/shop/deployments/payments"; s.lastURL != want {
		t.Errorf("addressed %q, want %q", s.lastURL, want)
	}
	if s.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", s.method)
	}
	// The confirmation promises that what the object owns goes with it, and
	// foreground deletion is what makes that true rather than eventual.
	if !strings.Contains(s.lastBody, `"propagationPolicy":"Foreground"`) {
		t.Errorf("body = %q, want a foreground delete", s.lastBody)
	}
}

func TestDeleteCanLeaveTheDependentsBehind(t *testing.T) {
	s := newStub(t, `{}`)

	if err := Delete(context.Background(), s.client, deploymentTarget(), "shop", "payments",
		DeleteOptions{Orphan: true}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.Contains(s.lastBody, `"propagationPolicy":"Orphan"`) {
		t.Errorf("body = %q, want the dependents orphaned", s.lastBody)
	}
}

func TestDeleteCarriesThePreconditionsItWasGiven(t *testing.T) {
	s := newStub(t, `{}`)

	if err := Delete(context.Background(), s.client, deploymentTarget(), "shop", "payments",
		DeleteOptions{UID: "dep-uid", ResourceVersion: "40213"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Without the uid the server would happily delete a namesake recreated
	// since the screen was read.
	for _, want := range []string{`"uid":"dep-uid"`, `"resourceVersion":"40213"`} {
		if !strings.Contains(s.lastBody, want) {
			t.Errorf("body = %q, want %s", s.lastBody, want)
		}
	}
}

func TestDeleteAddressesEveryKindTheSameWay(t *testing.T) {
	cases := []struct {
		name      string
		target    Target
		namespace string
		want      string
	}{
		{"namespaced", deploymentTarget(), "shop", "/apis/apps/v1/namespaces/shop/deployments/payments"},
		{
			"cluster scoped",
			Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}},
			"",
			"/api/v1/nodes/payments",
		},
		{
			"custom resource",
			widgetTarget(),
			"shop",
			"/apis/acme.example.com/v1alpha1/namespaces/shop/widgets/payments",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t, `{}`)
			if err := Delete(context.Background(), s.client, tc.target, tc.namespace, "payments", DeleteOptions{}); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if s.lastURL != tc.want {
				t.Errorf("addressed %q, want %q", s.lastURL, tc.want)
			}
		})
	}
}

func TestDeleteWithoutANameIsRefused(t *testing.T) {
	s := newStub(t, `{}`)
	if err := Delete(context.Background(), s.client, deploymentTarget(), "shop", "", DeleteOptions{}); err == nil {
		t.Error("an empty name would address the collection: every deployment in the namespace")
	}
	if s.method != "" {
		t.Errorf("the request reached the server as %s; it must not leave Correlux", s.method)
	}
}

func TestDeleteReportsWhatTheServerRefused(t *testing.T) {
	s := newStub(t, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403}`)
	s.status = http.StatusForbidden

	if err := Delete(context.Background(), s.client, deploymentTarget(), "shop", "payments", DeleteOptions{}); err == nil {
		t.Error("a refusal must be returned, not swallowed into a successful delete")
	}
}

func TestDeleteStopsWithItsContext(t *testing.T) {
	s := newStub(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Delete(ctx, s.client, deploymentTarget(), "shop", "payments", DeleteOptions{}); err == nil {
		t.Error("a cancelled request must not be sent")
	}
}
