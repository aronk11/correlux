package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
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
		"restart in a loop",               // the problem
		"memory limit",                    // the cause, read from the previous run
		"CAUSE",                           // the reading of the evidence, labelled as one
		"UNKNOWN",                         // what that reading still cannot explain
		"does not report why",             // said out loud rather than guessed
		"EVIDENCE",                        // what the cluster actually said
		"reason OOMKilled, exit code 137", // quoted, not paraphrased
		"RELATED",                         // what to walk to next
		"Deployment/payments",
		"WHAT TO CHECK",    // and what to do next
		"kubectl logs",     // with the command that shows it
		"confidence: high", // and how sure Correlux is
		"NEXT",             // the real keys that do something here
		"previous run's logs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the WHY view must contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "memory limit") {
		evidenceLine := out[strings.Index(out, "EVIDENCE"):strings.Index(out, "RELATED")]
		if strings.Contains(evidenceLine, "memory limit") {
			t.Errorf("evidence must quote the cluster, not the reading of it:\n%s", evidenceLine)
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

func TestWhyHealthIncludesAServiceFailureAfterEvidenceLoads(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("api", application.Healthy, 1, 1)
	app.Services = []application.Service{{
		Meta:     application.Meta{Kind: "Service", Name: "api", Namespace: "default"},
		Selector: map[string]string{"app": "does-not-match"},
	}}
	loadApplicationsInto(m, app)
	loadEvidenceInto(m, application.Context{Endpoints: []application.EndpointSet{{
		Service: "api", Namespace: "default",
	}}})

	press(t, m, "ctrl+w")
	out := plainView(m)
	if !strings.Contains(out, "down") || !strings.Contains(out, "matches no pods") {
		t.Errorf("delivery failures must change the displayed application health:\n%s", out)
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
			t.Error("Correlux must not offer to explain an application that is fine")
		}
	}
}

func TestNextOffersPreviousLogsOnlyWhenAPreviousRunExists(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "ctrl+w")

	out := plainView(m)
	if !strings.Contains(out, "[l] read the previous run's logs") {
		t.Errorf("a crash loop has a previous run; the hint must offer it honestly:\n%s", out)
	}
}

func TestNextNeverClaimsAPreviousRunThatDoesNotExist(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("payments", application.Degraded, 1, 2)
	for i := range app.Pods {
		app.Pods[i].Reason = ""
		app.Pods[i].Phase = "Running"
		app.Pods[i].Containers = []application.Container{{Name: "payments", State: "running"}}
	}
	app.Pods[1].Ready = false
	loadApplicationsInto(m, app)
	press(t, m, "ctrl+w")

	out := plainView(m)
	if !strings.Contains(out, "[l]") {
		t.Fatalf("the pods have logs to read; the hint must be offered:\n%s", out)
	}
	if strings.Contains(out, "previous run's logs") {
		t.Errorf("none of these pods have a previous run; offering it would be a lie:\n%s", out)
	}
}

func TestNextDoesNotOfferLogsWhenAContainerNeverStarted(t *testing.T) {
	m := newTestModel(t)
	app := testApplication("payments", application.Down, 0, 1)
	app.Pods[0].Containers = []application.Container{{
		Name: "payments", State: "waiting", Reason: "CreateContainerConfigError",
		Message: `secret "database" not found`,
	}}
	loadApplicationsInto(m, app)
	press(t, m, "ctrl+w")

	if out := plainView(m); strings.Contains(out, "read the pods' logs") {
		t.Errorf("a container that never started has no logs to offer:\n%s", out)
	}
}

func TestNextOffersNothingWhenThereIsNothingToExplain(t *testing.T) {
	m := newTestModel(t)
	healthy := testApplication("api", application.Healthy, 2, 2)
	for i := range healthy.Pods {
		healthy.Pods[i].Containers = []application.Container{{Name: "api", State: "running", Ready: true}}
	}
	loadApplicationsInto(m, healthy)
	press(t, m, "ctrl+w")

	if out := plainView(m); strings.Contains(out, "NEXT") {
		t.Errorf("a healthy application has nothing to act on, so NEXT must not appear:\n%s", out)
	}
}

func TestPressingLFromWhyActuallyOpensThePreviousRun(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "ctrl+w")
	press(t, m, "l")

	if m.view != viewLogs {
		t.Fatalf("l must open the logs the hint promised, got view %v", m.view)
	}
	if !m.logPrevious {
		t.Error("the hint said \"previous run\"; the log stream it opens must ask for it")
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
