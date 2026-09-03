package workloads

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aronk11/correlux/internal/domain/application"
)

// eventLimit bounds how many events one pass reads. A busy namespace produces
// thousands of them and only the most recent ones explain anything; the rest
// are history the cluster will drop within the hour anyway.
const eventLimit = int64(500)

// CollectContext reads the evidence a diagnosis reasons about: what the cluster
// said (events), what a service actually routes to (endpoints), where the pods
// are running (nodes) and whether their storage is bound (claims).
//
// It is a separate pass from Collect on purpose. The dashboard refreshes on a
// timer and has to stay cheap, while this is fetched at the moment somebody
// asks why one application is broken (ADR 6, ADR 18).
func CollectContext(ctx context.Context, cs kubernetes.Interface, opts Options) (application.Context, error) {
	out := application.Context{Scope: opts.Namespace, FetchedAt: time.Now()}
	var g group

	g.run("Event", func() (bool, error) {
		// One page only: events are ordered by nothing in particular, and
		// paging through a week of them to explain a pod that broke a minute
		// ago is work nobody asked for.
		l, err := cs.CoreV1().Events(opts.Namespace).List(ctx, metav1.ListOptions{Limit: eventLimit})
		if err != nil {
			return false, err
		}
		g.collect(func() {
			for i := range l.Items {
				out.Events = append(out.Events, fromEvent(&l.Items[i]))
			}
		})
		return l.Continue != "", nil
	})

	g.run("EndpointSlice", func() (bool, error) {
		slices := map[string]*application.EndpointSet{}
		truncated, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.DiscoveryV1().EndpointSlices(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			for i := range l.Items {
				addSlice(slices, &l.Items[i])
			}
			return l.Continue, nil
		})
		if err != nil {
			return false, err
		}
		g.collect(func() {
			for _, set := range slices {
				out.Endpoints = append(out.Endpoints, *set)
			}
			sort.Slice(out.Endpoints, func(i, j int) bool {
				return out.Endpoints[i].Service < out.Endpoints[j].Service
			})
		})
		return truncated, nil
	})

	g.run("Node", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.CoreV1().Nodes().List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Nodes = append(out.Nodes, fromNode(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
	})

	g.run("PersistentVolumeClaim", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.CoreV1().PersistentVolumeClaims(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Claims = append(out.Claims, fromClaim(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
	})

	gaps, _, err := g.wait()
	if err != nil {
		return application.Context{}, err
	}
	out.Gaps = gaps
	return out, nil
}

// addSlice folds one EndpointSlice into its service's counts. A service is
// described by several slices on a large cluster, and what a user needs is the
// one number: how many ready addresses are behind this name.
func addSlice(into map[string]*application.EndpointSet, slice *discoveryv1.EndpointSlice) {
	service := slice.Labels[discoveryv1.LabelServiceName]
	if service == "" {
		return
	}
	set, ok := into[service]
	if !ok {
		set = &application.EndpointSet{Service: service, Namespace: slice.Namespace}
		into[service] = set
	}
	for _, endpoint := range slice.Endpoints {
		// Conditions are three-valued: a nil Ready means the publisher does not
		// track readiness, which the API defines as ready.
		if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
			set.Ready += len(endpoint.Addresses)
			continue
		}
		set.NotReady += len(endpoint.Addresses)
	}
}

func fromEvent(e *corev1.Event) application.Event {
	out := application.Event{
		Meta:    meta("Event", e.ObjectMeta),
		Type:    e.Type,
		Reason:  e.Reason,
		Message: e.Message,
		Count:   e.Count,
		About: application.ObjectRef{
			Kind: e.InvolvedObject.Kind,
			Name: e.InvolvedObject.Name,
			UID:  string(e.InvolvedObject.UID),
		},
	}
	switch {
	case !e.LastTimestamp.IsZero():
		out.LastSeen = e.LastTimestamp.Time
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		out.LastSeen = e.Series.LastObservedTime.Time
	case !e.EventTime.IsZero():
		out.LastSeen = e.EventTime.Time
	default:
		out.LastSeen = e.CreationTimestamp.Time
	}
	if out.Count == 0 {
		out.Count = 1
	}
	return out
}

func fromNode(n *corev1.Node) application.Node {
	out := application.Node{
		Meta:          meta("Node", n.ObjectMeta),
		Unschedulable: n.Spec.Unschedulable,
	}
	for _, c := range n.Status.Conditions {
		switch {
		case c.Type == corev1.NodeReady:
			out.Ready = c.Status == corev1.ConditionTrue
			if !out.Ready {
				out.Reason, out.Message = c.Reason, c.Message
			}
		case c.Status == corev1.ConditionTrue:
			// Every other node condition is a problem when it is true.
			out.Pressure = append(out.Pressure, string(c.Type))
		}
	}
	out.Capacity = capacityOf(n.Status.Capacity)
	out.Allocatable = capacityOf(n.Status.Allocatable)
	return out
}

// capacityOf reads a node's resource list. A node that does not report one of
// the three leaves it at zero, which the usage view renders as unknown rather
// than as a full machine.
func capacityOf(list corev1.ResourceList) application.Capacity {
	out := application.Capacity{}
	if cpu, ok := list[corev1.ResourceCPU]; ok {
		out.CPUMilli = cpu.MilliValue()
	}
	if mem, ok := list[corev1.ResourceMemory]; ok {
		out.MemoryBytes = mem.Value()
	}
	if pods, ok := list[corev1.ResourcePods]; ok {
		out.Pods = pods.Value()
	}
	return out
}

func fromClaim(c *corev1.PersistentVolumeClaim) application.Claim {
	claim := application.Claim{
		Meta:   meta("PersistentVolumeClaim", c.ObjectMeta),
		Phase:  string(c.Status.Phase),
		Volume: c.Spec.VolumeName,
	}
	if c.Spec.StorageClassName != nil {
		claim.StorageClass = *c.Spec.StorageClassName
	}
	return claim
}

// CollectNodes reads just the nodes.
//
// The fleet overview needs them and nothing else from the evidence pass: a
// broken node is the most common thing that is wrong with a cluster and has
// nothing to do with any one application, so asking for the whole evidence
// bundle per member would be paying for eight kinds to learn about one.
func CollectNodes(ctx context.Context, cs kubernetes.Interface, opts Options) ([]application.Node, error) {
	var out []application.Node
	_, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
		list, err := cs.CoreV1().Nodes().List(ctx, o)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			out = append(out, fromNode(&list.Items[i]))
		}
		return list.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CollectClaims reads just the PersistentVolumeClaims, the same shape as
// CollectNodes: the fleet overview reads storage alongside nodes, and an
// unbound claim belongs to no application either.
func CollectClaims(ctx context.Context, cs kubernetes.Interface, opts Options) ([]application.Claim, error) {
	var out []application.Claim
	_, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
		list, err := cs.CoreV1().PersistentVolumeClaims(opts.Namespace).List(ctx, o)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			out = append(out, fromClaim(&list.Items[i]))
		}
		return list.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CollectEndpoints reads just the EndpointSlices, rolled up per service the
// same way CollectContext does. A Service with a published slice and no ready
// address is the most common way a healthy-looking application serves
// nothing, and it is invisible to anything that only reads workloads and pods.
func CollectEndpoints(ctx context.Context, cs kubernetes.Interface, opts Options) ([]application.EndpointSet, error) {
	slices := map[string]*application.EndpointSet{}
	_, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
		l, err := cs.DiscoveryV1().EndpointSlices(opts.Namespace).List(ctx, o)
		if err != nil {
			return "", err
		}
		for i := range l.Items {
			addSlice(slices, &l.Items[i])
		}
		return l.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]application.EndpointSet, 0, len(slices))
	for _, set := range slices {
		out = append(out, *set)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
}
