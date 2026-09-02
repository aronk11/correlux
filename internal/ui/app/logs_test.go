package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/logs"
)

// feedLogs delivers a batch the way the reader goroutine would.
func feedLogs(m *Model, events ...logs.Event) {
	m.Update(logBatchMsg{gen: m.logGeneration, events: events})
}

func line(pod, text string) logs.Event {
	return logs.Event{Line: logs.Line{Source: logs.Source{Namespace: "default", Pod: pod}, Text: text}}
}

// openPodLogs opens an application, selects its first pod and reads its logs.
func openPodLogs(t *testing.T, m *Model) {
	t.Helper()
	app := testApplication("payments", application.Degraded, 2, 3)
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{
			Apps:     []application.Application{app},
			Snapshot: application.Snapshot{Workloads: app.Workloads, Pods: app.Pods},
		},
	})
	press(t, m, "enter") // open the application
	press(t, m, "down")  // onto the first pod
	press(t, m, "l")
}

func TestLogsOpenForThePodUnderTheCursor(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	if m.view != viewLogs {
		t.Fatalf("l must open the logs, got view %v", m.view)
	}
	if len(m.logTargets) != 1 || !strings.HasPrefix(m.logTargets[0].Pod, "payments-") {
		t.Fatalf("targets = %+v, want the selected pod", m.logTargets)
	}

	feedLogs(m, line(m.logTargets[0].Pod, "listening on :8080"))
	if out := plainView(m); !strings.Contains(out, "listening on :8080") {
		t.Errorf("the output must be on screen:\n%s", out)
	}
}

func TestAWorkloadReadsEveryPodItOwns(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("payments", application.Healthy, 3, 3)
	for i := range app.Pods {
		app.Pods[i].Labels = map[string]string{"app": "payments"}
	}
	app.Workloads[0].Selector = map[string]string{"app": "payments"}
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{
			Apps:     []application.Application{app},
			Snapshot: application.Snapshot{Workloads: app.Workloads, Pods: app.Pods},
		},
	})
	press(t, m, "enter") // the workload row is selected first
	press(t, m, "l")

	if len(m.logTargets) != 3 {
		t.Fatalf("a Deployment's logs are its pods' logs, got %+v", m.logTargets)
	}
	// With more than one source, each line says which pod it came from.
	feedLogs(m, line(app.Pods[0].Name, "from the first"), line(app.Pods[1].Name, "from the second"))
	out := plainView(m)
	if !strings.Contains(out, app.Pods[0].Name) || !strings.Contains(out, "from the second") {
		t.Errorf("every line must be attributed:\n%s", out)
	}
}

func TestASourceThatCannotBeReadIsShownWithoutSilencingTheRest(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	feedLogs(m,
		line(m.logTargets[0].Pod, "still running"),
		logs.Event{
			Err:    errors.New("container \"payments\" is waiting to start"),
			Source: logs.Source{Namespace: "default", Pod: "payments-7d8f-2"},
		},
	)

	out := plainView(m)
	if !strings.Contains(out, "still running") {
		t.Errorf("the readable source must keep going:\n%s", out)
	}
	if !strings.Contains(out, "waiting to start") {
		t.Errorf("the unreadable one must be stated:\n%s", out)
	}
	if !strings.Contains(out, "unreadable:") {
		t.Errorf("the header must say something is missing:\n%s", out)
	}
}

func TestFollowingCanBePausedAndTheHeaderSaysWhich(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	if !strings.Contains(plainView(m), "following") {
		t.Errorf("a log opens following:\n%s", plainView(m))
	}
	press(t, m, "f")
	if out := plainView(m); !strings.Contains(out, "paused") {
		t.Errorf("f must pause, and say so:\n%s", out)
	}
	if m.cancelLogs != nil {
		t.Error("a paused log must not keep a connection open")
	}
}

func TestScrollingBackStopsFollowing(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)
	for i := 0; i < 200; i++ {
		feedLogs(m, line(m.logTargets[0].Pod, "line "+itoa(i)))
	}

	press(t, m, "up")
	if m.logFollow {
		t.Error("scrolling back is a statement that the user wants to read what is there")
	}
	press(t, m, "end")
	if !m.logFollow {
		t.Error("End must resume following")
	}
}

func TestTheOldestLinesAreDroppedAndSaidSo(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	for i := 0; i < maxLogLines+50; i++ {
		feedLogs(m, line(m.logTargets[0].Pod, "line "+itoa(i)))
	}

	if len(m.logLines) > maxLogLines {
		t.Errorf("kept %d lines, want at most %d", len(m.logLines), maxLogLines)
	}
	if out := plainView(m); !strings.Contains(out, "older lines dropped") {
		t.Errorf("dropping output must be admitted:\n%s", out)
	}
}

func TestTimestampsAndThePreviousRunReopenTheStream(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)
	gen := m.logGeneration

	press(t, m, "t")
	if !m.logTimestamps || m.logGeneration == gen {
		t.Error("asking for timestamps must reopen the stream")
	}
	if out := plainView(m); !strings.Contains(out, "timestamps") {
		t.Errorf("the header must say so:\n%s", out)
	}

	press(t, m, "p")
	if !m.logPrevious {
		t.Fatal("p must switch to the previous run")
	}
	if m.logFollow {
		t.Error("a previous run writes nothing more; following it is meaningless")
	}
	if out := plainView(m); !strings.Contains(out, "previous run") {
		t.Errorf("the header must say which run is shown:\n%s", out)
	}
}

func TestLeavingTheLogsStopsTheStream(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	press(t, m, "esc")
	if m.view == viewLogs {
		t.Fatal("esc must leave the log view")
	}
	if m.cancelLogs != nil {
		t.Error("the stream must be cancelled when the user leaves")
	}
}

func TestSwitchingScopeStopsTheStream(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)

	m.switchNamespace("other")
	if m.cancelLogs != nil || m.view == viewLogs {
		t.Errorf("a log belongs to the scope it was opened in: view %v", m.view)
	}
}

func TestAStaleBatchIsIgnored(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)
	feedLogs(m, line(m.logTargets[0].Pod, "current"))

	// A batch from a stream the user has already left.
	m.Update(logBatchMsg{gen: m.logGeneration - 1, events: []logs.Event{line("old", "from before")}})

	if out := plainView(m); strings.Contains(out, "from before") {
		t.Errorf("output from an abandoned stream must not appear:\n%s", out)
	}
}

func TestTimestampsAreRenderedWhenTheyArrive(t *testing.T) {
	m := newTestModel(t)
	openPodLogs(t, m)
	press(t, m, "t")

	at := time.Date(2026, 9, 2, 14, 30, 5, 250000000, time.UTC)
	m.Update(logBatchMsg{gen: m.logGeneration, events: []logs.Event{{
		Line: logs.Line{
			Source: logs.Source{Namespace: "default", Pod: m.logTargets[0].Pod},
			Text:   "ready", At: at,
		},
	}}})

	if out := plainView(m); !strings.Contains(out, at.Local().Format("15:04:05")) {
		t.Errorf("the line's own time must be shown:\n%s", out)
	}
}
