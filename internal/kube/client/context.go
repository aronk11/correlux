package client

import (
	"context"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/workloads"
)

// ApplicationContext reads the evidence a diagnosis needs for one scope:
// events, endpoints, nodes and volume claims.
func (f *Factory) ApplicationContext(
	ctx context.Context,
	contextName string,
	opts workloads.Options,
) (application.Context, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return application.Context{}, err
	}
	return workloads.CollectContext(ctx, cs, opts)
}
