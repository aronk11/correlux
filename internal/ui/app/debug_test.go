package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/kube/debug"
	"github.com/aronk11/correlux/internal/kube/resources"
)

func TestDebugCreationStopsAtTheExistingGate(t *testing.T) {
	m := newTestModel(t)
	job, err := debug.Job(debug.Options{Namespace: "team", Image: "busybox:1.37.0", Mode: "toolbox", Seconds: 900})
	if err != nil {
		t.Fatal(err)
	}
	if cmd := m.confirmDebugJob(job); cmd != nil {
		t.Fatal("creation ran before confirmation")
	}
	if m.pending == nil || m.overlay != overlayConfirm {
		t.Fatal("missing confirmation")
	}
	text := strings.Join(m.pending.Lines, " ")
	for _, want := range []string{"team", "busybox:1.37.0", "900s", "10 minutes", "identity can differ"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s", want)
		}
	}
	m.cancelPending()
	if m.pending != nil {
		t.Fatal("cancel retained job creation")
	}
}

func TestDebugSessionsPreserveServerPagingAndTargets(t *testing.T) {
	m := newTestModel(t)
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "Debug sessions", Name: "sessions", Resource: debugResource}
	raw := []byte(`{"metadata":{"continue":"next-token"},"items":[{"metadata":{"name":"probe","namespace":"team","annotations":{"correlux.dev/debug-mode":"http"}},"status":{"conditions":[{"type":"Failed","status":"True","reason":"DeadlineExceeded"}]}}]}`)
	loadObjectInto(m, &resources.Object{Raw: raw})
	d, refs := m.objectView()
	if len(refs) != 2 || refs[0].Namespace != "team" || refs[0].Resource != "jobs.batch" || refs[1].Continuation != "next-token" {
		t.Fatalf("wrong refs: %+v", refs)
	}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), "DeadlineExceeded") {
		t.Fatal("failed probe looked successful")
	}
}

func TestDebugRequiresExplicitNamespaceInAllNamespacesView(t *testing.T) {
	m := newTestModel(t)
	m.allNamespaces = true
	m.view = viewApplications
	// The only thing that may start is listing namespaces to choose from.
	m.openDebug("toolbox")
	if m.pending != nil {
		t.Fatal("nothing may be created before a namespace is chosen")
	}
	if m.overlay != overlayNamespaces || m.nsPicker.Title != "Namespace for troubleshooting" {
		t.Fatal("silently selected a namespace")
	}
}

func loadNamespaceList(m *Model, names ...string) {
	m.Update(namespacesLoadedMsg{gen: m.namespaces.Generation(), list: kubeclient.NamespaceList{Names: names}})
}

func TestTheTroubleshootingNamespaceIsPickedFromTheList(t *testing.T) {
	m := newTestModel(t)
	m.allNamespaces = true
	m.view = viewApplications
	m.openDebug("dns")
	loadNamespaceList(m, "kube-system", "payments", "shop")

	if hasTitle(m.nsPicker.Items(), "All namespaces") {
		t.Fatal("one namespace is needed; offering all of them is offering nothing")
	}
	typeInto(t, m, "shp")
	if item, ok := m.nsPicker.Selected(); !ok || item.ID != "shop" {
		t.Fatalf("a fuzzy filter must find shop from shp, got %+v", item)
	}
	press(t, m, "enter")
	if m.overlay != overlayPrompt || !strings.Contains(m.promptNote, "shop") || !m.allNamespaces {
		t.Fatalf("overlay %v, note %q, scope must not change (all=%v)", m.overlay, m.promptNote, m.allNamespaces)
	}
}

func TestCancellingAChoiceLeavesTheScopeSwitcherAsItWas(t *testing.T) {
	m := newTestModel(t)
	m.allNamespaces = true
	m.openDebug("toolbox")
	press(t, m, "esc")
	if m.nsChoice != nil || m.nsPicker.Title != "Namespaces" {
		t.Fatalf("choice %v, title %q", m.nsChoice, m.nsPicker.Title)
	}
	m.openOverlay(overlayNamespaces)
	loadNamespaceList(m, "shop")
	if !hasTitle(m.nsPicker.Items(), "All namespaces") {
		t.Fatal("the scope switcher must still offer all namespaces")
	}
}

func TestCompletedDebugPodsRemainReadableThroughJobLogs(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("probe", application.Healthy, 1, 1)
	app.Workloads[0].Kind = "Job"
	app.Workloads[0].Selector = map[string]string{"job-name": "probe"}
	app.Pods[0].Labels = map[string]string{"job-name": "probe"}
	app.Pods[0].Phase = "Succeeded"
	m.Update(applicationsLoadedMsg{gen: m.apps.Generation(), list: applicationList{Apps: []application.Application{app}, Snapshot: application.Snapshot{Workloads: app.Workloads, Pods: app.Pods}}})
	ref := objectRef{Kind: "Job", Name: app.Workloads[0].Name, Namespace: app.Workloads[0].Namespace}
	if sources, _, ok := m.sourcesFor(ref); !ok || len(sources) != 1 {
		t.Fatal("completed probe logs disappeared")
	}
}

