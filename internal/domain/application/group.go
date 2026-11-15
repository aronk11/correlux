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
//
// Between the recommended labels and the loose ones sit the labels operators
// write on what they create. They are narrower than "app" — a controller sets
// them on exactly the objects of one cluster, pipeline or service — and
// without them the things those operators run fall apart on the dashboard:
// every Knative revision, every Tekton TaskRun and every Spark executor would
// be an application of its own.
var groupLabels = []string{
	"app.kubernetes.io/instance",
	"app.kubernetes.io/name",

	"cnpg.io/cluster", // CloudNativePG
	"postgres-operator.crunchydata.com/cluster", // Crunchy PGO
	"strimzi.io/cluster",                        // Strimzi Kafka
	"serving.knative.dev/service",               // Knative: one service, many revisions
	"tekton.dev/pipeline",                       // Tekton: every run of one pipeline
	"workflows.argoproj.io/workflow",            // Argo Workflows
	"sparkoperator.k8s.io/app-name",             // Spark operator: driver and executors
	"spark-app-name",
	"ray.io/cluster",                    // KubeRay: head and workers
	"training.kubeflow.org/job-name",    // Kubeflow training jobs
	"leaderworkerset.sigs.k8s.io/name",  // LeaderWorkerSet
	"actions.github.com/scale-set-name", // GitHub Actions runner scale sets

	"app",
	"k8s-app",
	"run", // what `kubectl run` writes
}

// operatorLabels are the entries of groupLabels an operator wrote rather than
// a person, for naming the signal that matched.
var operatorLabels = map[string]bool{
	"cnpg.io/cluster": true,
	"postgres-operator.crunchydata.com/cluster": true,
	"strimzi.io/cluster":                        true,
	"serving.knative.dev/service":               true,
	"tekton.dev/pipeline":                       true,
	"workflows.argoproj.io/workflow":            true,
	"sparkoperator.k8s.io/app-name":             true,
	"spark-app-name":                            true,
	"ray.io/cluster":                            true,
	"training.kubeflow.org/job-name":            true,
	"leaderworkerset.sigs.k8s.io/name":          true,
	"actions.github.com/scale-set-name":         true,
	staticPodComponent:                          true,
}

// staticPodComponent is the label kubeadm writes on the control plane's static
// pods, and the only thing that names them: their owner is the Node they run
// on, and their own name has that node's name appended.
const staticPodComponent = "component"

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

	for i := range s.Workloads {
		root := ix.root(s.Workloads[i].Meta)
		if g := group(root.Namespace, appName(root)); g != nil {
			w := s.Workloads[i]
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

	for i := range s.Ingresses {
		ing := s.Ingresses[i]
		if g, reason := attachIngress(groups, &ing); g != nil {
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

	a.readTraits()
	a.evaluate()
}

func (a *Application) metas() []Meta {
	out := make([]Meta, 0, len(a.Workloads)+len(a.Pods)+len(a.Services)+len(a.Ingresses))
	for i := range a.Workloads {
		out = append(out, a.Workloads[i].Meta)
	}
	for i := range a.Pods {
		out = append(out, a.Pods[i].Meta)
	}
	for i := range a.Services {
		out = append(out, a.Services[i].Meta)
	}
	for i := range a.Ingresses {
		out = append(out, a.Ingresses[i].Meta)
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
		// No pod to match: the application is scaled to zero, or so broken that
		// it has no pods at all — which is exactly when somebody opens it and
		// wants to see the service in front of it. A workload whose own pod
		// selector contains every pair the service selects on can only ever
		// make pods the service matches, so the relationship holds without one.
		for _, g := range groups {
			if g.Namespace != svc.Namespace {
				continue
			}
			for i := range g.Workloads {
				if !selects(svc.Selector, g.Workloads[i].Selector) {
					continue
				}
				if best == nil || g.Name < best.Name {
					best = g
				}
				break
			}
		}
	}
	if best == nil {
		return nil, Reason{}
	}
	return best, Reason{Signal: SignalSelector, Value: renderSelector(svc.Selector)}
}

// attachIngress finds the application an ingress belongs to, by label or
// through the services it routes to.
func attachIngress(groups map[string]*Application, ing *Ingress) (*Application, Reason) {
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
	for i := range s.Workloads {
		if w := &s.Workloads[i]; w.UID != "" {
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
	if name, ok := staticPodName(m); ok {
		if m.Labels[staticPodComponent] == name {
			return Reason{Signal: SignalOperatorLabel, Key: staticPodComponent, Value: name}
		}
		return Reason{Signal: SignalNone}
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
	if name, ok := staticPodName(m); ok {
		return name
	}
	if name := labelName(m); name != "" {
		return name
	}
	// An unresolved controller names the group better than the object does:
	// "payments-7d8f" beats "payments-7d8f-2xk9l" for a pod whose ReplicaSet
	// was not readable.
	if ref, ok := m.Controller(); ok && ref.Name != "" {
		// And "payments" beats both. A Deployment's ReplicaSet is named after
		// it plus the pod template hash, and the pod carries that hash as a
		// label — so an account that may list pods but not ReplicaSets still
		// gets its pods under the Deployment they belong to instead of beside
		// it, one application per revision.
		if hash := m.Labels["pod-template-hash"]; hash != "" && ref.Kind == "ReplicaSet" {
			if base := strings.TrimSuffix(ref.Name, "-"+hash); base != "" && base != ref.Name {
				return base
			}
		}
		return ref.Name
	}
	return m.Name
}

// staticPodName names a static pod: the mirror of something a kubelet runs
// from a file on its node, which is how kubeadm runs the control plane.
//
// Such a pod is "owned" by its Node, and following that reference like any
// other would file the API server, the scheduler and etcd together under an
// application named after a machine. What it is called is its component label
// where kubeadm set one, and otherwise its own name without the node's.
// Either way the three API servers of an HA control plane come out as one
// application with three pods, which is what they are.
func staticPodName(m Meta) (string, bool) {
	ref, ok := m.Controller()
	if !ok || ref.Kind != "Node" || m.Kind != "Pod" {
		return "", false
	}
	if component := strings.TrimSpace(m.Labels[staticPodComponent]); component != "" {
		return component, true
	}
	if base := strings.TrimSuffix(m.Name, "-"+ref.Name); base != "" {
		return base, true
	}
	return m.Name, true
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
	case "run":
		return SignalRunLabel
	default:
		if operatorLabels[key] {
			return SignalOperatorLabel
		}
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
