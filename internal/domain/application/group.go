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
			w.GroupedBy = ix.reasonFor(w.Meta)
			g.Workloads = append(g.Workloads, w)
		}
	}

	for i := range s.Pods {
		root := ix.root(s.Pods[i].Meta)
		if g := group(root.Namespace, appName(root)); g != nil {
			p := s.Pods[i]
			p.GroupedBy = ix.reasonFor(p.Meta)
			g.Pods = append(g.Pods, p)
		}
	}

	for i := range s.Services {
		svc := s.Services[i]
		if g, reason := attachService(groups, &svc); g != nil {
			svc.GroupedBy = reason
			g.Services = append(g.Services, svc)
		}
	}

	for _, ing := range s.Ingresses {
		if g, reason := attachIngress(groups, ing); g != nil {
			ing.GroupedBy = reason
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

	// Who deployed this: the workloads carry the mark, and the first one that
	// does speaks for the application.
	for i := range a.Workloads {
		if manager := managerOf(a.Workloads[i].Meta); manager.Known() {
			a.Manager = manager
			break
		}
	}
	if !a.Manager.Known() {
		for i := range a.Pods {
			if manager := managerOf(a.Pods[i].Meta); manager.Known() {
				a.Manager = manager
				break
			}
		}
	}

	a.evaluate()
}

func (a *Application) metas() []Meta {
	out := make([]Meta, 0, len(a.Workloads)+len(a.Pods)+len(a.Services)+len(a.Ingresses))
	for _, w := range a.Workloads {
		out = append(out, w.Meta)
	}
	for i := range a.Pods {
		out = append(out, a.Pods[i].Meta)
	}
	for i := range a.Services {
		out = append(out, a.Services[i].Meta)
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
//
// It returns the Reason alongside the Application so the caller can record why,
// without recomputing which of the two rules actually matched.
func attachService(groups map[string]*Application, svc *Service) (*Application, Reason) {
	if key, value, ok := labelNameKV(svc.Meta); ok {
		if g, exists := groups[svc.Namespace+"/"+value]; exists {
			return g, Reason{Signal: signalForLabel(key), Key: key, Value: value}
		}
	}
	if len(svc.Selector) == 0 {
		return nil, Reason{}
	}
	var best *Application
	bestCount := 0
	for _, g := range groups {
		if g.Namespace != svc.Namespace {
			continue
		}
		count := 0
		for i := range g.Pods {
			if selects(svc.Selector, g.Pods[i].Labels) {
				count++
			}
		}
		// Ties go to the alphabetically first application, so the same cluster
		// always produces the same screen.
		if count > bestCount || (count == bestCount && count > 0 && best != nil && g.Name < best.Name) {
			best, bestCount = g, count
		}
	}
	if best == nil {
		return nil, Reason{}
	}
	return best, Reason{Signal: SignalSelector, Value: renderSelector(svc.Selector)}
}

// attachIngress finds the application an ingress belongs to, by label or
// through the services it routes to.
func attachIngress(groups map[string]*Application, ing Ingress) (*Application, Reason) {
	if key, value, ok := labelNameKV(ing.Meta); ok {
		if g, exists := groups[ing.Namespace+"/"+value]; exists {
			return g, Reason{Signal: signalForLabel(key), Key: key, Value: value}
		}
	}
	for _, backend := range ing.Backends {
		for _, g := range groups {
			if g.Namespace != ing.Namespace {
				continue
			}
			for i := range g.Services {
				if g.Services[i].Name == backend {
					return g, Reason{Signal: SignalBackend, Value: backend}
				}
			}
		}
	}
	return nil, Reason{}
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

// reasonFor explains why m belongs to whatever application it lands in. It
// follows exactly the cascade root and appName follow together — ownership
// first, then the label conventions in the same order labelNameKV reads them
// — so the answer can never disagree with the grouping it explains.
func (ix *index) reasonFor(m Meta) Reason {
	seen := map[string]bool{m.UID: true}
	cur := m
	var chain []string
	// unresolved carries the one owner reference a walk of zero hops still
	// found: the object names its owner, even though that owner was not in
	// the snapshot to walk any further.
	var unresolved string
	for depth := 0; depth < maxOwnerDepth; depth++ {
		ref, ok := cur.Controller()
		if !ok || ref.UID == "" || seen[ref.UID] {
			break
		}
		next, ok := ix.metas[ref.UID]
		if !ok {
			if len(chain) == 0 {
				unresolved = ref.Kind + "/" + ref.Name
			}
			break
		}
		chain = append(chain, ref.Kind+"/"+ref.Name)
		seen[ref.UID] = true
		cur = next
	}
	if len(chain) > 0 {
		return Reason{Signal: SignalOwner, Chain: chain}
	}
	// A label on the object itself outranks an owner reference that could not
	// be followed, exactly as appName reads root's own label before falling
	// back to root's controller.
	if key, value, ok := labelNameKV(m); ok {
		return Reason{Signal: signalForLabel(key), Key: key, Value: value}
	}
	if unresolved != "" {
		return Reason{Signal: SignalOwner, Chain: []string{unresolved}}
	}
	return Reason{Signal: SignalNone}
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

// The labels and annotations Helm, Flux and Argo CD write on what they create.
// Recognising them is the whole of Correlux's support for those tools: nothing
// is installed, nothing is asked of them, and an application says who manages
// it because the objects already say so.
const (
	helmManagedBy       = "app.kubernetes.io/managed-by"
	helmReleaseName     = "meta.helm.sh/release-name"
	helmReleaseNS       = "meta.helm.sh/release-namespace"
	fluxKustomizeName   = "kustomize.toolkit.fluxcd.io/name"
	fluxKustomizeNS     = "kustomize.toolkit.fluxcd.io/namespace"
	fluxHelmReleaseName = "helm.toolkit.fluxcd.io/name"
	fluxHelmReleaseNS   = "helm.toolkit.fluxcd.io/namespace"
	argoInstance        = "argocd.argoproj.io/instance"
)

// managerOf reads who deployed an object.
//
// Flux is checked before Helm because Flux deploys *through* Helm: a workload
// created by a Flux HelmRelease carries both sets of marks, and the object
// worth opening is the HelmRelease, not the release Helm happens to have made
// on its behalf.
func managerOf(m Meta) Manager {
	if name := m.Labels[fluxHelmReleaseName]; name != "" {
		return Manager{Tool: "Flux", Kind: "HelmRelease", Name: name,
			Namespace: firstNonEmpty(m.Labels[fluxHelmReleaseNS], m.Namespace)}
	}
	if name := m.Labels[fluxKustomizeName]; name != "" {
		return Manager{Tool: "Flux", Kind: "Kustomization", Name: name,
			Namespace: firstNonEmpty(m.Labels[fluxKustomizeNS], m.Namespace)}
	}
	if name := m.Annotations[argoInstance]; name != "" {
		return Manager{Tool: "Argo CD", Kind: "Application", Name: name}
	}
	if strings.EqualFold(m.Labels[helmManagedBy], "Helm") {
		return Manager{Tool: "Helm", Name: m.Annotations[helmReleaseName],
			Namespace: firstNonEmpty(m.Annotations[helmReleaseNS], m.Namespace)}
	}
	return Manager{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// labelName reads the application name off the conventional labels.
func labelName(m Meta) string {
	_, value, _ := labelNameKV(m)
	return value
}

// labelNameKV is labelName plus the key that matched, so a caller can say
// which convention decided — a bare "app" label and an instance label read
// the same value but must never carry the same weight (ADR 16).
func labelNameKV(m Meta) (key, value string, ok bool) {
	for _, k := range groupLabels {
		if v := strings.TrimSpace(m.Labels[k]); v != "" {
			return k, v, true
		}
	}
	return "", "", false
}

// signalForLabel names which Signal a matched label key stands for.
func signalForLabel(key string) Signal {
	switch key {
	case "app.kubernetes.io/instance":
		return SignalInstanceLabel
	case "app.kubernetes.io/name":
		return SignalNameLabel
	case "k8s-app":
		return SignalK8sAppLabel
	default: // "app"
		return SignalAppLabel
	}
}

// renderSelector renders a selector as one short, deterministic string: keys
// sorted, so the same selector always reads the same way regardless of Go's
// map ordering. Selectors are already small by construction — a handful of
// match keys, never a whole label map — so this stays within the same memory
// budget as everything else Reason carries.
func renderSelector(selector map[string]string) string {
	keys := make([]string, 0, len(selector))
	for k := range selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+selector[k])
	}
	return strings.Join(parts, ",")
}
