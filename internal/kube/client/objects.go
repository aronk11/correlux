package client

import (
	"context"

	"github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

// GetObject fetches one object of any kind — native or custom.
func (f *Factory) GetObject(
	ctx context.Context,
	contextName string,
	res discovery.Resource,
	namespace, name string,
) (*resources.Object, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	target := resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}
	return resources.Get(ctx, cs.Discovery().RESTClient(), target, namespace, name)
}

// Scale sets the replica count of a workload.
func (f *Factory) Scale(
	ctx context.Context,
	contextName string,
	res discovery.Resource,
	namespace, name string,
	replicas int32,
) error {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return err
	}
	target := resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}
	return resources.Scale(ctx, cs.Discovery().RESTClient(), target, namespace, name, replicas)
}