func TestAirGappedDebugPromptsRequireAnExplicitImage(t *testing.T) {
	for _, mode := range []string{"toolbox", "dns", "http", "tcp"} {
		t.Run(mode, func(t *testing.T) {
			m := newTestModel(t, func(o *Options) { o.Config.AirGapped = true; o.Config.Debug.ImagePullPolicy = "Never" })
			m.promptDebugDestination(mode, "team")
			if mode != "toolbox" {
				destination := map[string]string{"dns": "api.team.svc", "http": "https://api.team.svc", "tcp": "api.team.svc:443"}[mode]
				m.promptAccept(m, destination)
			}
			if m.promptInput.Value() != "" {
				t.Fatal("public image was prefilled")
			}
			m.promptAccept(m, "")
			if m.pending != nil || m.promptError == "" {
				t.Fatal("empty image reached confirmation")
			}
			m.promptAccept(m, "registry.internal/toolbox:latest")
			if m.pending == nil {
				t.Fatal("explicit image did not reach confirmation")
			}
			lines := strings.Join(m.pending.Lines, " ")
			if !strings.Contains(lines, "registry.internal/toolbox:latest") || !strings.Contains(lines, "Image pull policy: Never") {
				t.Fatal(lines)
			}
		})
	}
}

func TestAirGappedImagesAndPolicyDefaults(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.Config.AirGapped = true })
	if policy, err := m.debugPullPolicy(); err != nil || policy != "IfNotPresent" {
		t.Fatalf("policy=%s err=%v", policy, err)
	}
	m.cfg.Debug.ToolboxImage = "registry.internal/busybox:1"
	m.cfg.Debug.CurlImage = "registry.internal/curl:1"
	m.cfg.Debug.NetworkImage = "registry.internal/netshoot:1"
	for mode, want := range map[string]string{"toolbox": m.cfg.Debug.ToolboxImage, "dns": m.cfg.Debug.ToolboxImage, "http": m.cfg.Debug.CurlImage, "tcp": m.cfg.Debug.NetworkImage} {
		if got := m.debugImage(mode); got != want {
			t.Fatalf("%s image = %s", mode, got)
		}
	}
	m.cfg.Debug.ImagePullPolicy = "Typo"
	if cmd := m.promptDebugDestination("toolbox", "team"); cmd == nil || m.overlay == overlayPrompt || !strings.Contains(m.message, "imagePullPolicy") {
		t.Fatal("invalid policy opened creation flow")
	}
}

func TestAirGappedEphemeralImageHasNoPublicDefault(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.Config.AirGapped = true; o.Config.Debug.ImagePullPolicy = "Never" })
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "Pod", Name: "api", Namespace: "team", Resource: "pods"}
	loadObjectInto(m, &resources.Object{Raw: []byte(`{"metadata":{"uid":"uid","resourceVersion":"42"},"spec":{"containers":[{"name":"app","image":"app"}]},"status":{"phase":"Running"}}`)})
	m.promptEphemeral()
	m.promptAccept(m, "app")
	if m.promptInput.Value() != "" {
		t.Fatal("public ephemeral image was prefilled")
	}
	m.promptAccept(m, "registry.internal/toolbox:1")
	if m.pending == nil || !strings.Contains(strings.Join(m.pending.Lines, " "), "Image pull policy: Never") {
		t.Fatal("ephemeral confirmation lost pull policy")
	}
}

func TestAirGappedMirrorPrefillsReviewedDebugImage(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config.AirGapped = true
		o.Config.Debug.RegistryMirror = "mirror.internal/dockerhub"
	})
	m.promptDebugDestination("toolbox", "team")
	want := "mirror.internal/dockerhub/library/busybox:1.37.0"
	if m.promptInput.Value() != want {
		t.Fatalf("prompt image = %q", m.promptInput.Value())
	}
	m.promptAccept(m, m.promptInput.Value())
	if m.pending == nil || !strings.Contains(strings.Join(m.pending.Lines, " "), "Image: "+want) {
		t.Fatal("mirror image was not shown for confirmation")
	}
	// Ephemeral containers use exactly the same configured default.
	m.cancelPending()
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "Pod", Name: "api", Namespace: "team", Resource: "pods"}
	loadObjectInto(m, &resources.Object{Raw: []byte(`{"spec":{"containers":[{"name":"app","image":"app"}]}}`)})
	m.promptEphemeral()
	m.promptAccept(m, "app")
	if m.promptInput.Value() != want {
		t.Fatalf("ephemeral prompt image = %q", m.promptInput.Value())
	}
}

