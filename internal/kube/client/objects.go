package client

import (
	"context"

	"github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/resources"
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

// CordonNode marks a node schedulable or unschedulable.
func (f *Factory) CordonNode(
	ctx context.Context,
	contextName string,
	res discovery.Resource,
	name string,
	unschedulable bool,
) error {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return err
	}
	target := resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}
	return resources.Cordon(ctx, cs.Discovery().RESTClient(), target, name, unschedulable)
}

// UpdateObject replaces an object with an edited document.
func (f *Factory) UpdateObject(
	ctx context.Context,
	contextName string,
	res discovery.Resource,
	namespace, name string,
	document []byte,
) (*resources.Object, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	target := resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}
	return resources.Update(ctx, cs.Discovery().RESTClient(), target, namespace, name, document)
}
