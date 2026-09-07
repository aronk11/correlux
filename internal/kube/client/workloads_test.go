package client

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/aronk11/correlux/internal/kube/workloads"
)

func deployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
	}
}

// TestAScopedFleetReadsOnlyTheNamespacesItWasGiven is the point of the scope:
// the API server is asked about those namespaces, and nothing else comes back.
func TestAScopedFleetReadsOnlyTheNamespacesItWasGiven(t *testing.T) {
	cs := fake.NewSimpleClientset(
		deployment("payments", "api"),
		deployment("checkout", "web"),
		deployment("somebody-else", "noise"),
	)

	apps, snapshot, err := applicationsIn(
		context.Background(), cs, []string{"payments", "checkout"}, workloads.Options{})
	if err != nil {
		t.Fatalf("applicationsIn: %v", err)
	}

	got := map[string]bool{}
	for i := range apps {
		got[apps[i].Namespace+"/"+apps[i].Name] = true
	}
	for _, want := range []string{"payments/api", "checkout/web"} {
		if !got[want] {
			t.Errorf("the scope must contain %s, got %v", want, got)
		}
	}
	if got["somebody-else/noise"] {
		t.Errorf("a namespace outside the scope must not be read: %v", got)
	}
	if snapshot.Scope != "payments, checkout" {
		t.Errorf("scope = %q, want both namespaces named", snapshot.Scope)
	}
}

// TestANamespaceThatIsDeniedDoesNotHideTheRest keeps the fleet's rule: one
// cluster refusing one namespace is normal, and silence about the others would
// be the real failure.
func TestANamespaceThatIsDeniedDoesNotHideTheRest(t *testing.T) {
	cs := fake.NewSimpleClientset(deployment("payments", "api"))
	cs.PrependReactor("list", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() == "locked" {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "deployments"}, "", errors.New("nope"))
		}
		return false, nil, nil
	})

	apps, snapshot, err := applicationsIn(
		context.Background(), cs, []string{"payments", "locked"}, workloads.Options{})
	if err != nil {
		t.Fatalf("a partly readable scope must still answer: %v", err)
	}
	if len(apps) == 0 {
		t.Fatal("the namespace that could be read must still be reported")
	}

	var said bool
	for _, gap := range snapshot.Gaps {
		if gap.Scope == "locked" {
			said = true
		}
	}
	if !said {
		t.Errorf("the denied namespace must be named on the gap, got %+v", snapshot.Gaps)
	}
}

// TestAScopeThatCanBeReadNowhereIsAnError: a cluster that answered nothing is
// unreachable, and "no applications" would be a lie.
func TestAScopeThatCanBeReadNowhereIsAnError(t *testing.T) {
	cs := fake.NewSimpleClientset()
	for _, resource := range []string{
		"deployments", "statefulsets", "daemonsets", "jobs", "cronjobs",
		"pods", "services", "ingresses", "replicasets",
	} {
		cs.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("connection refused")
		})
	}

	if _, _, err := applicationsIn(
		context.Background(), cs, []string{"payments"}, workloads.Options{}); err == nil {
		t.Error("a scope nothing could be read in must be an error, not an empty answer")
	}
}
