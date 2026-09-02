package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
)

func testApplication(name string, health application.Health, ready, desired int32) application.Application {
	a := application.Application{
		Name:        name,
		Namespace:   "default",
		Health:      health,
		Summary:     itoa(int(ready)) + " of " + itoa(int(desired)) + " pods ready",
		ReadyPods:   ready,
		DesiredPods: desired,
		CreatedAt:   time.Now().Add(-90 * time.Minute),
		Workloads: []application.Workload{{
			Meta:       application.Meta{Kind: "Deployment", Name: name, Namespace: "default"},
			Desired:    desired,
			Ready:      ready,
			Replicated: true,
		}},
	}
	for i := int32(0); i < desired; i++ {
		pod := application.Pod{
			Meta:      application.Meta{Kind: "Pod", Name: name + "-7d8f-" + itoa(int(i)), Namespace: "default"},
			Phase:     "Running",
			Ready:     i < ready,
			Node:      "node-1",
			Scheduled: true,
		}
		if i >= ready {
			pod.Phase, pod.Reason = "Pending", "CrashLoopBackOff"
		}
		a.Pods = append(a.Pods, pod)
	}
	a.Services = []application.Service{{
		Meta: application.Meta{Kind: "Service", Name: name, Namespace: "default"},
		Type: "ClusterIP", Ports: []string{"80/TCP"},
	}}
	return a
}

// loadApplicationsInto feeds a dashboard to the model the way the runtime would.
func loadApplicationsInto(m *Model, apps ...application.Application) {
	m.Update(applicationsLoadedMsg{
		gen:  m.apps.Generation(),
		list: applicationList{Apps: apps, Snapshot: application.Snapshot{FetchedAt: time.Now()}},
	})
}

func TestTheFirstScreenIsTheApplicationDashboard(t *testing.T) {
	m := newTestModel(t)
	if m.view != viewApplications {
		t.Fatalf("kubeui must open on the applications, got view %v", m.view)
	}

	loadApplicationsInto(m,
		testApplication("payments", application.Down, 0, 3),
		testApplication("api", application.Healthy, 3, 3),
	)

	out := view(m)
	for _, want := range []string{"payments", "api", "down", "healthy", "0/3", "3/3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dashboard must show %q:\n%s", want, out)
		}
	}
}

func TestLoadingIsNotConfusedWithAnEmptyCluster(t *testing.T) {
	m := newTestModel(t)

	if out := view(m); !strings.Contains(out, "Looking for applications") {
		t.Errorf("an unfinished first load must say so:\n%s", out)
	}

	loadApplicationsInto(m)
	out := view(m)
	if !strings.Contains(out, "No applications in default.") {
		t.Errorf("an empty scope must say it is empty:\n%s", out)
	}
	if strings.Contains(out, "Looking for applications") {
		t.Error("an answered request must not still claim to be loading")
	}
}

func TestAFailedLoadSaysWhy(t *testing.T) {
	m := newTestModel(t)
	m.Update(applicationsLoadedMsg{gen: m.apps.Generation(), err: errors.New("connection refused")})

	out := view(m)
	if !strings.Contains(out, "connection refused") {
		t.Errorf("a failed load must name the failure:\n%s", out)
	}
	if strings.Contains(out, "No applications") {
		t.Error("a failure must never read as an empty cluster")
	}
}

func TestWhatCouldNotBeReadIsStated(t *testing.T) {
	m := newTestModel(t)
	m.Update(applicationsLoadedMsg{
		gen: m.apps.Generation(),
		list: applicationList{Snapshot: application.Snapshot{
			Gaps: []application.Gap{{Kind: "Ingress", Reason: "not permitted for this user"}},
		}},
	})

	if out := view(m); !strings.Contains(out, "not permitted for this user") {
		t.Errorf("a kind kubeui may not read must be stated, not hidden:\n%s", out)
	}
}

func TestOpeningAnApplicationShowsWhatItIsMadeOf(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Degraded, 2, 3))

	press(t, m, "enter")
	if m.view != viewApplication {
		t.Fatalf("Enter must open the application, got view %v", m.view)
	}

	out := view(m)
	for _, want := range []string{"payments", "WORKLOADS", "PODS", "NETWORK", "Deployment", "CrashLoopBackOff"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail view must show %q:\n%s", want, out)
		}
	}

	press(t, m, "esc")
	if m.view != viewApplications {
		t.Error("esc must return to the dashboard")
	}
}

func TestTheCursorFollowsTheApplicationAcrossARefresh(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		testApplication("api", application.Healthy, 3, 3),
		testApplication("payments", application.Healthy, 3, 3),
	)
	press(t, m, "down")
	if got := m.cursorApplicationKey(); got != "default/payments" {
		t.Fatalf("cursor is on %q, want default/payments", got)
	}

	// payments starts failing, so it sorts to the top: the cursor must follow
	// the application, not the row number.
	loadApplicationsInto(m,
		testApplication("payments", application.Down, 0, 3),
		testApplication("api", application.Healthy, 3, 3),
	)
	if got := m.cursorApplicationKey(); got != "default/payments" {
		t.Errorf("after a refresh the cursor is on %q, want default/payments", got)
	}
}

func TestAnOpenApplicationThatDisappearsSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "enter")

	loadApplicationsInto(m, testApplication("api", application.Healthy, 1, 1))
	if out := view(m); !strings.Contains(out, "no longer in default") {
		t.Errorf("a deleted application must be reported, not silently blank:\n%s", out)
	}
}

func TestEveryApplicationIsAPaletteCommand(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Down, 0, 3))

	press(t, m, "ctrl+p")
	typeInto(t, m, "payments")

	for _, it := range m.cmdPal.Items() {
		if it.Title == "Open payments" {
			return
		}
	}
	t.Error("an application must be reachable straight from the command palette")
}

