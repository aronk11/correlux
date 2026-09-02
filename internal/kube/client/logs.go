package client

import (
	"context"

	"github.com/aronk11/kubeui/internal/kube/logs"
)

// Tail streams the logs of one or more containers.
//
// The stream lives until the context is cancelled, which is how the UI stops
// reading when the user leaves the screen: a log that keeps arriving for a view
// nobody is looking at is a connection held open for nothing.
func (f *Factory) Tail(
	ctx context.Context,
	contextName string,
	sources []logs.Source,
	opts logs.Options,
) (<-chan logs.Event, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, err
	}
	return logs.Tail(ctx, cs, sources, opts), nil
}
