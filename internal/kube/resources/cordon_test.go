package resources

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func nodeTarget() Target {
	return Target{
		GVR:        schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
		Namespaced: false,
	}
}

func TestCordonPatchesTheNodeItself(t *testing.T) {
	s := newStub(t, `{"kind":"Node","spec":{"unschedulable":true}}`)

	if err := Cordon(context.Background(), s.client, nodeTarget(), "node-1", true); err != nil {
		t.Fatalf("Cordon: %v", err)
	}

	// Cluster-scoped: a namespace in the path would address something that
	// does not exist.
	if want := "/api/v1/nodes/node-1"; s.lastURL != want {
		t.Errorf("addressed %q, want %q", s.lastURL, want)
	}
	if s.method != "PATCH" {
		t.Errorf("method = %s, want PATCH", s.method)
	}
	if !strings.Contains(s.contentType, "merge-patch") {
		t.Errorf("content type = %q, want a merge patch", s.contentType)
	}
	if !strings.Contains(s.lastBody, `"unschedulable":true`) {
		t.Errorf("body = %q", s.lastBody)
	}
}

func TestUncordonAsksForTheOppositeOfCordon(t *testing.T) {
	s := newStub(t, `{"kind":"Node","spec":{}}`)

	if err := Cordon(context.Background(), s.client, nodeTarget(), "node-1", false); err != nil {
		t.Fatalf("Cordon: %v", err)
	}
	if !strings.Contains(s.lastBody, `"unschedulable":false`) {
		// Sending nothing, or omitting the field, would leave a cordoned node
		// cordoned while telling the user it had been released.
		t.Errorf("body = %q, want the field set to false", s.lastBody)
	}
}

func TestCordoningWithoutANameIsRefused(t *testing.T) {
	s := newStub(t, `{}`)
	if err := Cordon(context.Background(), s.client, nodeTarget(), "", true); err == nil {
		t.Error("an empty name would address the collection, not a node")
	}
	if s.lastURL != "" {
		t.Errorf("nothing may be sent, but %q was requested", s.lastURL)
	}
}

func TestARefusedCordonIsReported(t *testing.T) {
	s := newStub(t, `{"kind":"Status","status":"Failure","message":"nodes \"node-1\" is forbidden","code":403}`)
	s.status = http.StatusForbidden

	if err := Cordon(context.Background(), s.client, nodeTarget(), "node-1", true); err == nil {
		t.Fatal("a server that refuses the patch must not look like a success")
	}
}

func TestCordonHonoursCancellation(t *testing.T) {
	s := newStub(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Cordon(ctx, s.client, nodeTarget(), "node-1", true); err == nil {
		t.Error("a cancelled request must not report a change nobody made")
	}
}

func TestTheCurrentStateIsReadFromTheDocumentOrNotAtAll(t *testing.T) {
	cases := []struct {
		name                string
		raw                 string
		unschedulable, know bool
	}{
		{"a cordoned node", `{"kind":"Node","spec":{"unschedulable":true}}`, true, true},
		{"a node taking pods", `{"kind":"Node","spec":{"unschedulable":false}}`, false, true},
		{"the field left out", `{"kind":"Node","spec":{}}`, false, true},
		{"a document that is not one", `not json`, false, false},
		{"nothing read at all", ``, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unschedulable, known := Unschedulable([]byte(c.raw))
			if unschedulable != c.unschedulable || known != c.know {
				t.Errorf("Unschedulable() = %v, %v; want %v, %v",
					unschedulable, known, c.unschedulable, c.know)
			}
		})
	}
}