func TestScopeChangeDropsTheApplicationsOfTheOldNamespace(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))

	m.switchNamespace("other")
	if len(m.applications()) != 0 {
		t.Error("applications from the previous scope must never linger under a new heading")
	}
	if out := view(m); strings.Contains(out, "payments") {
		t.Errorf("stale applications are still on screen:\n%s", out)
	}
}

func TestAgeIsRenderedTheWayKubectlDoes(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m"},
		{50 * time.Hour, "2d2h"},
	}
	for _, tc := range cases {
		if got := formatAge(now.Add(-tc.age), now); got != tc.want {
			t.Errorf("age %v rendered as %q, want %q", tc.age, got, tc.want)
		}
	}
	if got := formatAge(time.Time{}, now); got != "—" {
		t.Errorf("an unknown age must render as a dash, got %q", got)
	}
}

// wheel builds a mouse wheel event, the way a terminal reports one.
func wheel(up bool) tea.MouseWheelMsg {
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg{Button: button}
}

func TestTheWheelScrollsTheDashboard(t *testing.T) {
	// The model is 120x40 from newTestModel; a resize would be debounced and
	// not yet applied, so the fixture is sized to overflow that instead.
	m := newTestModel(t)

	apps := make([]application.Application, 0, 80)
	for i := 0; i < 80; i++ {
		apps = append(apps, testApplication("app-"+itoa(i), application.Healthy, 1, 1))
	}
	loadApplicationsInto(m, apps...)

	m.Update(wheel(false))
	if m.appPort.Offset == 0 {
		t.Fatal("the wheel must scroll the dashboard")
	}
	// The cursor is dragged along only as far as it must be to stay visible.
	if m.appPort.Cursor < m.appPort.Offset {
		t.Errorf("cursor %d scrolled off the top of the viewport at offset %d", m.appPort.Cursor, m.appPort.Offset)
	}

	m.Update(wheel(true))
	if m.appPort.Offset != 0 {
		t.Errorf("scrolling back up must return to the top, offset is %d", m.appPort.Offset)
	}
}

func TestTheWheelScrollsTheOpenApplication(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Degraded, 1, 60))
	press(t, m, "enter")

	m.Update(wheel(false))
	if m.detailPort.Offset == 0 {
		t.Fatal("a detail view longer than the screen must scroll")
	}
	m.Update(wheel(true))
	if m.detailPort.Offset != 0 {
		t.Errorf("scrolling back must reach the top, offset is %d", m.detailPort.Offset)
	}
}

func TestTheWheelDoesNotScrollPastTheEnd(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))

	for i := 0; i < 20; i++ {
		m.Update(wheel(false))
	}
	if m.appPort.Offset != 0 {
		t.Errorf("a list that fits on screen must not scroll at all, offset is %d", m.appPort.Offset)
	}
}

func TestTheWheelDoesNotScrollBehindAnOverlay(t *testing.T) {
	m := newTestModel(t)
	apps := make([]application.Application, 0, 80)
	for i := 0; i < 80; i++ {
		apps = append(apps, testApplication("app-"+itoa(i), application.Healthy, 1, 1))
	}
	loadApplicationsInto(m, apps...)
	press(t, m, "?") // the help overlay has nothing to scroll

	m.Update(wheel(false))
	if m.appPort.Offset != 0 {
		t.Errorf("the dashboard scrolled behind an open overlay, offset is %d", m.appPort.Offset)
	}
}

func TestWhoDeployedAnApplicationIsOnScreenAndReachable(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	app := testApplication("payments", application.Degraded, 2, 3)
	app.Manager = application.Manager{
		Tool: "Flux", Kind: "HelmRelease", Name: "payments", Namespace: "flux-system",
	}
	loadApplicationsInto(m, app)

	// The dashboard carries it in the wide columns.
	press(t, m, "w")
	if out := plainView(m); !strings.Contains(out, "Flux payments") {
		t.Errorf("the dashboard must say who deployed it:\n%s", out)
	}
	press(t, m, "w")

	press(t, m, "enter")
	out := plainView(m)
	for _, want := range []string{"DELIVERED BY", "Flux", "HelmRelease/payments", "flux-system"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail view must show %q:\n%s", want, out)
		}
	}
}

func TestTheFluxObjectCanBeOpenedFromTheApplication(t *testing.T) {
	m := newTestModel(t)
	catalog := testCatalog()
	catalog.Resources = append(catalog.Resources, resource(
		"helm.toolkit.fluxcd.io", "v2", "helmreleases", "HelmRelease", true, false, "hr"))
	loadCatalogInto(m, catalog)

	app := testApplication("payments", application.Degraded, 2, 3)
	app.Manager = application.Manager{
		Tool: "Flux", Kind: "HelmRelease", Name: "payments", Namespace: "flux-system",
	}
	loadApplicationsInto(m, app)
	press(t, m, "enter")

	// Past the workload, the pods, the service, onto the delivery row.
	_, targets := m.applicationView()
	found := -1
	for i, ref := range targets {
		if ref.Kind == "HelmRelease" {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("the HelmRelease must be selectable: %+v", targets)
	}

	m.detailPort.Cursor = found
	press(t, m, "enter")
	if m.view != viewObject || m.objectTarget.Kind != "HelmRelease" {
		t.Errorf("view = %v target = %+v, want the Flux object open", m.view, m.objectTarget)
	}
}

func TestAnApplicationNobodyClaimsSaysSo(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "enter")

	if out := plainView(m); !strings.Contains(out, "nothing claims this application") {
		t.Errorf("on a cluster run by Flux, an unclaimed application is worth noticing:\n%s", out)
	}
}