func runningPodObject(t *testing.T, m *Model) {
	t.Helper()
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "Pod", Name: "payments-0", Namespace: "shop", Resource: "pods"}
	raw := []byte(`{"kind":"Pod","metadata":{"name":"payments-0","namespace":"shop","uid":"u1","resourceVersion":"7"},
		"spec":{"containers":[{"name":"payments","image":"registry/payments:1.4"}]},"status":{"phase":"Running"}}`)
	loadObjectInto(m, &resources.Object{Kind: "Pod", Name: "payments-0", Namespace: "shop", Raw: raw})
}

func TestChecksFromAPodAreOfferedOnThePod(t *testing.T) {
	m := newTestModel(t)
	runningPodObject(t, m)
	titles := map[string]bool{}
	for _, c := range m.debugCommands() {
		titles[c.Title] = true
	}
	for _, want := range []string{"Check DNS from this pod", "Check TCP from this pod", "Check HTTP from this pod", "Inspect a TLS certificate from this pod", "Inspect a TLS certificate"} {
		if !titles[want] {
			t.Errorf("missing %q in %v", want, titles)
		}
	}
}

func TestACheckFromAPodIsConfirmedWithItsIdentityAndCommand(t *testing.T) {
	m := newTestModel(t)
	runningPodObject(t, m)
	if cmd := m.openDebug("probe:tls"); cmd != nil {
		t.Fatal("nothing may run before the destination is entered")
	}
	if m.overlay != overlayPrompt || m.promptInput.Value() != "https://" {
		t.Fatalf("overlay %v, prefill %q", m.overlay, m.promptInput.Value())
	}
	if cmd := m.promptAccept(m, "http://insecure.example"); cmd != nil || m.promptError == "" {
		t.Fatal("a TLS check needs an https URL")
	}
	if cmd := m.promptAccept(m, "https://pay.example.com"); cmd != nil {
		t.Fatal("the check ran before confirmation")
	}
	if m.pending == nil || m.overlay != overlayConfirm {
		t.Fatal("missing confirmation")
	}
	text := strings.Join(m.pending.Lines, " ")
	for _, want := range []string{"curl", "--verbose", "https://pay.example.com", "pod payments-0 (shop)", "NetworkPolicies", "curlimages/curl"} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation must say %q:\n%s", want, text)
		}
	}
}

func TestAProbeOpensItsOwnOutput(t *testing.T) {
	m := newTestModel(t)
	runningPodObject(t, m)
	m.logPrevious = true
	m.Update(probeStartedMsg{cluster: m.contextName, ref: m.objectTarget, name: "correlux-probe-abc"})
	if m.view != viewLogs || len(m.logTargets) != 1 || m.logTargets[0].Container != "correlux-probe-abc" || m.logPrevious {
		t.Fatalf("view %v, targets %+v, previous %v", m.view, m.logTargets, m.logPrevious)
	}
}

func nodeShellTarget(m *Model) {
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "Node", Name: "node-1", Resource: "nodes"}
	loadObjectInto(m, &resources.Object{Kind: "Node", Name: "node-1", Raw: []byte(`{"kind":"Node","metadata":{"name":"node-1"}}`)})
}

func TestTheNodeShellIsOffUntilTheConfigurationEnablesIt(t *testing.T) {
	m := newTestModel(t)
	nodeShellTarget(m)
	for _, c := range m.debugCommands() {
		if c.Arg == "node" {
			t.Fatal("a root shell on a node must be opt-in")
		}
	}
	if cmd := m.openDebug("node"); cmd != nil || m.overlay != overlayNone {
		t.Fatal("the action must do nothing while disabled")
	}

	m.cfg.Debug.NodeShell = true
	found := false
	for _, c := range m.debugCommands() {
		found = found || c.Arg == "node"
	}
	if !found {
		t.Fatal("enabled, it must be offered on a node")
	}
	m.openDebug("node")
	loadNamespaceList(m, "default", "kube-system")
	if m.overlay != overlayNamespaces {
		t.Fatalf("overlay %v, want the namespace picker", m.overlay)
	}
	if item, ok := m.nsPicker.Selected(); !ok || item.ID != "kube-system" {
		t.Fatalf("kube-system must be suggested, got %+v", item)
	}
	press(t, m, "enter")
	if m.pending == nil {
		t.Fatal("picking the namespace must lead to the confirmation")
	}
	text := strings.Join(m.pending.Lines, " ")
	for _, want := range []string{"PRIVILEGED", "root on node-1", "/host", "chroot /host", "30 minutes"} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation must say %q:\n%s", want, text)
		}
	}
}
