package client

import (
	"context"
	"errors"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPermissionsAreAskedNotTried(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		a := review.Spec.ResourceAttributes
		switch {
		case a.Resource == "secrets":
			return true, nil, errors.New("the authoriser timed out")
		case a.Resource == "nodes" && a.Namespace != "":
			t.Errorf("nodes are cluster-scoped and must be asked without a namespace, got %q", a.Namespace)
		}
		// A read-only account: it may look, and nothing else.
		review.Status.Allowed = a.Verb == "list" || a.Verb == "get"
		return true, review, nil
	})

	got := map[string]Permission{}
	for _, p := range permissions(context.Background(), cs, "shop") {
		got[p.Label] = p
	}
	if p := got["read logs"]; !p.Known || !p.Allowed {
		t.Errorf("read logs = %+v, want allowed", p)
	}
	if p := got["open a shell"]; !p.Known || p.Allowed {
		t.Errorf("open a shell = %+v, want refused", p)
	}
	if p := got["read secrets"]; p.Known {
		t.Errorf("a question that failed has no answer, got %+v", p)
	}
	if len(got) != len(permissionProbes) {
		t.Errorf("one failed question must not cost the others: %d of %d", len(got), len(permissionProbes))
	}
}
