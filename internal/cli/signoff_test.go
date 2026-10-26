package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/ui/theme"
)

func TestLuxSignsOffForTheTimeOfDay(t *testing.T) {
	g := theme.GlyphsFor(false)
	day := signOff(time.Date(2026, 10, 20, 15, 0, 0, 0, time.Local), &g)
	night := signOff(time.Date(2026, 10, 20, 2, 0, 0, 0, time.Local), &g)
	if !strings.HasPrefix(day, g.Owl) || !strings.Contains(day, "See you soon.") {
		t.Errorf("day: %q", day)
	}
	if !strings.HasPrefix(night, g.OwlSleepy) || !strings.Contains(night, "Sleep well.") {
		t.Errorf("night: %q", night)
	}
}
