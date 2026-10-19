package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func TestLuxSitsBesideTheVersionInTheHeader(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))

	first := strings.SplitN(plainView(m), "\n", 2)[0]
	if !strings.Contains(first, "{ʘ,ʘ}") {
		t.Errorf("the header's first line must carry Lux:\n%s", first)
	}
}

func TestLuxFallsBackToASCII(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.Env = theme.MapEnv(map[string]string{"TERM": "dumb", "LANG": "C"})
	})
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))

	out := plainView(m)
	if strings.Contains(out, "ʘ") {
		t.Errorf("a terminal without Unicode must not get the Unicode owl:\n%s", out)
	}
	if !strings.Contains(out, "{o,o}") {
		t.Errorf("the ASCII owl must be drawn instead:\n%s", out)
	}
}

func TestAQuietDashboardSaysSoInLuxsVoice(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3),
		testApplication("frontend", application.Healthy, 2, 2))

	if !strings.Contains(plainView(m), "All quiet. Lux is keeping watch") {
		t.Errorf("an all-healthy dashboard must say it is quiet:\n%s", plainView(m))
	}
}

func TestABrokenDashboardIsNeverCalledQuiet(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication(), testApplication("frontend", application.Healthy, 2, 2))

	if strings.Contains(plainView(m), "All quiet") {
		t.Errorf("a dashboard with a broken application is not quiet:\n%s", plainView(m))
	}
}

func TestTheExplanationOpensCalmly(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	press(t, m, "ctrl+w")

	if !strings.Contains(plainView(m), "Breathe. This is what the cluster itself said") {
		t.Errorf("findings must open with Lux's line:\n%s", plainView(m))
	}
	_, report, _ := m.copyTarget()
	if strings.Contains(report, "Breathe") {
		t.Error("the pasted report is evidence for other people and carries no tone")
	}
}

func TestHelpIntroducesLux(t *testing.T) {
	m := newTestModel(t)
	if !strings.Contains(m.helpTextAt(60), "Lux keeps watch.") {
		t.Error("the help must introduce Lux")
	}
}
