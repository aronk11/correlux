package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// brokenApplication is one application with a real failure in it: three pods
// crash-looping after being killed for their memory limit.
func brokenApplication() application.Application {
	a := testApplication("payments", application.Down, 0, 3)
	for i := range a.Pods {
		a.Pods[i].Reason = "CrashLoopBackOff"
		a.Pods[i].Containers = []application.Container{{
			Name:         "payments",
			Image:        "registry/payments:1.4",
			State:        "waiting",
			Reason:       "CrashLoopBackOff",
			Restarts:     12,
			LastReason:   "OOMKilled",
			LastExitCode: 137,
			OOMKilled:    true,
		}}
	}
	return a
}

func loadEvidenceInto(m *Model, evidence application.Context) {
	m.Update(evidenceLoadedMsg{gen: m.evidence.Generation(), context: evidence})
}

func TestWhyExplainsTheApplicationUnderTheCursor(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())

	press(t, m, "ctrl+w")
	if m.view != viewWhy {
		t.Fatalf("Ctrl+W must open the explanation, got view %v", m.view)
	}

	out := plainView(m)
	for _, want := range []string{
		"payments",
		"restart in a loop", // the problem
		"memory limit",      // the cause, read from the previous run
		"WHY",               // the section a user is looking for
		"WHAT TO CHECK",     // and what to do next
		"kubectl logs",      // with the command that shows it
		"confidence: high",  // and how sure kubeui is
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the WHY view must contain %q:\n%s", want, out)
		}
	}
}

func TestWhyNamesWhatItHasNotReadYet(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "ctrl+w")

	if out := plainView(m); !strings.Contains(out, "have not been read yet") &&
		!strings.Contains(out, "Reading events") {
		t.Errorf("an explanation without its evidence must say so:\n%s", out)
	}
}

func TestWhyImprovesWhenTheEvidenceArrives(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("payments", application.Degraded, 1, 2)
	// A pod that is simply not ready: only an event can say why.
	for i := range app.Pods {
		app.Pods[i].Reason = ""
		app.Pods[i].Phase = "Running"
		app.Pods[i].Containers = []application.Container{{Name: "payments", State: "running"}}
	}
	app.Pods[1].Ready = false
	loadApplicationsInto(m, app)
	press(t, m, "ctrl+w")

	before := plainView(m)
	if !strings.Contains(before, "not ready") {
		t.Fatalf("the observation must be there without evidence:\n%s", before)
	}

	loadEvidenceInto(m, application.Context{Events: []application.Event{{
		Type: "Warning", Reason: "Unhealthy", Count: 9,
		Message:  "Readiness probe failed: connection refused",
		LastSeen: time.Now().Add(-90 * time.Second),
		About:    application.ObjectRef{Kind: "Pod", Name: app.Pods[1].Name, UID: app.Pods[1].UID},
	}}})

	after := plainView(m)
	if !strings.Contains(after, "connection refused") {
		t.Errorf("once the events are in, the probe's own words must appear:\n%s", after)
	}
	if !strings.Contains(after, "confidence: high") {
		t.Errorf("quoting the cluster raises the confidence:\n%s", after)
	}
}

func TestWhyOnAHealthyApplicationSaysThereIsNothing(t *testing.T) {
	m := newTestModel(t)
	healthy := testApplication("api", application.Healthy, 2, 2)
	for i := range healthy.Pods {
		healthy.Pods[i].Containers = []application.Container{{Name: "api", State: "running", Ready: true}}
	}
	loadApplicationsInto(m, healthy)
	loadEvidenceInto(m, application.Context{})

	press(t, m, "ctrl+w")
	if out := plainView(m); !strings.Contains(out, "Nothing is wrong with api") {
		t.Errorf("a healthy application must get a plain answer, not an empty screen:\n%s", out)
	}
}

func TestWhyFailsLoudlyRatherThanQuietly(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "ctrl+w")
	m.Update(evidenceLoadedMsg{gen: m.evidence.Generation(), err: errors.New("connection refused")})

	if out := plainView(m); !strings.Contains(out, "Evidence unavailable") {
		t.Errorf("evidence that could not be read must be stated:\n%s", out)
	}
}

func TestWhyIsReachableFromTheOpenApplicationAndBack(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())

	press(t, m, "enter")
	press(t, m, "ctrl+w")
	if m.view != viewWhy {
		t.Fatalf("Ctrl+W must work from the detail view too, got %v", m.view)
	}

	press(t, m, "enter")
	if m.view != viewApplication {
		t.Errorf("Enter must lead from the explanation to the objects, got %v", m.view)
	}

	press(t, m, "ctrl+w")
	press(t, m, "esc")
	if m.view != viewApplications {
		t.Errorf("Esc is home from anywhere, got %v", m.view)
	}
}

func TestTheDashboardOffersAWhyCommandPerBrokenApplication(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		brokenApplication(),
		testApplication("api", application.Healthy, 2, 2),
	)

	press(t, m, "ctrl+p")
	typeInto(t, m, "why payments")

	for _, it := range m.cmdPal.Items() {
		if strings.HasPrefix(it.Title, "Why is payments") {
			if !strings.Contains(it.Subtitle, "restart in a loop") {
				t.Errorf("the palette entry must summarise the incident, got %q", it.Subtitle)
			}
			return
		}
	}
	t.Error("every unhealthy application must be one search away from its explanation")
}

func TestAHealthyApplicationHasNoWhyCommand(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("api", application.Healthy, 2, 2))

	for _, c := range m.registry.Commands() {
		if strings.HasPrefix(c.Title, "Why is api") {
			t.Error("kubeui must not offer to explain an application that is fine")
		}
	}
}

func TestEventsAppearOnTheApplicationDetail(t *testing.T) {
	m := newTestModel(t)
	app := brokenApplication()
	loadApplicationsInto(m, app)
	press(t, m, "enter")
	loadEvidenceInto(m, application.Context{Events: []application.Event{{
		Type: "Warning", Reason: "BackOff", Count: 30,
		Message:  "Back-off restarting failed container",
		LastSeen: time.Now().Add(-2 * time.Minute),
		About:    application.ObjectRef{Kind: "Pod", Name: app.Pods[0].Name, UID: app.Pods[0].UID},
	}}})

	out := plainView(m)
	for _, want := range []string{"RECENT EVENTS", "BackOff", "Back-off restarting failed container"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail view must show what the cluster said, missing %q:\n%s", want, out)
		}
	}
}
