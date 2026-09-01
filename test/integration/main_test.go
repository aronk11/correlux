//go:build integration

// Package integration exercises kubeui against a real Kubernetes API server.
//
// The unit tests prove that the code does what it says; these prove that what
// it says is true of an actual cluster — that discovery really finds CRDs, that
// the API server really renders their printer columns, that paging really
// terminates, and that a cluster with thousands of objects stays responsive.
//
// Run them with `task test:integration`, which brings up a kind cluster first.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	"github.com/aronk11/kubeui/internal/kube/kubeconfig"
)

// testTimeout bounds any single API interaction in the suite.
const testTimeout = 60 * time.Second

// cluster is the connection the whole suite shares.
type cluster struct {
	factory *kubeclient.Factory
	config  *kubeconfig.Config
	context string
}

var shared *cluster

func TestMain(m *testing.M) {
	path := os.Getenv("KUBEUI_TEST_KUBECONFIG")
	if path == "" {
		// Refusing to run is better than silently testing nothing.
		println("integration tests need KUBEUI_TEST_KUBECONFIG; run `task test:integration`")
		os.Exit(1)
	}

	cfg, err := kubeconfig.Load(kubeconfig.LoadOptions{ExplicitPath: path})
	if err != nil {
		println("load kubeconfig: " + err.Error())
		os.Exit(1)
	}
	contextName, err := cfg.ResolveStartContext("", "")
	if err != nil {
		println("resolve context: " + err.Error())
		os.Exit(1)
	}

	shared = &cluster{
		factory: kubeclient.New(cfg.Raw(), cfg.LoadingRules(), kubeclient.Options{Timeout: testTimeout}),
		config:  cfg,
		context: contextName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	info := shared.factory.Probe(ctx, contextName)
	cancel()
	if info.State != kubeclient.ConnOK {
		println("cluster is not reachable: " + info.State.String())
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ctx returns a context bounded by the suite timeout.
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return c
}
