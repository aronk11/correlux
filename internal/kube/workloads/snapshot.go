// Package workloads collects the objects an application view is derived from.
//
// It is the adapter between client-go and the pure domain: one bounded,
// cancellable pass over a scope, converting Kubernetes objects into the small
// structs internal/domain/application reasons about. Nothing here decides what
// an application is, and nothing in the domain knows that client-go exists.
//
// Two properties matter more than completeness:
//
//   - A kind that cannot be read is a gap, not a failure. Service accounts are
//     routinely allowed to list pods but not ingresses, and an application list
//     is still worth having without them.
//   - The pass is bounded. A namespace with fifty thousand pods must cost a
//     known number of requests, and say that it was truncated, rather than
//     pulling the cluster into memory (ADR 6).
package workloads

import (
	"context"
	"strconv"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// Paging bounds. The page size is what one request costs; the page count is how
// much of a very large scope kubeui reads before it says "this is a subset".
const (
	DefaultPageSize = int64(500)
	DefaultMaxPages = 10
)

// Options selects what to collect.
type Options struct {
	// Namespace scopes the collection; empty means every namespace.
	Namespace string
	// PageSize and MaxPages bound the work. Zero means the defaults.
	PageSize int64
	MaxPages int
}

func (o Options) pageSize() int64 {
	if o.PageSize > 0 {
		return o.PageSize
	}
	return DefaultPageSize
}

func (o Options) maxPages() int {
	if o.MaxPages > 0 {
		return o.MaxPages
	}
	return DefaultMaxPages
}

// Collect reads a scope and returns the snapshot the domain groups.
//
// Every kind is fetched concurrently: the pass costs one round trip's latency
// rather than nine, which is the difference between a dashboard that appears
// and one that is waited for. An error is returned only when nothing at all
// could be read — that is an unreachable cluster, and saying "no applications"
// would be a lie.
func Collect(ctx context.Context, cs kubernetes.Interface, opts Options) (application.Snapshot, error) {
	snap := application.Snapshot{Scope: opts.Namespace, FetchedAt: time.Now()}

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		firstErr  error
		attempted int
	)

	// collect runs one kind's listing. Recording the result under the mutex
	// keeps the snapshot free of races without serialising the requests.
	collect := func(kind string, list func(context.Context) (bool, error)) {
		attempted++
		wg.Add(1)
		go func() {
			defer wg.Done()
			truncated, err := list(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				snap.Gaps = append(snap.Gaps, application.Gap{Kind: kind, Reason: gapReason(err)})
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			if truncated {
				snap.Truncated = true
			}
		}()
	}

	add := func(f func()) { mu.Lock(); defer mu.Unlock(); f() }

	collect("Deployment", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.AppsV1().Deployments(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Workloads = append(snap.Workloads, fromDeployment(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("StatefulSet", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.AppsV1().StatefulSets(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Workloads = append(snap.Workloads, fromStatefulSet(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("DaemonSet", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.AppsV1().DaemonSets(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Workloads = append(snap.Workloads, fromDaemonSet(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("CronJob", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.BatchV1().CronJobs(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Workloads = append(snap.Workloads, fromCronJob(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("Job", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.BatchV1().Jobs(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Workloads = append(snap.Workloads, fromJob(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	// ReplicaSets are never shown as workloads: they exist here only so a pod
	// can be walked up to the Deployment that owns it.
	collect("ReplicaSet", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.AppsV1().ReplicaSets(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Owners = append(snap.Owners, meta("ReplicaSet", l.Items[i].ObjectMeta))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("Pod", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.CoreV1().Pods(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Pods = append(snap.Pods, fromPod(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("Service", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.CoreV1().Services(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Services = append(snap.Services, fromService(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	collect("Ingress", func(ctx context.Context) (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, int, error) {
			l, err := cs.NetworkingV1().Ingresses(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", 0, err
			}
			add(func() {
				for i := range l.Items {
					snap.Ingresses = append(snap.Ingresses, fromIngress(&l.Items[i]))
				}
			})
			return l.Continue, len(l.Items), nil
		})
	})

	wg.Wait()

	if len(snap.Gaps) == attempted && firstErr != nil {
		// Nothing at all was readable. That is a broken connection, not a
		// cluster without applications.
		return application.Snapshot{}, firstErr
	}
	sortGaps(snap.Gaps)
	return snap, nil
}

// page walks a resource's pages until the server runs out of them or the budget
// does, and reports whether anything was left behind.
func page(
	ctx context.Context,
	opts Options,
	fetch func(context.Context, metav1.ListOptions) (string, int, error),
) (bool, error) {
	list := metav1.ListOptions{Limit: opts.pageSize()}
	for i := 0; i < opts.maxPages(); i++ {
		next, _, err := fetch(ctx, list)
		if err != nil {
			return false, err
		}
		if next == "" {
			return false, nil
		}
		list.Continue = next
	}
	return true, nil
}

func sortGaps(gaps []application.Gap) {
	for i := 1; i < len(gaps); i++ {
		for j := i; j > 0 && gaps[j].Kind < gaps[j-1].Kind; j-- {
			gaps[j], gaps[j-1] = gaps[j-1], gaps[j]
		}
	}
}

func meta(kind string, m metav1.ObjectMeta) application.Meta {
	out := application.Meta{
		Kind:      kind,
		Name:      m.Name,
		Namespace: m.Namespace,
		UID:       string(m.UID),
		Labels:    m.Labels,
		CreatedAt: m.CreationTimestamp.Time,
	}
	for _, o := range m.OwnerReferences {
		out.Owners = append(out.Owners, application.OwnerRef{
			Kind:       o.Kind,
			Name:       o.Name,
			UID:        string(o.UID),
			Controller: o.Controller != nil && *o.Controller,
		})
	}
	return out
}

func fromDeployment(d *appsv1.Deployment) application.Workload {
	return application.Workload{
		Meta:       meta("Deployment", d.ObjectMeta),
		Desired:    replicas(d.Spec.Replicas),
		Ready:      d.Status.ReadyReplicas,
		Updated:    d.Status.UpdatedReplicas,
		Selector:   matchLabels(d.Spec.Selector),
		Replicated: true,
		Paused:     d.Spec.Paused,
	}
}

func fromStatefulSet(s *appsv1.StatefulSet) application.Workload {
	return application.Workload{
		Meta:       meta("StatefulSet", s.ObjectMeta),
		Desired:    replicas(s.Spec.Replicas),
		Ready:      s.Status.ReadyReplicas,
		Updated:    s.Status.UpdatedReplicas,
		Selector:   matchLabels(s.Spec.Selector),
		Replicated: true,
	}
}

// fromDaemonSet takes its counts from the status: a DaemonSet's "replicas" is
// however many nodes it currently belongs on, which only the cluster knows.
func fromDaemonSet(d *appsv1.DaemonSet) application.Workload {
	return application.Workload{
		Meta:       meta("DaemonSet", d.ObjectMeta),
		Desired:    d.Status.DesiredNumberScheduled,
		Ready:      d.Status.NumberReady,
		Updated:    d.Status.UpdatedNumberScheduled,
		Selector:   matchLabels(d.Spec.Selector),
		Replicated: true,
	}
}

// fromJob and fromCronJob leave Replicated false: a Job's health is the state
// of its pods, and "0 of 1 ready" would report every job that is still running
// as broken.
func fromJob(j *batchv1.Job) application.Workload {
	return application.Workload{
		Meta:      meta("Job", j.ObjectMeta),
		Selector:  matchLabels(j.Spec.Selector),
		Suspended: j.Spec.Suspend != nil && *j.Spec.Suspend,
	}
}

func fromCronJob(c *batchv1.CronJob) application.Workload {
	return application.Workload{
		Meta:      meta("CronJob", c.ObjectMeta),
		Suspended: c.Spec.Suspend != nil && *c.Spec.Suspend,
	}
}

func fromService(s *corev1.Service) application.Service {
	out := application.Service{
		Meta:      meta("Service", s.ObjectMeta),
		Type:      string(s.Spec.Type),
		ClusterIP: s.Spec.ClusterIP,
		Selector:  s.Spec.Selector,
	}
	for _, p := range s.Spec.Ports {
		out.Ports = append(out.Ports, strconv.Itoa(int(p.Port))+"/"+string(p.Protocol))
	}
	return out
}

func fromIngress(i *networkingv1.Ingress) application.Ingress {
	out := application.Ingress{Meta: meta("Ingress", i.ObjectMeta)}
	if b := i.Spec.DefaultBackend; b != nil && b.Service != nil {
		out.Backends = append(out.Backends, b.Service.Name)
	}
	for _, rule := range i.Spec.Rules {
		if rule.Host != "" {
			out.Hosts = append(out.Hosts, rule.Host)
		}
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				out.Backends = append(out.Backends, path.Backend.Service.Name)
			}
		}
	}
	return out
}

func replicas(n *int32) int32 {
	if n == nil {
		// The API server defaults an absent replica count to one.
		return 1
	}
	return *n
}

func matchLabels(s *metav1.LabelSelector) map[string]string {
	if s == nil {
		return nil
	}
	return s.MatchLabels
}
