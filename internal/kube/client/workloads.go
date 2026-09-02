package client

import (
	"context"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/workloads"
)

// Applications reads a scope and groups it into applications.
//
// The two halves are deliberately separate: collecting is I/O the UI must be
// able to cancel, grouping is pure and cheap. Doing both here means the UI
// receives a finished answer from one cancellable command.
func (f *Factory) Applications(
	ctx context.Context,
	contextName string,
	opts workloads.Options,
) ([]application.Application, application.Snapshot, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, application.Snapshot{}, err
	}
	snapshot, err := workloads.Collect(ctx, cs, opts)
	if err != nil {
		return nil, application.Snapshot{}, err
	}
	return application.Group(snapshot), snapshot, nil
}

// Nodes reads a cluster's nodes, which is what the fleet overview needs to say
// whether a cluster itself is healthy rather than only its applications.
func (f *Factory) Nodes(ctx context.Context, contextName string) ([]application.Node, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	return workloads.CollectNodes(ctx, cs, workloads.Options{})
}
