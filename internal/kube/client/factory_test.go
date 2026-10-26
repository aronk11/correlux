package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aronk11/correlux/internal/kube/logs"
)

// twoClusters is a kubeconfig with two contexts, written to disk because that
// is where client-go reads one from: the factory is only honest about what it
// builds if it builds from a real file.
const twoClusters = `apiVersion: v1
kind: Config
current-context: staging
clusters:
  - name: prod
    cluster: {server: "https://api.prod.example.com"}
  - name: staging
    cluster: {server: "https://api.staging.example.com"}
contexts:
  - name: prod-eu
    context: {cluster: prod, user: admin, namespace: payments}
  - name: staging
    context: {cluster: staging, user: admin}
users:
  - name: admin
    user: {token: shh}
`

func newFactory(t *testing.T, opts Options) *Factory {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(twoClusters), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	raw, err := rules.Load()
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}
	return New(*raw, rules, opts)
}

func TestEachContextGetsItsOwnServer(t *testing.T) {
	f := newFactory(t, Options{})

	prod, err := f.RESTConfig("prod-eu")
	if err != nil {
		t.Fatalf("prod-eu: %v", err)
	}
	staging, err := f.RESTConfig("staging")
	if err != nil {
		t.Fatalf("staging: %v", err)
	}

	if prod.Host == staging.Host {
		t.Fatalf("both contexts point at %q; they must not share a config", prod.Host)
	}
	if !strings.Contains(prod.Host, "prod") {
		t.Errorf("prod-eu points at %q", prod.Host)
	}
}

func TestTheSameContextIsBuiltOnce(t *testing.T) {
	f := newFactory(t, Options{})

	first, err := f.RESTConfig("prod-eu")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	second, _ := f.RESTConfig("prod-eu")
	if first != second {
		t.Error("a context's config is built once and reused; the UI asks for it on every command")
	}

	client, err := f.Clientset("prod-eu")
	if err != nil {
		t.Fatalf("Clientset: %v", err)
	}
	again, _ := f.Clientset("prod-eu")
	if client != again {
		t.Error("the clientset is cached with it")
	}
}

func TestAContextThatIsNotInTheKubeconfigIsAnError(t *testing.T) {
	f := newFactory(t, Options{})

	_, err := f.RESTConfig("a-cluster-that-does-not-exist")
	if err == nil {
		t.Fatal("an unknown context must be an error, not an empty config")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("the error must say what is wrong, got %q", err)
	}

	// And the failure is remembered rather than retried on every keystroke:
	// it is deterministic for a given kubeconfig.
	if _, again := f.RESTConfig("a-cluster-that-does-not-exist"); again == nil {
		t.Error("the second attempt must fail the same way")
	}
}

func TestInvalidateRebuildsOneContextAndResetRebuildsAll(t *testing.T) {
	f := newFactory(t, Options{})

	prod, _ := f.RESTConfig("prod-eu")
	staging, _ := f.RESTConfig("staging")

	f.Invalidate("prod-eu")
	rebuilt, _ := f.RESTConfig("prod-eu")
	if rebuilt == prod {
		t.Error("Invalidate must drop the cached config so the next call rebuilds it")
	}
	if kept, _ := f.RESTConfig("staging"); kept != staging {
		t.Error("and must leave every other context alone")
	}

	f.Reset()
	if after, _ := f.RESTConfig("staging"); after == staging {
		t.Error("Reset must drop them all, which is what a reloaded kubeconfig needs")
	}
}

func TestEveryRequestIsBoundedAndIdentifiesCorrelux(t *testing.T) {
	f := newFactory(t, Options{Timeout: 3 * time.Second})

	cfg, err := f.RESTConfig("staging")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want the one configured", cfg.Timeout)
	}
	if f.Timeout() != 3*time.Second {
		t.Errorf("Timeout() = %v, want the same value the commands bound themselves by", f.Timeout())
	}
	if cfg.UserAgent != UserAgent {
		t.Errorf("user agent = %q, want correlux to be identifiable in an audit log", cfg.UserAgent)
	}
	if cfg.QPS != DefaultQPS || cfg.Burst != DefaultBurst {
		t.Errorf("qps/burst = %v/%d, want the configured rate limit", cfg.QPS, cfg.Burst)
	}
	if cfg.WarningHandler == nil {
		t.Error("warnings must be swallowed: a stray line corrupts a full-screen frame")
	}
}

