package client

import (
	"context"

	"github.com/aronk11/correlux/internal/kube/logs"
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
	// Only a followed log is meant to stay open; a one-off read keeps the
	// request timeout every other call has, so a stalled server is reported.
	get := f.Clientset
	if opts.Follow {
		get = f.StreamingClientset
	}
	cs, err := get(contextName)
	if err != nil {
		return nil, err
	}
	return logs.Tail(ctx, cs, sources, opts), nil
}
