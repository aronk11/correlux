package client

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListNamespacesSorts(t *testing.T) {
	cs := fake.NewSimpleClientset(
		ns("zeta"), ns("alpha"), ns("kube-system"),
	)

	got, err := listNamespaces(context.Background(), cs)
	if err != nil {
		t.Fatalf("listNamespaces: %v", err)
	}
	want := []string{"alpha", "kube-system", "zeta"}
	if len(got.Names) != len(want) {
		t.Fatalf("got %v, want %v", got.Names, want)
	}
	for i := range want {
		if got.Names[i] != want[i] {
			t.Fatalf("got %v, want %v", got.Names, want)
		}
	}
	if got.Restricted {
		t.Error("a successful list must not be marked restricted")
	}
}

func TestListNamespacesForbiddenIsNotAnError(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "namespaces"}, "", errors.New("nope"))
	})

	got, err := listNamespaces(context.Background(), cs)
	if err != nil {
		t.Fatalf("a denied list must be reported as restricted, not as an error: %v", err)
	}
	if !got.Restricted {
		t.Error("Restricted must be set so the UI can offer manual entry")
	}
	if len(got.Names) != 0 {
		t.Errorf("got %v, want no names", got.Names)
	}
}

func TestListNamespacesPropagatesRealErrors(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection reset")
	})

	if _, err := listNamespaces(context.Background(), cs); err == nil {
		t.Fatal("a transport failure must surface as an error")
	}
}

func ns(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}
