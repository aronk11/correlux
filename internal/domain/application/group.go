package application

import (
	"sort"
	"strings"
)

// groupLabels name the application an object belongs to, in the order
// Kubernetes' own conventions give them. The instance label comes first
// because it identifies one *installation* — a Helm release, an Argo
// application — which is what an operator means by "the payments app", while
// the name label identifies the software and would merge two independent
// installations of the same chart into one row.
var groupLabels = []string{
	"app.kubernetes.io/instance",
	"app.kubernetes.io/name",
	"app",
	"k8s-app",
}

// maxOwnerDepth bounds the owner walk. Kubernetes' own chains are two links
// long (Pod, ReplicaSet, Deployment); a custom controller may add one or two.
// A malformed or cyclic chain must cost a few map lookups, never a hang.
const maxOwnerDepth = 8

// Group turns a snapshot into applications.
//
// The rules, in the order they are applied:
//
//  1. Every workload is walked up its owner references to the controller that
//     is not itself owned — a ReplicaSet resolves to its Deployment, a Job to
//     its CronJob — and that root names an application.
//  2. Pods join the application of the workload that owns them. A pod with no
//     resolvable owner falls back to its labels, so a bare pod still shows up
//     rather than disappearing.
//  3. Services join by label, or by their selector actually matching the pods
//     of an application. Ingresses join by label, or through the service they
//     route to.
//
// Objects that match nothing are left out: they remain fully visible in the
// resource browser, and inventing an "application" for a stray ConfigMap would
// bury the ones that matter.
func Group(s Snapshot) []Application {
	ix := newIndex(s)
	groups := map[string]*Application{}

	group := func(namespace, name string) *Application {
		if name == "" {
			return nil
		}
		key := namespace + "/" + name
		if g, ok := groups[key]; ok {
			return g
		}
		g := &Application{Name: name, Namespace: namespace}
		groups[key] = g
		return g
	}

	for _, w := range s.Workloads {
		root := ix.root(w.Meta)
		if g := group(root.Namespace, appName(root)); g != nil {
			g.Workloads = append(g.Workloads, w)
		}
	}

	for _, p := range s.Pods {
		root := ix.root(p.Meta)
		if g := group(root.Namespace, appName(root)); g != nil {
			g.Pods = append(g.Pods, p)
		}
	}

	for _, svc := range s.Services {
		if g := attachService(groups, svc); g != nil {
			g.Services = append(g.Services, svc)
		}
	}

	for _, ing := range s.Ingresses {
		if g := attachIngress(groups, ing); g != nil {
			g.Ingresses = append(g.Ingresses, ing)
		}
	}

	out := make([]Application, 0, len(groups))
	for _, g := range groups {
		g.finish()
		out = append(out, *g)
	}
	sortApplications(out)
	return out
}

