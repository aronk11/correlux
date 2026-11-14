package client

import (
	"context"
	"sync"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Permission is one thing Correlux can do, and whether this account may.
type Permission struct {
	// Label is the action in the words the interface uses for it.
	Label string
	// Allowed is the API server's answer; Known is false when it gave none,
	// which is neither a yes nor a no.
	Allowed bool
	Known   bool
}

// permissionProbes are the actions Correlux offers, each as the request the
// API server would have to authorise. They are asked rather than tried:
// finding out that a restart is forbidden by attempting one in production is
// the expensive way to learn it.
var permissionProbes = []struct {
	label, verb, group, resource, subresource string
	// cluster marks a cluster-scoped resource, asked without a namespace.
	cluster bool
}{
	{"read pods", "list", "", "pods", "", false},
	{"read logs", "get", "", "pods", "log", false},
	{"read events", "list", "", "events", "", false},
	{"read secrets", "get", "", "secrets", "", false},
	{"open a shell", "create", "", "pods", "exec", false},
	{"port-forward", "create", "", "pods", "portforward", false},
	{"debug containers", "patch", "", "pods", "ephemeralcontainers", false},
	{"scale workloads", "patch", "apps", "deployments", "scale", false},
	{"edit and restart workloads", "patch", "apps", "deployments", "", false},
	{"delete pods", "delete", "", "pods", "", false},
	{"read nodes", "list", "", "nodes", "", true},
	{"cordon nodes", "patch", "", "nodes", "", true},
}

// Permissions asks the API server what this account may do in a namespace —
// or across all of them, for an empty one. The questions are independent and
// asked at once; one that fails comes back unknown rather than failing the
// rest, because a partial answer is still an answer.
func (f *Factory) Permissions(ctx context.Context, contextName, namespace string) ([]Permission, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	return permissions(ctx, cs, namespace), nil
}

func permissions(ctx context.Context, cs kubernetes.Interface, namespace string) []Permission {
	out := make([]Permission, len(permissionProbes))
	var wg sync.WaitGroup
	for i, p := range permissionProbes {
		out[i].Label = p.label
		wg.Add(1)
		go func() {
			defer wg.Done()
			attributes := &authv1.ResourceAttributes{
				Namespace: namespace, Verb: p.verb, Group: p.group, Resource: p.resource, Subresource: p.subresource,
			}
			if p.cluster {
				attributes.Namespace = ""
			}
			review, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx,
				&authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes}},
				metav1.CreateOptions{})
			if err != nil {
				return
			}
			out[i].Allowed, out[i].Known = review.Status.Allowed, true
		}()
	}
	wg.Wait()
	return out
}
