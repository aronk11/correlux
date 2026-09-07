package client

import (
	"context"
	"sync"

	"k8s.io/client-go/kubernetes"

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

// ApplicationsIn reads a few named namespaces of one cluster and groups them as
// one answer.
//
// The namespaces are read one after another rather than all at once: each pass
// is already nine concurrent list calls, and a fleet of thirty clusters must
// not turn a three-namespace scope into a burst of requests nobody asked for
// (ADR 19). No namespaces means the whole cluster, which is what the fleet has
// always done.
//
// A namespace that cannot be read is recorded as a gap rather than failing the
// cluster: in a fleet, one cluster denying one namespace is normal, and the
// other namespaces still have something to say. Only a cluster where nothing
// at all could be read is an error.
func (f *Factory) ApplicationsIn(
	ctx context.Context,
	contextName string,
	namespaces []string,
	opts workloads.Options,
) ([]application.Application, application.Snapshot, error) {
	if len(namespaces) == 0 {
		return f.Applications(ctx, contextName, opts)
	}
	cs, err := f.Clientset(contextName)
	if err != nil {
		return nil, application.Snapshot{}, err
	}
	return applicationsIn(ctx, cs, namespaces, opts)
}

func applicationsIn(
	ctx context.Context,
	cs kubernetes.Interface,
	namespaces []string,
	opts workloads.Options,
) ([]application.Application, application.Snapshot, error) {
	snapshots := make([]application.Snapshot, 0, len(namespaces))
	var (
		denied   []application.Gap
		firstErr error
	)
	for _, namespace := range namespaces {
		scoped := opts
		scoped.Namespace = namespace
		snapshot, err := workloads.Collect(ctx, cs, scoped)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			denied = append(denied, application.Gap{
				Kind:   application.WholeScopeKind,
				Reason: workloads.GapReason(err),
				Scope:  namespace,
			})
			continue
		}
		// Which namespace a kind was denied in is the whole of the fact when
		// several were read at once.
		for i := range snapshot.Gaps {
			snapshot.Gaps[i].Scope = namespace
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil, application.Snapshot{}, firstErr
	}

	merged := application.MergeSnapshots(snapshots...)
	merged.Gaps = append(merged.Gaps, denied...)
	return application.Group(merged), merged, nil
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
