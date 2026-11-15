package workloads

import (
	"context"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aronk11/correlux/internal/domain/application"
)

// CollectContext reads the evidence a diagnosis reasons about: what the cluster
// said (events), what a service actually routes to (endpoints), where the pods
// are running (nodes), whether their storage is bound (claims) and what the
// last rollout of each Deployment changed (revisions).
//
// It is a separate pass from Collect on purpose. The dashboard refreshes on a
// timer and has to stay cheap, while this is fetched at the moment somebody
// asks why one application is broken (ADR 6, ADR 18).
func CollectContext(ctx context.Context, cs kubernetes.Interface, opts Options) (application.Context, error) {
	out := application.Context{Scope: opts.Namespace, FetchedAt: time.Now()}
	var g group

	// Events are paged like everything else. The API server returns them in
	// the order of their keys — namespace, then name — not by time, so one
	// page of them is the events of whatever sorts first: in a busy scope the
	// pod that broke a minute ago had none, and its explanation fell back to
	// "Kubernetes has not said why" while the reason sat on page two. When
	// even the page budget is not enough, that is said rather than hidden.
	eventsTruncated := false
	g.run("Event", func() (bool, error) {
		truncated, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.CoreV1().Events(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Events = append(out.Events, fromEvent(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
		g.collect(func() { eventsTruncated = truncated })
		return truncated, err
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
				if out.Endpoints[i].Namespace != out.Endpoints[j].Namespace {
					return out.Endpoints[i].Namespace < out.Endpoints[j].Namespace
				}
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

	// ReplicaSets again, this time with their templates: the dashboard reads
	// them only to connect pods to Deployments, and the question here is the
	// one the dashboard never asks — what did the last rollout change?
	g.run("ReplicaSet", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.AppsV1().ReplicaSets(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			revisions := make([]application.Revision, 0, len(l.Items))
			for i := range l.Items {
				if r, ok := fromReplicaSet(&l.Items[i]); ok {
					revisions = append(revisions, r)
				}
			}
			g.collect(func() { out.Revisions = append(out.Revisions, revisions...) })
			return l.Continue, nil
		})
	})

	collectEcosystem(ctx, &g, cs, opts, &out)
	collectConfigReferences(ctx, &g, cs, opts, &out)

	gaps, _, err := g.wait()
	if err != nil {
		return application.Context{}, err
	}
	out.Gaps = gaps
	if eventsTruncated {
		out.Gaps = append(out.Gaps, application.Gap{Kind: "Event",
			Reason: "there are more than were read, so a cause may be missing"})
	}
	out.Revisions = pruneRevisions(out.Revisions)
	return out, nil
}

// maxConfigMaps bounds how many ConfigMaps one evidence pass reads. They are
// fetched one by one, by name, because the alternative — listing every
// ConfigMap in scope with its data — pulls megabytes of other people's
// configuration to find a dozen URLs.
const maxConfigMaps = 40

// collectConfigReferences reads the named ConfigMaps and keeps, of each
// value, only the hosts it points at. A ConfigMap that cannot be read is not
// a gap: the dependency it would have shown is simply not shown.
func collectConfigReferences(ctx context.Context, g *group, cs kubernetes.Interface, opts Options, out *application.Context) {
	names := opts.ConfigMaps
	if len(names) > maxConfigMaps {
		names = names[:maxConfigMaps]
	}
	if len(names) == 0 {
		return
	}
	g.quiet(func() {
		for _, full := range names {
			if ctx.Err() != nil {
				return
			}
			namespace, name, ok := strings.Cut(full, "/")
			if !ok {
				continue
			}
			cm, err := cs.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			var refs []application.ConfigReference
			for key, value := range cm.Data {
				for _, r := range application.ReferencesIn(key, value) {
					refs = append(refs, application.ConfigReference{Namespace: namespace, ConfigMap: name, Key: key, Host: r.Host})
				}
			}
			sort.Slice(refs, func(i, j int) bool { return refs[i].Key+refs[i].Host < refs[j].Key+refs[j].Host })
			g.collect(func() { out.ConfigReferences = append(out.ConfigReferences, refs...) })
		}
	})
}

// addSlice folds one EndpointSlice into its service's counts. A service is
// described by several slices on a large cluster, and what a user needs is the
// one number: how many ready addresses are behind this name.
func addSlice(into map[string]*application.EndpointSet, slice *discoveryv1.EndpointSlice) {
	service := slice.Labels[discoveryv1.LabelServiceName]
	if service == "" {
		return
	}
	// Keyed by namespace as well as name: read across all namespaces, two
	// services called "web" are two services, and folding their slices
	// together reported the healthy one as having no endpoints.
	key := slice.Namespace + "/" + service
	set, ok := into[key]
	if !ok {
		set = &application.EndpointSet{Service: service, Namespace: slice.Namespace}
		into[key] = set
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

// nodeProblemConditions are the node conditions that mean trouble when they
// are true: the kubelet's own, and the ones node-problem-detector sets.
//
// It is a list rather than "everything but Ready" because conditions come in
// both polarities. Managed node agents publish conditions such as
// KernelReady or NetworkingReady that are true on a healthy machine, and
// reading those as pressure put a warning on every application in the
// cluster.
var nodeProblemConditions = map[string]bool{
	"MemoryPressure":              true,
	"DiskPressure":                true,
	"PIDPressure":                 true,
	"NetworkUnavailable":          true,
	"KernelDeadlock":              true,
	"ReadonlyFilesystem":          true,
	"CorruptDockerOverlay2":       true,
	"FrequentKubeletRestart":      true,
	"FrequentDockerRestart":       true,
	"FrequentContainerdRestart":   true,
	"FrequentUnregisterNetDevice": true,
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
		case c.Status == corev1.ConditionTrue && nodeProblemConditions[string(c.Type)]:
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
