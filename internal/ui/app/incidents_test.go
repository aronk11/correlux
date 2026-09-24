package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
)

func TestTheDashboardSaysWhatNeedsAttentionInTheRoomItHas(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, dumpApplications()...)

	out := plainView(m)
	strip := strings.Index(out, "NEEDS ATTENTION")
	if strip < 0 {
		t.Fatalf("a short table leaves room for what needs attention:\n%s", out)
	}
	if !strings.Contains(out[strip:], "payments  3 pods restart in a loop — the container exceeded its memory limit") {
		t.Errorf("each entry says what is wrong and why:\n%s", out)
	}
	if strings.Contains(out[strip:], "frontend") {
		t.Errorf("a healthy application needs no attention:\n%s", out)
	}
}

func TestTheStripNeverTakesALineFromTheTable(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m.applyLayout()
	apps := []application.Application{brokenApplication()}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		apps = append(apps, testApplication(name, application.Healthy, 1, 1))
	}
	loadApplicationsInto(m, apps...)
	if strings.Contains(plainView(m), "NEEDS ATTENTION") {
		t.Errorf("a table that does not fit keeps every line:\n%s", plainView(m))
	}
}

func TestPaletteKeysAndCategoriesReadDifferently(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, dumpApplications()...)
	for _, item := range m.filterCommands("") {
		if item.RightIsKey && strings.ToLower(item.Right) == item.Right && !strings.ContainsAny(item.Right, "+/?") && len(item.Right) > 1 {
			t.Errorf("%q is marked as a key but reads like a category", item.Right)
		}
		if !item.RightIsKey && item.Right != "" && item.Right != strings.ToLower(item.Right) {
			t.Errorf("category %q must read as a word, not a key", item.Right)
		}
	}
}
