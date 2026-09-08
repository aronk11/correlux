package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func TestNavigationAndDockFitTerminal(t *testing.T) {
	for _, size := range [][2]int{{60, 12}, {80, 24}, {110, 32}, {180, 50}} {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.applyLayout()
		if w := lipgloss.Width(m.renderNavigation()); w != size[0] {
			t.Fatalf("navigation width %d at %v", w, size)
		}
		before := m.view
		press(t, m, "ctrl+p")
		rect := m.overlayRect()
		if rect.Y < m.screen.Body.Y || rect.Y+rect.Height != m.screen.Status.Y {
			t.Fatalf("dock covers header or footer: %+v", rect)
		}
		frame := strings.Split(plainView(m), "\n")
		if !strings.Contains(frame[0], "staging") || !strings.Contains(frame[rect.Y+1], "Commands") {
			t.Fatalf("overlay must render at its hit target and preserve cluster header:\n%s", plainView(m))
		}
		if m.view != before {
			t.Fatal("commands must retain workspace")
		}
		press(t, m, "esc")
		if m.view != before || m.overlay != overlayNone {
			t.Fatal("escape must restore workspace")
		}
	}
}

func TestNavigationMouseAndModalIsolation(t *testing.T) {
	m := newTestModel(t)
	var commands navigationItem
	for _, item := range m.navigationItems() {
		if item.action == ActionPalette {
			commands = item
		}
	}
	m.handleClick(tea.MouseClickMsg{X: commands.start, Y: 2, Button: tea.MouseLeft})
	if m.overlay != overlayPalette {
		t.Fatal("commands navigation must open the dock")
	}
	view(m) // resolve the selector viewport
	rect := m.overlayRect()
	selected, _ := m.cmdPal.Selected()
	m.handleClick(tea.MouseClickMsg{X: rect.X + 3, Y: rect.Y + rect.Height - 2, Button: tea.MouseLeft})
	if m.overlay != overlayPalette {
		t.Fatal("footer click executed an invisible command")
	}
	got, _ := m.cmdPal.Selected()
	if got.ID != selected.ID {
		t.Fatal("footer moved selection")
	}
	m.handleClick(tea.MouseClickMsg{X: 0, Y: 2, Button: tea.MouseLeft})
	if m.overlay != overlayPalette {
		t.Fatal("navigation behind modal must not activate")
	}
}

func TestFilteredWhyAndPaletteAgreeOnTarget(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Down, 0, 3), testApplication("worker", application.Degraded, 1, 2))
	typeFilter(t, m, "worker")
	press(t, m, "enter")
	press(t, m, "ctrl+p")
	if !strings.Contains(m.cmdPal.Title, "worker") || !strings.Contains(m.whySubtitle(), "worker") {
		t.Fatal("palette must name visible application")
	}
	selected, _ := m.cmdPal.Selected()
	if selected.ID != "cmd.why" {
		t.Fatalf("triage should lead with diagnosis, got %s", selected.ID)
	}
	press(t, m, "enter")
	a, ok := m.currentApplication()
	if !ok || a.Name != "worker" || m.view != viewWhy {
		t.Fatal("WHY must diagnose filtered selection")
	}
}

func TestFilteredLogsUseVisiblePod(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1", "worker-1")})
	typeFilter(t, m, "worker")
	press(t, m, "enter")
	sources, _, ok := m.logSources()
	if !ok || len(sources) != 1 || sources[0].Pod != "worker-1" {
		t.Fatalf("wrong filtered log target: %+v", sources)
	}
}

func TestNavigationReturnsFromApplication(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Down, 0, 3))
	m.openApplication("payments")
	m.clickNavigation(0)
	if m.view != viewApplications {
		t.Fatal("Apps must return from detail to dashboard")
	}
}

func TestLogsReturnToInvestigation(t *testing.T) {
	for _, origin := range []viewKind{viewApplication, viewWhy, viewTable} {
		m := newTestModel(t)
		loadCatalogInto(m, testCatalog())
		loadApplicationsInto(m, brokenApplication())
		m.openApplication("payments")
		if origin == viewTable {
			m.openResource("pods")
			m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1")})
		}
		m.view = origin
		press(t, m, "l")
		if m.view != viewLogs {
			t.Fatalf("logs did not open from %v", origin)
		}
		press(t, m, "esc")
		if m.view != origin || m.cancelLogs != nil {
			t.Fatalf("logs must return to %v and close stream", origin)
		}
	}
}

