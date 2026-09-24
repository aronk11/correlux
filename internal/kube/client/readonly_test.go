package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
)

// recordingServer answers every request with an empty object and remembers
// what reached it, which is the only way to prove a refused request was never
// sent rather than sent and failed.
func recordingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"api"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func factoryFor(t *testing.T, server string, opts Options) *Factory {
	t.Helper()
	cfg := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: c
    cluster: {server: "` + server + `"}
contexts:
  - name: prod
    context: {cluster: c, user: u}
  - name: dev
    context: {cluster: c, user: u}
users:
  - name: u
    user: {token: t}
`
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	raw, err := rules.Load()
	if err != nil {
		t.Fatal(err)
	}
	return New(*raw, rules, opts)
}

func TestAReadOnlyContextReadsAndNeverWrites(t *testing.T) {
	srv, seen := recordingServer(t)
	f := factoryFor(t, srv.URL, Options{ReadOnly: func(id Identity) bool { return id.Context == "prod" }})
	cs, err := f.Clientset("prod")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, getErr := cs.AppsV1().Deployments("shop").Get(ctx, "api", metav1.GetOptions{}); getErr != nil {
		t.Fatalf("a read must pass: %v", getErr)
	}
	_, err = cs.AppsV1().Deployments("shop").Patch(ctx, "api", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a patch must be refused with ErrReadOnly, got %v", err)
	}
	err = cs.AppsV1().Deployments("shop").Delete(ctx, "api", metav1.DeleteOptions{})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a delete must be refused with ErrReadOnly, got %v", err)
	}

	got := seen()
	if len(got) != 1 || got[0] != "GET /apis/apps/v1/namespaces/shop/deployments/api" {
		t.Errorf("only the read may reach the server, it saw %v", got)
	}
}

func TestOnlyTheNamedContextsAreReadOnly(t *testing.T) {
	srv, seen := recordingServer(t)
	f := factoryFor(t, srv.URL, Options{ReadOnly: func(id Identity) bool { return id.Context == "prod" }})
	cs, err := f.Clientset("dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AppsV1().Deployments("shop").Patch(context.Background(), "api", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{}); err != nil {
		t.Fatalf("dev is not read-only: %v", err)
	}
	if len(seen()) != 1 {
		t.Errorf("the patch must reach the server, it saw %v", seen())
	}
}

func TestReadOnlyRefusesSessionsInsidePods(t *testing.T) {
	cases := []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/api/v1/namespaces/shop/pods/api-0", true},
		{http.MethodGet, "/api/v1/namespaces/shop/pods/api-0/log", true},
		{http.MethodGet, "/api/v1/namespaces/shop/pods/api-0/exec", false},
		{http.MethodPost, "/api/v1/namespaces/shop/pods/api-0/exec", false},
		{http.MethodPost, "/api/v1/namespaces/shop/pods/api-0/attach", false},
		{http.MethodPost, "/api/v1/namespaces/shop/pods/api-0/portforward", false},
		{http.MethodPost, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", true},
		{http.MethodPost, "/k8s/clusters/c-1/apis/authorization.k8s.io/v1/selfsubjectrulesreviews", true},
		{http.MethodPost, "/api/v1/namespaces/shop/pods", false},
		{http.MethodPut, "/api/v1/nodes/node-1", false},
		{http.MethodPatch, "/apis/apps/v1/namespaces/shop/deployments/api/scale", false},
		{http.MethodDelete, "/api/v1/namespaces/shop/pods/api-0", false},
	}
	for _, c := range cases {
		if got := readOnlyAllows(c.method, c.path); got != c.allowed {
			t.Errorf("%s %s: allowed = %v, want %v", c.method, c.path, got, c.allowed)
		}
	}
}
