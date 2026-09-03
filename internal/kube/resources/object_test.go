package resources

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const podObject = `{
  "kind": "Pod",
  "apiVersion": "v1",
  "metadata": {
    "name": "payments-7d8f-0",
    "namespace": "shop",
    "resourceVersion": "40213",
    "creationTimestamp": "2026-08-30T10:00:00Z",
    "labels": {"app.kubernetes.io/name": "payments"},
    "annotations": {"kubectl.kubernetes.io/last-applied-configuration": "{}"},
    "ownerReferences": [
      {"kind": "ReplicaSet", "name": "payments-7d8f", "uid": "rs-uid", "controller": true}
    ]
  },
  "spec": {"nodeName": "node-1", "containers": [{"name": "payments", "image": "registry/payments:1.4"}]},
  "status": {"phase": "Running", "unknownFieldFromANewerServer": {"nested": true}}
}`

func podTarget() Target {
	return Target{
		GVR:        schema.GroupVersionResource{Version: "v1", Resource: "pods"},
		Namespaced: true,
	}
}

func TestGetAddressesTheObjectAndRendersItAsYAML(t *testing.T) {
	s := newStub(t, podObject)

	obj, err := Get(context.Background(), s.client, podTarget(), "shop", "payments-7d8f-0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if want := "/api/v1/namespaces/shop/pods/payments-7d8f-0"; s.lastURL != want {
		t.Errorf("requested %q, want %q", s.lastURL, want)
	}
	if obj.Kind != "Pod" || obj.Name != "payments-7d8f-0" || obj.Namespace != "shop" {
		t.Errorf("identity = %s %s/%s", obj.Kind, obj.Namespace, obj.Name)
	}
	if obj.ResourceVersion != "40213" {
		t.Errorf("resourceVersion = %q; an update needs it to detect a conflict", obj.ResourceVersion)
	}
	if !strings.Contains(obj.YAML, "image: registry/payments:1.4") {
		t.Errorf("the YAML must be readable:\n%s", obj.YAML)
	}
	// Fields Correlux knows nothing about must survive: on a custom resource that
	// is most of the document.
	if !strings.Contains(obj.YAML, "unknownFieldFromANewerServer") {
		t.Errorf("unknown fields were dropped:\n%s", obj.YAML)
	}
}

func TestGetKeepsTheOwnerForNavigation(t *testing.T) {
	s := newStub(t, podObject)

	obj, err := Get(context.Background(), s.client, podTarget(), "shop", "payments-7d8f-0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	ref, ok := obj.Controller()
	if !ok || ref.Kind != "ReplicaSet" || ref.Name != "payments-7d8f" {
		t.Errorf("controller = %+v, want the ReplicaSet", ref)
	}
}

func TestGetRejectsAnErrorDisguisedAsAnObject(t *testing.T) {
	s := newStub(t, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods \"gone\" not found","code":404}`)

	if _, err := Get(context.Background(), s.client, podTarget(), "shop", "gone"); err == nil {
		t.Fatal("a Status returned with a 200 must not be shown as if it were the object")
	}
}

func TestGetWithoutANameIsRefused(t *testing.T) {
	s := newStub(t, podObject)
	if _, err := Get(context.Background(), s.client, podTarget(), "shop", ""); err == nil {
		t.Error("an empty name would address the collection, not an object")
	}
}

func TestGetOfAClusterScopedObjectOmitsTheNamespace(t *testing.T) {
	s := newStub(t, `{"kind":"Node","apiVersion":"v1","metadata":{"name":"node-1"}}`)
	target := Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}}

	if _, err := Get(context.Background(), s.client, target, "", "node-1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := "/api/v1/nodes/node-1"; s.lastURL != want {
		t.Errorf("requested %q, want %q", s.lastURL, want)
	}
}