// sortApplications puts the worst first: the dashboard's job is to answer
// "what is broken?" before "what exists?" (SPEC 1).
func sortApplications(apps []Application) {
	sort.Slice(apps, func(i, j int) bool {
		a, b := apps[i], apps[j]
		if a.Health.severity() != b.Health.severity() {
			return a.Health.severity() > b.Health.severity()
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}

// finish sorts an application's members and derives its health.
func (a *Application) finish() {
	sort.Slice(a.Workloads, func(i, j int) bool { return a.Workloads[i].Name < a.Workloads[j].Name })
	sort.Slice(a.Pods, func(i, j int) bool { return a.Pods[i].Name < a.Pods[j].Name })
	sort.Slice(a.Services, func(i, j int) bool { return a.Services[i].Name < a.Services[j].Name })
	sort.Slice(a.Ingresses, func(i, j int) bool { return a.Ingresses[i].Name < a.Ingresses[j].Name })

	for _, m := range a.metas() {
		if m.CreatedAt.IsZero() {
			continue
		}
		if a.CreatedAt.IsZero() || m.CreatedAt.Before(a.CreatedAt) {
			a.CreatedAt = m.CreatedAt
		}
	}
	a.evaluate()
}

func (a *Application) metas() []Meta {
	out := make([]Meta, 0, len(a.Workloads)+len(a.Pods)+len(a.Services)+len(a.Ingresses))
	for _, w := range a.Workloads {
		out = append(out, w.Meta)
	}
	for _, p := range a.Pods {
		out = append(out, p.Meta)
	}
	for _, s := range a.Services {
		out = append(out, s.Meta)
	}
	for _, i := range a.Ingresses {
		out = append(out, i.Meta)
	}
	return out
}

// attachService finds the application a service belongs to: its label first,
// then the pods its selector actually matches. A service whose selector matches
// nothing is a real and common failure, but it is not evidence of a *different*
// application, so it stays out of the list rather than inventing one.
func attachService(groups map[string]*Application, svc Service) *Application {
	if g, ok := groups[svc.Namespace+"/"+labelName(svc.Meta)]; ok && labelName(svc.Meta) != "" {
		return g
	}
	if len(svc.Selector) == 0 {
		return nil
	}
	var best *Application
	bestCount := 0
	for _, g := range groups {
		if g.Namespace != svc.Namespace {
			continue
		}
		count := 0
		for _, p := range g.Pods {
			if selects(svc.Selector, p.Labels) {
				count++
			}
		}
		// Ties go to the alphabetically first application, so the same cluster
		// always produces the same screen.
		if count > bestCount || (count == bestCount && count > 0 && best != nil && g.Name < best.Name) {
			best, bestCount = g, count
		}
	}
	return best
}

// attachIngress finds the application an ingress belongs to, by label or
// through the services it routes to.
func attachIngress(groups map[string]*Application, ing Ingress) *Application {
	if g, ok := groups[ing.Namespace+"/"+labelName(ing.Meta)]; ok && labelName(ing.Meta) != "" {
		return g
	}
	for _, backend := range ing.Backends {
		for _, g := range groups {
			if g.Namespace != ing.Namespace {
				continue
			}
			for _, svc := range g.Services {
				if svc.Name == backend {
					return g
				}
			}
		}
	}
	return nil
}

// selects reports whether a label selector matches a label set, using the
// equality semantics a Service selector has.
func selects(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// index resolves owner references within a snapshot.
type index struct {
	metas map[string]Meta
}

func newIndex(s Snapshot) *index {
	ix := &index{metas: make(map[string]Meta, len(s.Workloads)+len(s.Owners))}
	for _, w := range s.Workloads {
		if w.UID != "" {
			ix.metas[w.UID] = w.Meta
		}
	}
	for _, o := range s.Owners {
		if o.UID != "" {
			ix.metas[o.UID] = o
		}
	}
	return ix
}

// root walks an object up to the controller that owns everything else. It stops
// at the first owner the snapshot does not contain — a ReplicaSet the user may
// not list, a custom controller outside the scope — and the caller falls back
// to labels, which is exactly what a human does in the same situation.
func (ix *index) root(m Meta) Meta {
	seen := map[string]bool{m.UID: true}
	for depth := 0; depth < maxOwnerDepth; depth++ {
		ref, ok := m.Controller()
		if !ok || ref.UID == "" || seen[ref.UID] {
			break
		}
		next, ok := ix.metas[ref.UID]
		if !ok {
			break
		}
		seen[ref.UID] = true
		m = next
	}
	return m
}

// appName names the application an object belongs to.
func appName(m Meta) string {
	if name := labelName(m); name != "" {
		return name
	}
	// An unresolved controller names the group better than the object does:
	// "payments-7d8f" beats "payments-7d8f-2xk9l" for a pod whose ReplicaSet
	// was not readable.
	if ref, ok := m.Controller(); ok && ref.Name != "" {
		return ref.Name
	}
	return m.Name
}

// labelName reads the application name off the conventional labels.
func labelName(m Meta) string {
	for _, key := range groupLabels {
		if v := strings.TrimSpace(m.Labels[key]); v != "" {
			return v
		}
	}
	return ""
}
