package app

import (
	"reflect"
	"testing"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/domain/application"
	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/ui/layout"
)

// restoreFrom restores saved the way the palette does, with the focus on the
// pane at index focus, and lets the cluster answer.
func restoreFrom(t *testing.T, saved config.SavedInvestigation, focus int, open ...followKind) *Model {
	t.Helper()
	m := newTestModel(t)
	for _, kind := range open {
		m.openPane(kind, layout.SplitAuto)
	}
	m.focusPane(focus)
	saved.Context, saved.Namespace = m.contextName, m.namespace
	m.restoreInvestigation(&saved)
	loadApplicationsInto(m, testApplication("web", application.Healthy, 1, 1))
	m.catalog.Succeed(m.catalog.Start(), &kubediscovery.Catalog{})
	m.tryRestoreInvestigation()
	return m
}

func companionIDs(m *Model) []string {
	var ids []string
	for _, p := range m.panes {
		if p.follow != followNone {
			ids = append(ids, p.follow.id())
		}
	}
	return ids
}

func TestSavingFromASplitScreenRecordsCompanionsAndDriverNavigation(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("web", application.Healthy, 1, 1))
	m.selectedApp = "web"
	m.view = viewApplication
	m.openPane(followLogs, layout.SplitBelow)
	m.openPane(followEvents, layout.SplitAuto)
	// A companion with the focus must not decide what is saved.
	m.focusPane(1)
	if m.focused().follow == followNone {
		t.Fatal("the focus should be on a companion")
	}

	saved := m.currentInvestigation()
	if saved.View != "application" || saved.Application != "web" {
		t.Errorf("navigation = %q %q, want the driver's application", saved.View, saved.Application)
	}
	if want := []string{"logs", "events"}; !reflect.DeepEqual(saved.Panes, want) {
		t.Errorf("panes = %v, want %v", saved.Panes, want)
	}
	if saved.Split != "below" {
		t.Errorf("split = %q, want below", saved.Split)
	}
}

func TestSavingASinglePaneLeavesLayoutEmpty(t *testing.T) {
	saved := newTestModel(t).currentInvestigation()
	if saved.Panes != nil || saved.Split != "" {
		t.Errorf("layout = %v %q, want none", saved.Panes, saved.Split)
	}
}

func TestRestoringReopensSavedCompanions(t *testing.T) {
	// Whatever was open before is replaced, and the focus may be on a companion.
	m := restoreFrom(t, config.SavedInvestigation{View: "applications", Panes: []string{"why", "logs"}, Split: "right"}, 1, followYAML)

	if got, want := companionIDs(m), []string{"why", "logs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("companions = %v, want %v", got, want)
	}
	if len(m.panes) != 3 || m.split != layout.SplitRight {
		t.Errorf("panes = %d split = %v, want 3 and right", len(m.panes), m.split)
	}
	if m.focused().follow != followNone {
		t.Error("the focus should stay on the main pane")
	}
}

func TestRestoringWithoutLayoutLeavesOnePane(t *testing.T) {
	m := restoreFrom(t, config.SavedInvestigation{View: "applications"}, 0, followLogs)

	if len(m.panes) != 1 {
		t.Errorf("panes = %d, want 1", len(m.panes))
	}
}

func TestRestoringIgnoresUnknownNamesAndNeverExceedsMaxPanes(t *testing.T) {
	names := []string{"nope", "logs", "why", "details", "yaml", "events"}
	m := restoreFrom(t, config.SavedInvestigation{View: "applications", Panes: names, Split: "sideways"}, 0)

	if len(m.panes) != maxPanes {
		t.Errorf("panes = %d, want %d", len(m.panes), maxPanes)
	}
	if got, want := companionIDs(m), []string{"logs", "why", "details"}; !reflect.DeepEqual(got, want) {
		t.Errorf("companions = %v, want %v", got, want)
	}
}
