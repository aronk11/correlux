package client

import (
	"context"

	"github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

// ListTable fetches one page of any resource — native or custom — as a
// server-rendered table.
func (f *Factory) ListTable(
	ctx context.Context,
	contextName string,
	res discovery.Resource,
	opts resources.ListOptions,
) (*resources.Table, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	target := resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}
	return resources.List(ctx, cs.Discovery().RESTClient(), target, opts)
}
