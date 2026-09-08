//go:build correlux_demo

package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	"github.com/aronk11/correlux/internal/domain/usage"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/kubeconfig"
	"github.com/aronk11/correlux/internal/kube/logs"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/theme"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// BrowserDemo drives the real Model and renderer with in-memory cluster data.
// No tea.Cmd is EVER executed: Kubernetes, subprocesses, files, clipboard and
// release checks cannot be reached. This adapter is absent from native builds.
type BrowserDemo struct {
	wrapped           *pendingAction
	requestedReplicas int32
	m                 *Model
	replicas          map[string]int32
	recovered         map[string]bool
	deleted           map[string]bool
	context           string
	namespace         string
}

type DemoInput struct {
	Replace bool    `json:"replace"`
	Key     string  `json:"key"`
	Screen  string  `json:"screen"`
	Text    string  `json:"text"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Click   *[2]int `json:"click"`
}
type DemoFrame struct {
	ANSI    string `json:"ansi"`
	View    int    `json:"view"`
	Overlay int    `json:"overlay"`
	Context string `json:"context"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

func NewBrowserDemo() *BrowserDemo {
	cfg := config.Default()
	cfg.Theme = config.ThemeDark
	cfg.Refresh.Auto = false
	cfg.Fleet = []string{"prod-eu", "staging", "dev-local"}
	kc := &kubeconfig.Config{CurrentContext: "prod-eu", Sources: []string{"demo kubeconfig (in memory)"}}
	for _, name := range cfg.Fleet {
		kc.Contexts = append(kc.Contexts, kubeconfig.Context{Name: name, Cluster: name, Namespace: "default", Server: "https://" + name + ".example.invalid", Production: name == "prod-eu"})
	}
	m := New(Options{Config: cfg, Kubeconfig: kc, Factory: kubeclient.New(kc.Raw(), kc.LoadingRules(), kubeclient.Options{}), Classifier: kubeconfig.DefaultClassifier(), ContextName: "prod-eu", Env: theme.MapEnv(map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor", "LANG": "en_US.UTF-8"})})
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	d := &BrowserDemo{m: m, replicas: map[string]int32{}, recovered: map[string]bool{}, deleted: map[string]bool{}}
	d.hydrate(true)
	return d
}

func (d *BrowserDemo) Step(in DemoInput) DemoFrame {
	m := d.m
	if in.Width > 0 {
		m.width = max(60, min(in.Width, 180))
		m.height = max(20, min(in.Height, 60))
		m.applyLayout()
	}
	m.message = ""
	if in.Screen != "" {
		d.navigate(in.Screen)
	}
	if in.Replace {
		if m.overlay == overlayNone && !m.searching {
			m.openOverlay(overlayPalette)
		}
		m.Update(keyPress("ctrl+u"))
	}
	if in.Text != "" {
		for _, r := range []rune(in.Text) {
			m.Update(keyPress(string(r)))
		}
	}
	if in.Click != nil {
		m.handleClick(tea.MouseClickMsg{X: in.Click[0], Y: in.Click[1], Button: tea.MouseLeft})
	}
	if m.overlay == overlayPrompt {
		d.requestedReplicas, _ = parseReplicas(m.promptInput.Value())
		if d.requestedReplicas > 20 && in.Key == "enter" {
			m.promptError = "Demo limit: 0–20 replicas. The installed app uses your cluster limits."
			in.Key = ""
		}
	}
	if in.Key != "" {
		if m.overlay == overlayNone && !m.searching && (in.Key == "x" || in.Key == "e" || in.Key == "c" || in.Key == "q" || in.Key == "ctrl+c") {
			switch in.Key {
			case "x":
				m.notice("Demo: a real shell needs an installed Correlux and your cluster. Explore logs with l.", theme.StatusUnknown)
			case "e":
				if m.view == viewFleet {
					m.Update(keyPress(in.Key))
				} else {
					m.notice("Demo: external YAML editors are available in the installed terminal app. Press y to inspect YAML.", theme.StatusUnknown)
				}
			case "c":
				m.notice("Demo: select terminal text to copy it with your browser.", theme.StatusUnknown)
			default:
				m.notice("Demo session stays open. Use Reset demo to start again.", theme.StatusUnknown)
			}
		} else {
			m.Update(keyPress(in.Key))
		}
	}
	// Installing a simulated callback preserves the production challenge and
	// confirmation UI while making the only possible mutation local fixture data.
	if m.pending != nil && m.pending != d.wrapped {
		d.wrapped = m.pending
		title := m.pending.Title
		ref, _ := m.targetRef()
		n := d.requestedReplicas
		m.pending.Run = func(m *Model) tea.Cmd {
			key := d.fixtureKey(ref.Name)
			if strings.HasPrefix(title, "Scale ") {
				d.replicas[key] = int32(n)
			}
			if strings.HasPrefix(title, "Restart ") {
				d.recovered[key] = true
			}
			if strings.HasPrefix(title, "Delete ") {
				d.deleted[key] = true
			}
			m.notice("Simulated: "+title+". Only demo data changed; Reset demo restores it.", theme.StatusHealthy)
			d.hydrate(true)
			return nil
		}
	}
	if m.quitting {
		m.quitting = false
		m.notice("Demo session stays open. Use Reset demo to start again.", theme.StatusUnknown)
	}
	d.hydrate(false)
	return DemoFrame{ANSI: m.View().Content, View: int(m.view), Overlay: int(m.overlay), Context: m.contextName, Width: m.screen.Width, Height: m.screen.Height}
}

func (d *BrowserDemo) navigate(screen string) {
	m := d.m
	m.cancelPending()
	m.cancelPrompt()
	m.closeOverlay()
	m.searching = false
	switch screen {
	case "apps":
		m.backToApplications()
	case "application", "why", "logs", "yaml", "object", "scale", "restart", "delete":
		m.backToApplications()
		m.clearSearch()
		m.openApplication("payments")
		d.hydrate(false)
		switch screen {
		case "why":
			m.explain()
		case "logs":
			m.openLogs()
		case "object", "yaml":
			m.openObject(objectRef{Kind: "Deployment", Name: "payments", Namespace: m.namespace})
			d.hydrate(false)
			if screen == "yaml" {
				m.objectYAML = true
			}
		case "scale":
			m.scaleTarget()
		case "restart":
			m.restartTarget()
		case "delete":
			m.deleteTarget()
		}
	case "resources":
		m.openOverlay(overlayResources)
	case "pods":
		m.openResource("pods")
	case "usage":
		if m.view != viewUsage {
			m.openUsage()
		}
	case "events":
		if m.view != viewActivity {
			m.openActivity()
		}
	case "fleet":
		if m.view != viewFleet {
			m.openFleet()
		}
	case "fleet-resources":
		m.openFleetResourceByName("pods")
	case "session":
		m.backToOverview()
	case "commands":
		m.openOverlay(overlayPalette)
	case "clusters":
		m.openOverlay(overlayContexts)
	case "namespaces":
		m.openOverlay(overlayNamespaces)
	case "fleet-clusters":
		m.openFleetPicker(m.activeFleetGroup)
	case "fleet-namespaces":
		m.openFleetNamespacePicker()
	case "help":
		m.openOverlay(overlayHelp)
	}
}

func (d *BrowserDemo) fixtureKey(name string) string {
	return d.m.contextName + "/" + d.m.namespace + "/" + name
}

func (d *BrowserDemo) hydrate(force bool) {
	m := d.m
	changed := d.context != m.contextName || d.namespace != m.scopeLabel()
	force = force || changed
	d.context, d.namespace = m.contextName, m.scopeLabel()
	if force || m.cluster.State() != async.Ready {
		m.Update(clusterProbedMsg{gen: m.cluster.Generation(), info: kubeclient.ClusterInfo{State: kubeclient.ConnOK, ServerVersion: "v1.34.1", Latency: 18 * time.Millisecond}})
	}
	if force || m.namespaces.State() != async.Ready {
		m.Update(namespacesLoadedMsg{gen: m.namespaces.Generation(), list: kubeclient.NamespaceList{Names: []string{"default", "payments", "platform", "kube-system"}}})
	}
	if force || m.catalog.State() != async.Ready {
		m.Update(catalogLoadedMsg{gen: m.catalog.Generation(), catalog: demoCatalog()})
	}
	apps, snapshot := d.applications()
	if force || m.apps.State() != async.Ready {
		m.Update(applicationsLoadedMsg{gen: m.apps.Generation(), list: applicationList{Apps: apps, Snapshot: snapshot}})
	}
	if force || m.evidence.State() != async.Ready {
		m.Update(evidenceLoadedMsg{gen: m.evidence.Generation(), context: d.evidence(apps)})
	}
	if m.view == viewTable && (force || m.table.State() != async.Ready) {
		m.Update(tableLoadedMsg{gen: m.table.Generation(), table: d.table(m.resource.Kind(), apps)})
	}
	if m.view == viewObject && (force || m.object.State() != async.Ready) {
		m.Update(objectLoadedMsg{gen: m.object.Generation(), object: d.object(m.objectTarget)})
	}
	if m.view == viewUsage && (force || m.usage.State() != async.Ready) {
		live := usage.Live{Nodes: d.evidence(apps).Nodes, Metrics: usage.Metrics{Available: true, At: time.Now(), Window: 30 * time.Second}}
		for _, n := range live.Nodes {
			live.Metrics.Nodes = append(live.Metrics.Nodes, usage.NodeSample{Name: n.Name, Used: application.Amounts{HasCPU: true, HasMemory: true, CPUMilli: 1600, MemoryBytes: 3 << 30}})
		}
		for _, p := range snapshot.Pods {
			live.Metrics.Pods = append(live.Metrics.Pods, usage.PodSample{Name: p.Name, Namespace: p.Namespace, Used: application.Amounts{HasCPU: true, HasMemory: true, CPUMilli: 120, MemoryBytes: 96 << 20}})
		}
		m.Update(usageLoadedMsg{gen: m.usage.Generation(), live: live})
	}
	if m.view == viewFleet {
		for i := range m.fleetMembers {
			member := &m.fleetMembers[i]
			member.State = fleet.Ready
			member.Applications = apps
			member.Err = nil
		}
	}
	if m.view == viewFleetResource && (len(m.fleetTable.Rows) == 0 || force) {
		var parts []resources.Part
		for _, ctx := range m.fleetContexts() {
			parts = append(parts, resources.Part{Source: ctx, Table: d.table(m.fleetResource.Kind(), apps)})
		}
		m.fleetTable = resources.Merge(parts, m.fleetResource.Namespaced)
		m.fleetPending = 0
	}
	if m.view == viewLogs && len(m.logLines) == 0 {
		for i := 0; i < 35; i++ {
			text := fmt.Sprintf("INFO request completed method=GET path=/health duration=%dms", 12+i%7)
			if strings.Contains(m.logTitle, "payments") {
				text = "ERROR allocation failed: memory limit exceeded; worker shutting down"
			}
			m.logLines = append(m.logLines, logs.Line{Source: m.logTargets[0], Text: text, At: time.Now().Add(time.Duration(i-35) * time.Second)})
		}
		m.logClosed = true
	}
	m.appsLoading = false
	m.evidenceLoading = false
	m.clusterLoading = false
	m.tableLoading = false
	m.objectLoading = false
	m.usageLoading = false
	m.rebuildCommands()
	commands := m.registry.Commands()
	for i := range commands {
		switch commands[i].Action {
		case paletteExec, paletteEdit, paletteCopy, paletteCopyYAML, paletteCopyJSON, paletteCopyKubectl, paletteCopyLogs, paletteCopyTable, paletteReloadConfig, paletteCheckUpdate:
			commands[i].Enabled = false
			commands[i].DisabledReason = "requires installed app"
		}
	}
	m.registry.Set(commands)
	m.cmdPal.Refresh()
}

func demoCatalog() *kubediscovery.Catalog {
	c := &kubediscovery.Catalog{Failures: map[string]string{}}
	for _, r := range [][4]string{{"", "v1", "pods", "Pod"}, {"apps", "v1", "deployments", "Deployment"}, {"", "v1", "services", "Service"}, {"networking.k8s.io", "v1", "ingresses", "Ingress"}, {"", "v1", "nodes", "Node"}, {"", "v1", "configmaps", "ConfigMap"}, {"", "v1", "secrets", "Secret"}, {"batch", "v1", "jobs", "Job"}, {"batch", "v1", "cronjobs", "CronJob"}, {"", "v1", "persistentvolumeclaims", "PersistentVolumeClaim"}, {"autoscaling", "v2", "horizontalpodautoscalers", "HorizontalPodAutoscaler"}, {"kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "Kustomization"}} {
		c.Resources = append(c.Resources, kubediscovery.Resource{GVR: schema.GroupVersionResource{Group: r[0], Version: r[1], Resource: r[2]}, GVK: schema.GroupVersionKind{Group: r[0], Version: r[1], Kind: r[3]}, Namespaced: r[3] != "Node", Builtin: r[3] != "Kustomization", Scalable: r[3] == "Deployment", Verbs: []string{"get", "list", "watch", "patch", "delete"}})
	}
	return c
}

func (d *BrowserDemo) applications() ([]application.Application, application.Snapshot) {
	ns := d.m.namespace
	if d.m.allNamespaces {
		ns = "default"
	}
	snapshot := application.Snapshot{Scope: ns, FetchedAt: time.Now()}
	var apps []application.Application
	for _, name := range []string{"payments", "worker", "api", "frontend"} {
		if d.deleted[d.fixtureKey(name)] {
			continue
		}
		desired := int32(3)
		if n, ok := d.replicas[d.fixtureKey(name)]; ok {
			desired = n
		}
		ready := desired
		health := application.Healthy
		if d.m.contextName == "prod-eu" && !d.recovered[d.fixtureKey(name)] {
			if name == "payments" {
				ready = 0
				health = application.Down
			}
			if name == "worker" {
				ready = max(desired-1, 0)
				health = application.Degraded
			}
		}
		meta := application.Meta{Kind: "Deployment", Name: name, Namespace: ns, UID: name + "-uid", Labels: map[string]string{"app.kubernetes.io/name": name}, CreatedAt: time.Now().Add(-90 * time.Minute)}
		a := application.Application{Name: name, Namespace: ns, Health: health, ReadyPods: ready, DesiredPods: desired, Summary: fmt.Sprintf("%d of %d pods ready", ready, desired), CreatedAt: meta.CreatedAt}
		a.Workloads = []application.Workload{{Meta: meta, Desired: desired, Ready: ready, Replicated: true, Selector: map[string]string{"app.kubernetes.io/name": name}}}
		for i := int32(0); i < desired; i++ {
			p := application.Pod{Meta: application.Meta{Kind: "Pod", Name: fmt.Sprintf("%s-7d8f-%d", name, i), Namespace: ns, Labels: meta.Labels}, Phase: "Running", Ready: i < ready, Scheduled: true, Node: "node-1", Containers: []application.Container{{Name: name, Image: "ghcr.io/example/" + name + ":1.4", State: "running", Ready: i < ready, Requests: application.Amounts{HasCPU: true, HasMemory: true, CPUMilli: 250, MemoryBytes: 128 << 20}, Limits: application.Amounts{HasCPU: true, HasMemory: true, CPUMilli: 500, MemoryBytes: 256 << 20}}}}
			if !p.Ready {
				p.Reason = "CrashLoopBackOff"
				p.Containers[0].State = "waiting"
				p.Containers[0].Reason = p.Reason
				p.Containers[0].Restarts = 12
				p.Containers[0].OOMKilled = true
				p.Containers[0].LastReason = "OOMKilled"
				p.Containers[0].LastExitCode = 137
			}
			a.Pods = append(a.Pods, p)
			snapshot.Pods = append(snapshot.Pods, p)
		}
		a.Services = []application.Service{{Meta: application.Meta{Kind: "Service", Name: name, Namespace: ns}, Type: "ClusterIP", Ports: []string{"80/TCP"}, Selector: meta.Labels}}
		a.Ingresses = []application.Ingress{{Meta: application.Meta{Kind: "Ingress", Name: name, Namespace: ns}, Hosts: []string{name + ".example.com"}}}
		snapshot.Workloads = append(snapshot.Workloads, a.Workloads...)
		snapshot.Services = append(snapshot.Services, a.Services...)
		snapshot.Ingresses = append(snapshot.Ingresses, a.Ingresses...)
		apps = append(apps, a)
	}
	return apps, snapshot
}

func (d *BrowserDemo) evidence(apps []application.Application) application.Context {
	e := application.Context{}
	for _, a := range apps {
		if a.Health != application.Healthy && len(a.Pods) > 0 {
			e.Events = append(e.Events, application.Event{Meta: application.Meta{Namespace: a.Namespace}, Type: "Warning", Reason: "BackOff", Count: 41, Message: "Back-off restarting failed container " + a.Name, LastSeen: time.Now().Add(-90 * time.Second), About: application.ObjectRef{Kind: "Pod", Name: a.Pods[0].Name}})
		}
	}
	for _, name := range []string{"node-1", "node-2"} {
		cap := application.Capacity{CPUMilli: 4000, MemoryBytes: 8 << 30, Pods: 110}
		e.Nodes = append(e.Nodes, application.Node{Meta: application.Meta{Kind: "Node", Name: name}, Ready: true, Capacity: cap, Allocatable: cap})
	}
	return e
}

func (d *BrowserDemo) table(kind string, apps []application.Application) *resources.Table {
	t := &resources.Table{Columns: []resources.Column{{Name: "Name", Type: "string"}, {Name: "Status", Type: "string"}, {Name: "Ready", Type: "string"}, {Name: "Restarts", Type: "integer"}}, Remaining: -1}
	for _, a := range apps {
		if kind == "Pod" {
			for _, p := range a.Pods {
				status := "Running"
				if p.Reason != "" {
					status = p.Reason
				}
				ready := "1/1"
				if !p.Ready {
					ready = "0/1"
				}
				t.Rows = append(t.Rows, resources.Row{Name: p.Name, Namespace: p.Namespace, Cells: []string{p.Name, status, ready, strconv.Itoa(int(p.Containers[0].Restarts))}})
			}
			continue
		}
		name := a.Name
		if kind == "Node" {
			if len(t.Rows) >= 2 {
				break
			}
			name = "node-" + strconv.Itoa(len(t.Rows)+1)
		}
		t.Rows = append(t.Rows, resources.Row{Name: name, Namespace: a.Namespace, Cells: []string{name, a.Health.String(), fmt.Sprintf("%d/%d", a.ReadyPods, a.DesiredPods), "0"}})
	}
	return t
}

func (d *BrowserDemo) object(ref objectRef) *resources.Object {
	res, _ := demoCatalog().Lookup(ref.lookup())
	replicas := int32(3)
	if n, ok := d.replicas[d.fixtureKey(ref.Name)]; ok {
		replicas = n
	}
	document := map[string]any{"apiVersion": res.GVK.GroupVersion().String(), "kind": ref.Kind, "metadata": map[string]any{"name": ref.Name, "namespace": ref.Namespace, "resourceVersion": "1", "labels": map[string]string{"app.kubernetes.io/name": strings.Split(ref.Name, "-7d8f")[0]}}, "spec": map[string]any{"replicas": replicas, "selector": map[string]any{"matchLabels": map[string]string{"app.kubernetes.io/name": ref.Name}}, "template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": ref.Name, "image": "ghcr.io/example/" + ref.Name + ":1.4"}}}}}}
	if ref.Kind == "Secret" {
		delete(document, "spec")
		document["type"] = "Opaque"
		document["data"] = map[string]string{"example": "ZGVtby1vbmx5"}
	}
	if ref.Kind == "ConfigMap" {
		delete(document, "spec")
		document["data"] = map[string]string{"LOG_LEVEL": "info"}
	}
	raw, _ := json.Marshal(document)
	y, _ := yaml.JSONToYAML(raw)
	return &resources.Object{Target: resources.Target{GVR: res.GVR, Namespaced: res.Namespaced}, Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, UID: ref.Name + "-uid", Raw: raw, YAML: string(y), ResourceVersion: "1", Labels: map[string]string{"app.kubernetes.io/name": strings.Split(ref.Name, "-7d8f")[0]}}
}