// TestHeaderBlockIsClosedAndCounted pins the shape of the chrome: three lines
// that say where you are, a rule that says where the cluster's own data
// starts, and the count of what the breadcrumb points at at the end of that
// line rather than inside the crumb.
func TestHeaderBlockIsClosedAndCounted(t *testing.T) {
	// Both glyph sets: a terminal that cannot draw an arrow or a box rule gets
	// the same frame in ASCII, and that is what the Windows runners see.
	for _, env := range []map[string]string{
		{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"},
		{"TERM": "xterm-256color"},
	} {
		m := newTestModel(t, func(o *Options) { o.Env = theme.MapEnv(env) })
		m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
		m.applyLayout()
		loadApplicationsInto(m, dumpApplications()...)

		arrow := " " + m.theme.Glyphs.Arrow + " "
		lines := strings.Split(plainView(m), "\n")
		crumb := lines[1]
		if !strings.HasPrefix(crumb, "Cluster"+arrow+"default"+arrow+"Applications") {
			t.Fatalf("the breadcrumb must name the place:\n%s", crumb)
		}
		if !strings.HasSuffix(crumb, "4 total, 1 down, 1 degraded, 2 healthy") {
			t.Fatalf("the count belongs at the end of the breadcrumb line:\n%s", crumb)
		}
		if !strings.Contains(lines[m.screen.Nav.Y], "[Apps") {
			t.Fatalf("the navigation must sit on its own hit target:\n%s", lines[m.screen.Nav.Y])
		}

		rule := lines[m.screen.Body.Y-1]
		if strings.Trim(rule, m.theme.Glyphs.Rule) != "" || lipgloss.Width(rule) != m.screen.Width {
			t.Fatalf("a rule must close the header block across the width: %q", rule)
		}
		if !strings.HasPrefix(lines[m.screen.Body.Y], "STATUS") {
			t.Fatalf("the body must start immediately below the rule:\n%s", lines[m.screen.Body.Y])
		}
	}
}

// TestNavigationTargetsMatchWhatIsDrawn keeps the mouse honest: an item is
// the same width whether it is active or not, so the cell a label is drawn in
// is the cell that opens it.
func TestNavigationTargetsMatchWhatIsDrawn(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m.applyLayout()
	for _, view := range []viewKind{viewApplications, viewUsage, viewActivity} {
		m.view = view
		drawn := []rune(ansi.ReplaceAllString(m.renderNavigation(), ""))
		for _, item := range m.navigationItems() {
			if item.end > len(drawn) {
				t.Fatalf("%v: target %q runs past the bar", view, item.label)
			}
			if target := string(drawn[item.start:item.end]); !strings.Contains(target, item.label) {
				t.Fatalf("%v: clicking %q lands on %q", view, item.label, target)
			}
		}
	}
}

// lastLine is the status bar of a rendered frame.
func lastLine(frame string) string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// TestEscapeIsAlwaysOneStepBack pins the rule that makes the key predictable:
// Esc returns to the screen behind this one, never all the way home.
func TestEscapeIsAlwaysOneStepBack(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	loadApplicationsInto(m, brokenApplication())

	press(t, m, "enter")
	press(t, m, "ctrl+w")
	press(t, m, "esc")
	if m.view != viewApplication {
		t.Fatalf("Esc from the explanation must land on the application it explains, got %v", m.view)
	}
	press(t, m, "esc")
	if m.view != viewApplications {
		t.Fatalf("Esc from an application must land on the dashboard, got %v", m.view)
	}

	m.openResource("pods")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: manyPods("payments-1")})
	press(t, m, "enter")
	if m.view != viewObject {
		t.Fatalf("Enter must open the row, got %v", m.view)
	}
	press(t, m, "esc")
	if m.view != viewTable {
		t.Fatalf("Esc from an object must land on the table that listed it, got %v", m.view)
	}
	press(t, m, "esc")
	if m.view != viewApplications {
		t.Fatalf("Esc from the table must land on the dashboard, got %v", m.view)
	}
}

// TestTheBackKeyNamesWhereItGoes keeps the bottom row answering the question
// people actually have — not whether Esc leaves, but what it leaves to.
func TestTheBackKeyNamesWhereItGoes(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.applyLayout()
	loadApplicationsInto(m, brokenApplication())

	if footer := lastLine(plainView(m)); strings.Contains(footer, "Esc") {
		t.Errorf("the dashboard is where back stops; it must not offer it:\n%s", footer)
	}
	press(t, m, "enter")
	if footer := lastLine(plainView(m)); !strings.Contains(footer, "Esc Applications") {
		t.Errorf("an open application goes back to the dashboard:\n%s", footer)
	}
	press(t, m, "ctrl+w")
	if footer := lastLine(plainView(m)); !strings.Contains(footer, "Esc payments") {
		t.Errorf("an explanation goes back to its application, by name:\n%s", footer)
	}
}

// TestTheStatusBarDoesNotRepeatTheNavigationBar: a shortcut is printed once —
// in the navigation bar when it fits there, and in the status bar when the
// terminal is too narrow for the bar to carry it.
func TestTheStatusBarDoesNotRepeatTheNavigationBar(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	for _, size := range [][2]int{{140, 32}, {110, 32}, {80, 24}, {60, 12}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.applyLayout()

		repeated := 0
		for _, h := range m.statusData().Hints {
			switch h.Desc {
			case "Resources", "Usage", "Commands":
				repeated++
			}
		}
		if bar := m.navigationShowsKeys(); bar == (repeated > 0) {
			t.Errorf("%v: navigation shows shortcuts=%v, status bar repeats %d of them", size, bar, repeated)
		}
	}
}

// TestAnEmptyDashboardOffersOnlyWhatItCanDo covers the screen an operator sees
// when the cluster is unreachable: every key on offer must do something.
func TestAnEmptyDashboardOffersOnlyWhatItCanDo(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.applyLayout()
	loadApplicationsInto(m)

	footer := lastLine(plainView(m))
	for _, offer := range []string{"Open", "Why", "Grouping"} {
		if strings.Contains(footer, offer) {
			t.Errorf("%q acts on a row, and there is no row:\n%s", offer, footer)
		}
	}
	if !strings.Contains(footer, "Cluster") {
		t.Errorf("the way out of an empty scope must stay on offer:\n%s", footer)
	}
}
