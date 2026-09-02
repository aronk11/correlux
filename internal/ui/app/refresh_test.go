package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/config"
	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

// tick delivers one auto-refresh tick and reports the commands it produced.
func tick(m *Model) tea.Cmd {
	_, cmd := m.Update(autoRefreshTickMsg{seq: m.refreshSeq})
	return cmd
}

func TestAutoRefreshIsOffUntilItIsAskedFor(t *testing.T) {
	m := newTestModel(t)
	if m.autoRefresh {
		t.Fatal("kubeui must not poll anybody's API server without being told to")
	}
	if strings.Contains(view(m), "auto ") {
		t.Error("nothing may claim to be refreshing while it is off")
	}

	press(t, m, "ctrl+f")
	if !m.autoRefresh {
		t.Fatal("the toggle must turn it on")
	}
	if out := view(m); !strings.Contains(out, "auto 10s") {
		t.Errorf("a screen that changes on its own must say so:\n%s", out)
	}

	press(t, m, "ctrl+f")
	if m.autoRefresh {
		t.Error("the toggle must turn it off again")
	}
}

func TestTheConfiguredIntervalIsHonoured(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config.Refresh = config.Refresh{Auto: true, Every: "30s"}
	})
	if !m.autoRefresh {
		t.Fatal("refresh.auto must start the timed reload")
	}
	if m.refreshEvery != 30*time.Second {
		t.Errorf("interval = %v, want 30s", m.refreshEvery)
	}
}

func TestAnAbsurdIntervalIsRaisedToTheFloor(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Config.Refresh = config.Refresh{Auto: true, Every: "50ms"}
	})
	if m.refreshEvery != config.MinRefreshInterval {
		t.Errorf("interval = %v, want the %v floor", m.refreshEvery, config.MinRefreshInterval)
	}
}

func TestATickReloadsOnlyWhatIsOnScreen(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "ctrl+f")
	// Turning it on reloads once; let that request answer before ticking.
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))

	catalogGen, namespacesGen := m.catalog.Generation(), m.namespaces.Generation()
	appsGen := m.apps.Generation()

	tick(m)
	if m.apps.Generation() == appsGen {
		t.Error("a tick on the dashboard must reload the applications")
	}
	if m.catalog.Generation() != catalogGen || m.namespaces.Generation() != namespacesGen {
		t.Error("discovery and the namespace list must not be refetched on every tick")
	}
}

func TestATickDoesNotStackRequests(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "ctrl+f")
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))

	tick(m)
	inFlight := m.apps.Generation()
	tick(m)
	if m.apps.Generation() != inFlight {
		t.Error("a tick must not start a second request while the first is unanswered")
	}
}

func TestATickIsSkippedWhileAnOverlayIsOpen(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "ctrl+f")
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "ctrl+p")

	gen := m.apps.Generation()
	tick(m)
	if m.apps.Generation() != gen {
		t.Error("the list under an open palette must not be reloaded from under it")
	}
}

func TestADeepPagedTableIsLeftAlone(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")

	names := make([]string, 0, int(resources.DefaultPageSize)+10)
	for i := 0; i < int(resources.DefaultPageSize)+10; i++ {
		names = append(names, "payments-"+itoa(i))
	}
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage(names...)})
	press(t, m, "ctrl+f")

	gen := m.table.Generation()
	tick(m)
	if m.table.Generation() != gen {
		t.Error("refreshing a table someone has paged deep into would delete the rows under them")
	}
}

func TestFailuresBackOff(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "ctrl+f")
	if got := m.autoRefreshDelay(); got != m.refreshEvery {
		t.Fatalf("the first delay is the interval, got %v", got)
	}

	for i := 0; i < 2; i++ {
		m.Update(applicationsLoadedMsg{gen: m.apps.Generation(), err: errors.New("connection refused")})
		m.loadApplications()
	}
	if got := m.autoRefreshDelay(); got <= m.refreshEvery {
		t.Errorf("an unreachable cluster must be polled less often, delay is %v", got)
	}

	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	if got := m.autoRefreshDelay(); got != m.refreshEvery {
		t.Errorf("a successful load must return to the configured interval, got %v", got)
	}
}

func TestAStaleTickerIsIgnored(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	press(t, m, "ctrl+f")
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))
	stale := m.refreshSeq

	press(t, m, "ctrl+f") // off
	press(t, m, "ctrl+f") // on again
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 1, 1))

	gen := m.apps.Generation()
	m.Update(autoRefreshTickMsg{seq: stale})
	if m.apps.Generation() != gen {
		t.Error("the ticker of a previous toggle must not keep refreshing")
	}
}

func TestRefreshingATableKeepsTheCursorOnItsRow(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("api-1", "api-2", "api-3")})

	press(t, m, "down")
	press(t, m, "down")
	if m.tableCursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.tableCursor)
	}

	// The first pod is gone and a new one appeared: the cursor must stay on the
	// pod the user selected, not on the third row.
	m.loadTable()
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("api-2", "api-3", "api-4")})
	if got := m.cursorRowKey(); got != "default/api-3" {
		t.Errorf("the cursor is on %q, want the row it was on", got)
	}
}
