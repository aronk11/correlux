package components

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func plainTheme() *theme.Theme {
	return theme.New(theme.Capabilities{Unicode: true}, config.ThemeDark)
}

func hints() []KeyHint {
	return []KeyHint{
		{Key: "↑↓", Desc: "Move", Priority: 90},
		{Key: "ctrl+p", Desc: "Commands", Priority: 100},
		{Key: "ctrl+r", Desc: "Refresh", Priority: 60},
		{Key: "ctrl+c", Desc: "Quit", Priority: 40},
		{Key: "?", Desc: "Help", Priority: 95},
	}
}

func TestStatusBarKeepsTheMostUsefulKeys(t *testing.T) {
	// Wide enough for three hints only: the two least important go, wherever
	// they sit in the list.
	out := RenderStatus(plainTheme(), StatusData{Hints: hints()}, 34)

	for _, want := range []string{"Commands", "Help", "Move"} {
		if !strings.Contains(out, want) {
			t.Errorf("a narrow bar must keep %q: %q", want, out)
		}
	}
	for _, gone := range []string{"Quit", "Refresh"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should have been dropped first: %q", gone, out)
		}
	}
}

func TestStatusBarKeepsTheGivenOrder(t *testing.T) {
	out := RenderStatus(plainTheme(), StatusData{Hints: hints()}, 60)
	move, help := strings.Index(out, "Move"), strings.Index(out, "Help")
	if move < 0 || help < 0 || move > help {
		t.Errorf("surviving hints must keep the order they were given: %q", out)
	}
}

func TestStatusBarShowsEverythingWhenItFits(t *testing.T) {
	out := RenderStatus(plainTheme(), StatusData{Hints: hints()}, 120)
	for _, want := range []string{"Move", "Commands", "Refresh", "Quit", "Help"} {
		if !strings.Contains(out, want) {
			t.Errorf("a wide bar must show %q: %q", want, out)
		}
	}
}

func TestAMessageReplacesTheHints(t *testing.T) {
	out := RenderStatus(plainTheme(), StatusData{
		Message:       "Auto-refresh every 10s",
		MessageStatus: theme.StatusHealthy,
		Hints:         hints(),
	}, 80)
	if !strings.Contains(out, "Auto-refresh every 10s") {
		t.Errorf("the message must be shown: %q", out)
	}
	if strings.Contains(out, "Commands") {
		t.Errorf("a message replaces the hints rather than sharing the line: %q", out)
	}
}
