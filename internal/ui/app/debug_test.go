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