func TestRESTConfigForExecHasNoRequestTimeout(t *testing.T) {
	f := newFactory(t, Options{Timeout: 3 * time.Second})

	cfg, err := f.RESTConfigForExec("staging")
	if err != nil {
		t.Fatalf("RESTConfigForExec: %v", err)
	}
	if cfg.Timeout != 0 {
		t.Errorf("timeout = %v, want none: an exec session must not be cut off mid-session", cfg.Timeout)
	}
	if !strings.Contains(cfg.Host, "staging") {
		t.Errorf("host = %q, want the exec config to authenticate exactly as RESTConfig does", cfg.Host)
	}

	// The cached, timeout-bearing config must be untouched: everything else
	// that reads it still gets a bounded request.
	plain, _ := f.RESTConfig("staging")
	if plain.Timeout != 3*time.Second {
		t.Errorf("RESTConfig's timeout = %v after RESTConfigForExec, want it unchanged", plain.Timeout)
	}
}

func TestATimeoutIsAlwaysSet(t *testing.T) {
	// Zero means the default, never "wait forever": the UI must be able to give
	// up on a cluster that does not answer.
	f := newFactory(t, Options{})
	if f.Timeout() != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", f.Timeout(), DefaultTimeout)
	}
	cfg, err := f.RESTConfig("staging")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Timeout <= 0 {
		t.Errorf("request timeout = %v, want a bound", cfg.Timeout)
	}
}

// A followed log is one request that stays open; the per-request timeout every
// other call carries would cut it off mid-stream after Timeout().
func TestFollowedLogsUseAClientWithoutTheRequestTimeout(t *testing.T) {
	f := newFactory(t, Options{Timeout: 15 * time.Second})

	normal, err := f.Clientset("staging")
	if err != nil {
		t.Fatal(err)
	}
	streaming, err := f.StreamingClientset("staging")
	if err != nil {
		t.Fatal(err)
	}
	timeout := func(cs kubernetes.Interface) time.Duration {
		return cs.CoreV1().RESTClient().(*rest.RESTClient).Client.Timeout
	}
	if timeout(normal) != 15*time.Second {
		t.Errorf("ordinary requests must keep their timeout, got %v", timeout(normal))
	}
	if timeout(streaming) != 0 {
		t.Errorf("the streaming client must not time out, got %v", timeout(streaming))
	}
	if cfg, _ := f.RESTConfig("staging"); cfg.Timeout != 15*time.Second {
		t.Error("building the streaming client must not change the shared config")
	}
}

func TestAFollowedLogOutlivesTheRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 10; i++ {
			_, _ = fmt.Fprintf(w, "2026-10-26T10:00:%02dZ line %d\n", i, i)
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}))
	defer srv.Close()
	f := logFactory(t, srv.URL, 300*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := f.Tail(ctx, "local", []logs.Source{{Namespace: "shop", Pod: "payments-0"}}, logs.Options{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for event := range stream {
		if event.Err != nil {
			t.Fatalf("the stream failed after %d lines: %v", lines, event.Err)
		}
		lines++
		if lines == 10 {
			cancel()
		}
	}
	if lines < 10 {
		t.Errorf("only %d of 10 lines arrived; a 300ms request timeout must not cut a followed log", lines)
	}
}

func TestAOneOffLogReadKeepsTheRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	f := logFactory(t, srv.URL, 200*time.Millisecond)

	stream, err := f.Tail(context.Background(), "local", []logs.Source{{Namespace: "shop", Pod: "payments-0"}}, logs.Options{Previous: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var failed error
		for event := range stream {
			if event.Err != nil {
				failed = event.Err
			}
		}
		done <- failed
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a stalled one-off read must be reported as failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a one-off read hung past the request timeout")
	}
}

func logFactory(t *testing.T, server string, timeout time.Duration) *Factory {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	config := "apiVersion: v1\nkind: Config\ncurrent-context: local\nclusters:\n- name: local\n  cluster: {server: \"" + server +
		"\"}\ncontexts:\n- name: local\n  context: {cluster: local, user: me}\nusers:\n- name: me\n  user: {token: t}\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	raw, err := rules.Load()
	if err != nil {
		t.Fatal(err)
	}
	return New(*raw, rules, Options{Timeout: timeout})
}
