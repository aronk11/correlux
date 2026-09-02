package resources

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// stub serves a canned response and records what was asked for.
type stub struct {
	server      *httptest.Server
	client      rest.Interface
	lastURL     string
	accept      string
	method      string
	contentType string
	lastBody    string
	body        string
	status      int
}

func newStub(t *testing.T, body string) *stub {
	t.Helper()
	s := &stub{body: body, status: http.StatusOK}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastURL = r.URL.String()
		s.accept = r.Header.Get("Accept")
		s.method = r.Method
		s.contentType = r.Header.Get("Content-Type")
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			s.lastBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.server.Close)

	cfg := &rest.Config{
		Host: s.server.URL,
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	}
	client, err := rest.UnversionedRESTClientFor(cfg)
	if err != nil {
		t.Fatalf("build REST client: %v", err)
	}
	s.client = client
	return s
}

const podTable = `{
  "kind": "Table",
  "apiVersion": "meta.k8s.io/v1",
  "columnDefinitions": [
    {"name": "Name", "type": "string", "priority": 0},
    {"name": "Ready", "type": "string", "priority": 0},
    {"name": "Restarts", "type": "integer", "priority": 0},
    {"name": "Node", "type": "string", "priority": 1}
  ],
  "rows": [
    {
      "cells": ["payments-7d8f", "1/1", 3, "node-1"],
      "object": {"kind":"PartialObjectMetadata","metadata":{"name":"payments-7d8f","namespace":"payments","creationTimestamp":"2026-08-30T10:00:00Z"}}
    },
    {
      "cells": ["worker-8a91", "0/1", 0, null],
      "object": {"kind":"PartialObjectMetadata","metadata":{"name":"worker-8a91","namespace":"payments","creationTimestamp":"2026-08-31T10:00:00Z"}}
    }
  ],
  "metadata": {"continue": "next-page-token", "remainingItemCount": 42}
}`

func TestListDecodesAServerRenderedTable(t *testing.T) {
	s := newStub(t, podTable)

	table, err := List(context.Background(), s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
		ListOptions{Namespace: "payments"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(table.Columns) != 4 {
		t.Fatalf("got %d columns, want 4", len(table.Columns))
	}
	if !table.Columns[3].Wide() || table.Columns[0].Wide() {
		t.Error("priority must decide which columns belong to the wide view")
	}
	if len(table.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(table.Rows))
	}
	if got := table.Rows[0].Cells[2]; got != "3" {
		t.Errorf("an integer cell rendered as %q, want \"3\"", got)
	}
	if got := table.Rows[1].Cells[3]; got != "<none>" {
		t.Errorf("a null cell rendered as %q, want <none>", got)
	}
	if table.Rows[0].Name != "payments-7d8f" || table.Rows[0].Namespace != "payments" {
		t.Errorf("row metadata = %+v", table.Rows[0])
	}
	if table.Rows[0].CreatedAt.IsZero() {
		t.Error("the creation timestamp must be decoded")
	}
	if !table.HasMore() || table.Continue != "next-page-token" {
		t.Errorf("pagination token = %q", table.Continue)
	}
	if table.Remaining != 42 {
		t.Errorf("remaining = %d, want 42", table.Remaining)
	}
}

func TestListAsksForATable(t *testing.T) {
	s := newStub(t, podTable)
	_, _ = List(context.Background(), s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
		ListOptions{Namespace: "payments"})

	if want := "as=Table"; !strings.Contains(s.accept, want) {
		t.Errorf("Accept header %q must request %q — that is what gives CRDs their printer columns", s.accept, want)
	}
}

func TestListBuildsThePathForEveryKindOfResource(t *testing.T) {
	tests := []struct {
		name      string
		target    Target
		namespace string
		want      string
	}{
		{
			name:      "core namespaced",
			target:    Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
			namespace: "payments",
			want:      "/api/v1/namespaces/payments/pods",
		},
		{
			name:   "core cluster scoped",
			target: Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}},
			want:   "/api/v1/nodes",
		},
		{
			name:      "core namespaced across all namespaces",
			target:    Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
			namespace: "",
			want:      "/api/v1/pods",
		},
		{
			name:      "grouped",
			target:    Target{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true},
			namespace: "payments",
			want:      "/apis/apps/v1/namespaces/payments/deployments",
		},
		{
			name:      "custom resource",
			target:    Target{GVR: schema.GroupVersionResource{Group: "acme.example.com", Version: "v1alpha1", Resource: "widgets"}, Namespaced: true},
			namespace: "payments",
			want:      "/apis/acme.example.com/v1alpha1/namespaces/payments/widgets",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := path(tc.target, tc.namespace); got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListPassesPagingAndSelectors(t *testing.T) {
	s := newStub(t, podTable)
	_, _ = List(context.Background(), s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
		ListOptions{Namespace: "x", Limit: 250, Continue: "tok", LabelSelector: "app=payments", FieldSelector: "status.phase=Running"})

	for _, want := range []string{"limit=250", "continue=tok", "labelSelector=app%3Dpayments", "fieldSelector=status.phase%3DRunning"} {
		if !strings.Contains(s.lastURL, want) {
			t.Errorf("request %q is missing %q", s.lastURL, want)
		}
	}
}

func TestListDefaultsThePageSize(t *testing.T) {
	s := newStub(t, podTable)
	_, _ = List(context.Background(), s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}}, ListOptions{})

	if !strings.Contains(s.lastURL, "limit=500") {
		t.Errorf("an unbounded list must never be sent; got %q", s.lastURL)
	}
}

func TestNonTableResponseIsReported(t *testing.T) {
	s := newStub(t, `{"kind":"PodList","apiVersion":"v1","items":[]}`)
	_, err := List(context.Background(), s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}}, ListOptions{})
	if err == nil {
		t.Fatal("a server that cannot print tables must be reported clearly")
	}
}

func TestCancellationIsHonoured(t *testing.T) {
	s := newStub(t, podTable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := List(ctx, s.client,
		Target{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}}, ListOptions{}); err == nil {
		t.Fatal("a cancelled context must abort the request")
	}
}

func TestFormatCellHandlesAnythingACustomResourceMightContain(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, "<none>"},
		{"", "<none>"},
		{"Ready", "Ready"},
		{true, "true"},
		{float64(7), "7"},
		{2.5, "2.5"},
		{[]any{"a", "b"}, "a,b"},
		{map[string]any{"k": "v"}, "map[k:v]"},
	}
	for _, tc := range tests {
		if got := formatCell(tc.in); got != tc.want {
			t.Errorf("formatCell(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := decode([]byte("not json")); err == nil {
		t.Fatal("malformed JSON must be an error, not a panic")
	}
}

func TestRowFallsBackToTheFirstCellForItsName(t *testing.T) {
	body := `{"kind":"Table","columnDefinitions":[{"name":"Name","type":"string"}],"rows":[{"cells":["thing-1"]}]}`
	table, err := decode([]byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if table.Rows[0].Name != "thing-1" {
		t.Errorf("name = %q", table.Rows[0].Name)
	}
}
