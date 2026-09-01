package client

import (
	"context"

	"github.com/aronk11/kubeui/internal/kube/discovery"
)

// Catalog discovers the resource kinds a context's cluster serves, including
// custom resources.
//
// Discovery is a handful of round trips, so it runs once per context and is
// refreshed explicitly (a CRD installed while kubeui is open appears after a
// refresh, not never).
func (f *Factory) Catalog(ctx context.Context, contextName string) (*discovery.Catalog, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	// The discovery client's calls do not take a context, so honour
	// cancellation by abandoning the result rather than the request.
	type result struct {
		catalog *discovery.Catalog
		err     error
	}
	done := make(chan result, 1)
	go func() {
		catalog, buildErr := discovery.BuildCatalog(cs.Discovery())
		done <- result{catalog: catalog, err: buildErr}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.catalog, r.err
	}
}
