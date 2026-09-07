package app

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// filterTo types a filter on the screen the model is on.
func filterTo(t *testing.T, m *Model, text string) {
	t.Helper()
	press(t, m, "/")
	typeInto(t, m, text)
}

func agedApp(name, ns string, health application.Health, restarts int32, age time.Duration) application.Application {
	return application.Application{
		Name: name, Namespace: ns, Health: health,
		ReadyPods: 0, DesiredPods: 3, Restarts: restarts,
		CreatedAt: time.Now().Add(-age),
		Summary:   "the summary",
	}
}

// TestTheDashboardAnswersAComparison is the feature as an operator uses it:
// "which of these keeps restarting" is not a question fuzzy text can answer.
func TestTheDashboardAnswersAComparison(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		agedApp("payments", "shop", application.Down, 17, 50*time.Hour),
		agedApp("checkout", "shop", application.Degraded, 3, 45*time.Minute),
		agedApp("billing", "finance", application.Healthy, 0, 13*24*time.Hour),
	)

	filterTo(t, m, "restarts>5")
	got := visibleNames(m)
	if len(got) != 1 || got[0] != "payments" {
		t.Fatalf("restarts>5 kept %v, want only the one above five", got)
	}

	m.clearSearch()
	filterTo(t, m, "age<1h")
	if got := visibleNames(m); len(got) != 1 || got[0] != "checkout" {
		t.Fatalf("age<1h kept %v, want only what is new", got)
	}

	m.clearSearch()
	filterTo(t, m, "ns=shop restarts>0")
	if got := visibleNames(m); len(got) != 2 {
		t.Fatalf("two terms kept %v, want both shop applications", got)
	}
}

// TestFuzzyTextStillWorksBesideAComparison: the old behaviour is the default,
// and the two compose.
func TestFuzzyTextStillWorksBesideAComparison(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		agedApp("payments", "shop", application.Down, 17, time.Hour),
		agedApp("payments-worker", "shop", application.Down, 2, time.Hour),
	)

	filterTo(t, m, "pay")
	if got := visibleNames(m); len(got) != 2 {
		t.Fatalf("plain text kept %v, want both", got)
	}

	m.clearSearch()
	filterTo(t, m, "pay restarts>5")
	if got := visibleNames(m); len(got) != 1 || got[0] != "payments" {
		t.Fatalf("text and comparison kept %v, want the one that is both", got)
	}
}

// TestAColumnThisScreenDoesNotHaveIsNamed: an empty list must never be the
// answer to a question Correlux did not understand.
func TestAColumnThisScreenDoesNotHaveIsNamed(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, agedApp("payments", "shop", application.Down, 1, time.Hour))

	filterTo(t, m, "cpu>500m")
	if got := visibleNames(m); len(got) != 0 {
		t.Fatalf("kept %v, want nothing — the column does not exist", got)
	}
	out := plainView(m)
	if !strings.Contains(out, "no cpu column here") {
		t.Errorf("the screen must say why it is empty:\n%s", out)
	}
}

// visibleNames is what the dashboard would draw.
func visibleNames(m *Model) []string {
	apps := m.visibleApplications()
	out := make([]string, 0, len(apps))
	for i := range apps {
		out = append(out, apps[i].Name)
	}
	return out
}
