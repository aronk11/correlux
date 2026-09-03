package client

import (
	"context"
	"sync"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/usage"
	"github.com/aronk11/correlux/internal/kube/metrics"
	"github.com/aronk11/correlux/internal/kube/workloads"
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

// Usage reads what the resource-usage view needs and the dashboard does not
// already hold: the machines, and what the metrics API says is running on them.
//
// Neither half is required. Metrics are optional (SPEC 23) and a
// namespace-scoped user often may not list nodes, so both failures come back
// as a reason to show rather than as an error — the view is still worth
// drawing from the pod specs alone.
func (f *Factory) Usage(
	ctx context.Context,
	contextName string,
	opts workloads.Options,
) (usage.Live, error) {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return usage.Live{}, err
	}

	var (
		wg   sync.WaitGroup
		live usage.Live
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Nodes are cluster-scoped: the namespace bounds the pods, never the
		// machines they run on.
		nodes, err := workloads.CollectNodes(ctx, cs, workloads.Options{
			PageSize: opts.PageSize, MaxPages: opts.MaxPages,
		})
		if err != nil {
			live.NodesReason = metrics.Reason(err)
			return
		}
		live.Nodes = nodes
	}()
	go func() {
		defer wg.Done()
		snapshot, err := metrics.Collect(ctx, cs.CoreV1().RESTClient(), opts.Namespace)
		if err != nil {
			live.Metrics = usage.Metrics{Reason: metrics.Reason(err)}
			return
		}
		live.Metrics = fromMetrics(snapshot)
	}()
	wg.Wait()

	return live, nil
}

// fromMetrics converts samples into the domain's own terms. Every value that
// arrived is marked as present, so a measured zero stays distinguishable from
// a node nothing measured.
func fromMetrics(s metrics.Snapshot) usage.Metrics {
	out := usage.Metrics{Available: true, Note: s.Missing, At: s.At}
	for _, node := range s.Nodes {
		if out.Window == 0 {
			out.Window = node.Window
		}
		out.Nodes = append(out.Nodes, usage.NodeSample{
			Name: node.Name,
			Used: application.Amounts{
				CPUMilli: node.CPUMilli, MemoryBytes: node.MemoryBytes,
				HasCPU: true, HasMemory: true,
			},
		})
	}
	for _, pod := range s.Pods {
		out.Pods = append(out.Pods, usage.PodSample{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Used: application.Amounts{
				CPUMilli: pod.CPUMilli, MemoryBytes: pod.MemoryBytes,
				HasCPU: true, HasMemory: true,
			},
		})
	}
	return out
}
