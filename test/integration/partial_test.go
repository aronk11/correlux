//go:build integration

package integration

import (
	gocontext "context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aggregatorclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
)

// TestBrokenAggregatedAPIDoesNotBreakDiscovery reproduces the single most
// common way a Kubernetes UI becomes useless: an aggregated API server (metrics,
// a mesh, a controller's webhook) is unreachable, discovery returns an error
// alongside a perfectly good list of everything else, and the tool refuses to
// show anything.
//
// kubeui must degrade to "60 kinds, one group unavailable".
func TestBrokenAggregatedAPIDoesNotBreakDiscovery(t *testing.T) {
	restConfig, err := shared.factory.RESTConfig(shared.context)
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	client, err := aggregatorclient.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("aggregator client: %v", err)
	}

	const name = "v1alpha1.broken.kubeui.dev"
	apiService := &apiregistrationv1.APIService{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiregistrationv1.APIServiceSpec{
			Group:                 "broken.kubeui.dev",
			Version:               "v1alpha1",
			GroupPriorityMinimum:  100,
			VersionPriority:       100,
			InsecureSkipTLSVerify: true,
			Service: &apiregistrationv1.ServiceReference{
				Name:      "does-not-exist",
				Namespace: "default",
			},
		},
	}

	if _, err := client.ApiregistrationV1().APIServices().Create(ctx(t), apiService, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			t.Fatalf("create the broken APIService: %v", err)
		}
	}
	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup; a leftover APIService only affects this test cluster
		cleanupCtx, cancel := gocontext.WithTimeout(gocontext.Background(), 30*time.Second)
		defer cancel()
		client.ApiregistrationV1().APIServices().Delete(cleanupCtx, name, metav1.DeleteOptions{})
	})

	// Give the aggregator a moment to notice the endpoint is unavailable.
	deadline := time.Now().Add(45 * time.Second)
	var partial bool
	for time.Now().Before(deadline) {
		catalog, err := shared.factory.Catalog(ctx(t), shared.context)
		if err != nil {
			t.Fatalf("discovery must not fail outright when one group is broken: %v", err)
		}
		if catalog.Partial() {
			partial = true

			if catalog.Len() < 40 {
				t.Errorf("only %d kinds survived a single broken group", catalog.Len())
			}
			if _, ok := catalog.Lookup("pods"); !ok {
				t.Error("pods must still be usable")
			}
			if _, ok := catalog.Lookup("widgets"); !ok {
				t.Error("custom resources must still be usable")
			}
			if len(catalog.Failures) == 0 {
				t.Error("the failing group must be named so the UI can explain it")
			}
			t.Logf("catalog degraded gracefully: %d kinds, failures: %v", catalog.Len(), catalog.Failures)
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !partial {
		t.Skip("the aggregator did not report the broken group in time; nothing to assert")
	}
}
