package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
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
	if cmd := m.openDebug("toolbox"); cmd != nil {
		t.Fatal("unexpected async operation")
	}
	if m.overlay != overlayPrompt || m.promptTitle != "Namespace for troubleshooting" {
		t.Fatal("silently selected a namespace")
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
