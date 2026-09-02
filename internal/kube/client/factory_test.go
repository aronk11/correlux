package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
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

func TestEveryRequestIsBoundedAndIdentifiesKubeui(t *testing.T) {
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
		t.Errorf("user agent = %q, want kubeui to be identifiable in an audit log", cfg.UserAgent)
	}
	if cfg.QPS != DefaultQPS || cfg.Burst != DefaultBurst {
		t.Errorf("qps/burst = %v/%d, want the configured rate limit", cfg.QPS, cfg.Burst)
	}
	if cfg.WarningHandler == nil {
		t.Error("warnings must be swallowed: a stray line corrupts a full-screen frame")
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
