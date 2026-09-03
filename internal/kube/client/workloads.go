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

// FleetExtras is what the fleet overview reads for a member besides its
// applications: the things that belong to no application at all.
type FleetExtras struct {
	Nodes    []application.Node
	NodesErr error
	Claims   []application.Claim
	// ClaimsErr and EndpointsErr are kept apart from NodesErr because a scoped
	// service account routinely may read one kind and not another; a member
	// that cannot list claims is not thereby a member that cannot list nodes.
	ClaimsErr    error
	Endpoints    []application.EndpointSet
	EndpointsErr error
}

// FleetExtras reads a cluster's nodes, storage and service health in one
// bounded, concurrent pass.
//
// It is deliberately one more call alongside Applications rather than three:
// a fleet of thirty contexts must not turn "what else is wrong" into ninety
// extra round trips, and each of the three listings fails on its own so one
// missing permission does not blank out the other two (ADR 19).
func (f *Factory) FleetExtras(ctx context.Context, contextName string) FleetExtras {
	cs, err := f.Clientset(contextName)
	if err != nil {
		return FleetExtras{NodesErr: err, ClaimsErr: err, EndpointsErr: err}
	}

	var out FleetExtras
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		out.Nodes, out.NodesErr = workloads.CollectNodes(ctx, cs, workloads.Options{})
	}()
	go func() {
		defer wg.Done()
		out.Claims, out.ClaimsErr = workloads.CollectClaims(ctx, cs, workloads.Options{})
	}()
	go func() {
		defer wg.Done()
		out.Endpoints, out.EndpointsErr = workloads.CollectEndpoints(ctx, cs, workloads.Options{})
	}()
	wg.Wait()
	return out
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
