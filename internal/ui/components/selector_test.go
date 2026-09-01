package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/config"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

func items(names ...string) []Item {
	out := make([]Item, len(names))
	for i, n := range names {
		out[i] = Item{ID: n, Title: n}
	}
	return out
}

func prefixFilter(all []Item) FilterFunc {
	return func(query string) []Item {
		if query == "" {
			return all
		}
		var out []Item
		for _, it := range all {
			if strings.Contains(strings.ToLower(it.Title), strings.ToLower(query)) {
				out = append(out, it)
			}
		}
		return out
	}
}

func testTheme() *theme.Theme {
	return theme.New(theme.Capabilities{Unicode: false, Attributes: false}, config.ThemeAuto)
}

func TestSelectorNavigationDoesNotWrap(t *testing.T) {
	// Wrapping makes it easy to overshoot and act on the wrong row — which, in
	// a Kubernetes tool, may mean the wrong cluster.
	s := NewSelector("Clusters", "", prefixFilter(items("a", "b", "c")))
	s.Render(testTheme(), 40, 10)

	s.Move(-1)
	if got, _ := s.Selected(); got.ID != "a" {
		t.Errorf("selected = %q, want to stay on the first row", got.ID)
	}
	s.Move(10)
	if got, _ := s.Selected(); got.ID != "c" {
		t.Errorf("selected = %q, want to stop on the last row", got.ID)
	}
}

func TestSelectorFilteringResetsCursor(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("prod-eu", "prod-us", "staging")))
	s.Render(testTheme(), 40, 10)
	s.Move(2)

	for _, r := range "prod" {
		s.HandleKey("", string(r))
	}
	if len(s.Items()) != 2 {
		t.Fatalf("got %d items, want 2", len(s.Items()))
	}
	if got, _ := s.Selected(); got.ID != "prod-eu" {
		t.Errorf("selected = %q, want the first match after filtering", got.ID)
	}
}

func TestSelectorKeepsSelectionAcrossRefresh(t *testing.T) {
	// A background reload must not move the user's cursor out from under them.
	all := items("alpha", "beta", "gamma")
	s := NewSelector("Namespaces", "", prefixFilter(all))
	s.Render(testTheme(), 40, 10)
	s.Move(1)

	s.Refresh()
	if got, _ := s.Selected(); got.ID != "beta" {
		t.Errorf("selected = %q, want beta to survive the refresh", got.ID)
	}
}

func TestSelectorSelectID(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("a", "b", "c")))
	s.SelectID("c")
	if got, _ := s.Selected(); got.ID != "c" {
		t.Errorf("selected = %q, want c", got.ID)
	}
	s.SelectID("missing")
	if got, _ := s.Selected(); got.ID != "c" {
		t.Errorf("selecting a missing id must not move the cursor, got %q", got.ID)
	}
}

func TestSelectorEmptyResultHasNoSelection(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("a")))
	for _, r := range "zzz" {
		s.HandleKey("", string(r))
	}
	if _, ok := s.Selected(); ok {
		t.Error("an empty result must not report a selection")
	}
}

func TestSelectorScrollsLongLists(t *testing.T) {
	names := make([]string, 100)
	for i := range names {
		names[i] = "ns-" + itoa(i)
	}
	s := NewSelector("Namespaces", "", prefixFilter(items(names...)))
	s.Render(testTheme(), 40, 10)

	s.Move(60)
	out := s.Render(testTheme(), 40, 10)
	if !strings.Contains(out, "ns-60") {
		t.Error("the selected row must be scrolled into view")
	}
	if strings.Contains(out, "ns-0\n") {
		t.Error("rows far above the selection must be scrolled out")
	}
}

func TestSelectorRenderRespectsWidth(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items(
		"a-very-long-context-name-that-will-not-fit-in-a-narrow-terminal",
		"short",
	)))
	const width = 30
	for _, line := range strings.Split(s.Render(testTheme(), width, 8), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %q is %d cells wide, want at most %d", line, w, width)
		}
	}
}

func TestSelectorRenderFillsHeight(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("a", "b")))
	const height = 9
	if got := len(strings.Split(s.Render(testTheme(), 40, height), "\n")); got != height {
		t.Errorf("rendered %d lines, want exactly %d so the overlay does not jitter", got, height)
	}
}

func TestSelectorClickSelectsRow(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("a", "b", "c")))
	s.Render(testTheme(), 40, 10)

	if !s.ClickRow(2) {
		t.Fatal("clicking an enabled row must select it")
	}
	if got, _ := s.Selected(); got.ID != "c" {
		t.Errorf("selected = %q, want c", got.ID)
	}
	if s.ClickRow(99) {
		t.Error("clicking past the end must not select anything")
	}
}

func TestSelectorDisabledRowIsNotChoosable(t *testing.T) {
	s := NewSelector("Namespaces", "", func(string) []Item {
		return []Item{{ID: "info", Title: "Loading…", Disabled: true}, {ID: "a", Title: "a"}}
	})
	s.Render(testTheme(), 40, 10)
	if s.ClickRow(0) {
		t.Error("a disabled row must not be choosable")
	}
}

func TestSelectorResetClearsQuery(t *testing.T) {
	s := NewSelector("Clusters", "", prefixFilter(items("alpha", "beta")))
	for _, r := range "be" {
		s.HandleKey("", string(r))
	}
	s.Reset()
	if s.Query() != "" {
		t.Errorf("query = %q, want empty", s.Query())
	}
	if len(s.Items()) != 2 {
		t.Errorf("got %d items after reset, want the unfiltered list", len(s.Items()))
	}
}
