package client

import (
	"context"
	"io"
	"time"

	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardEvent reports readiness with actual local ports, or the final outcome.
type ForwardEvent struct {
	Ports []portforward.ForwardedPort
	Done  bool
	Err   error
}

// ForwardPod binds only loopback addresses and stops when its context ends.
func (f *Factory) ForwardPod(ctx context.Context, cluster, namespace, pod string, ports []string) (<-chan ForwardEvent, error) {
	cfg, err := f.RESTConfigForExec(cluster)
	if err != nil {
		return nil, err
	}
	core, err := corev1client.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	req := core.RESTClient().Post().Namespace(namespace).Resource("pods").Name(pod).SubResource("portforward")
	// The dialer's request carries cancellation, including during connection setup.
	dialer := &contextForwardDialer{ctx: ctx, transport: transport, upgrader: upgrader, url: req.URL()}
	ready := make(chan struct{})
	streamCtx, cancel := context.WithCancel(ctx)
	dialer.ctx = streamCtx
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, ports, streamCtx.Done(), ready, io.Discard, io.Discard)
	if err != nil {
		cancel()
		return nil, err
	}
	events := make(chan ForwardEvent, 2)
	go func() {
		defer close(events)
		defer cancel()
		timer := time.NewTimer(f.Timeout())
		defer timer.Stop()
		finished := make(chan error, 1)
		go func() { finished <- forwarder.ForwardPorts() }()
		select {
		case <-timer.C:
			cancel()
			<-finished
			events <- ForwardEvent{Done: true, Err: context.DeadlineExceeded}
			return
		case err := <-finished:
			events <- ForwardEvent{Done: true, Err: err}
			return
		case <-ready:
			bound, err := forwarder.GetPorts()
			if err != nil {
				events <- ForwardEvent{Done: true, Err: err}
				return
			}
			events <- ForwardEvent{Ports: bound}
		}
		err := <-finished
		events <- ForwardEvent{Done: true, Err: err}
	}()
	return events, nil
}
