package app

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func TestLuxSitsBesideTheVersionInTheHeader(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))

	// The glyph set depends on the platform's terminal detection (Windows
	// consoles get ASCII), so the test asks the theme which owl it drew.
	first := strings.SplitN(plainView(m), "\n", 2)[0]
	if !strings.Contains(first, m.theme.Glyphs.Owl) {
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

func at(hour int) func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 20, hour, 7, 0, 0, time.Local) }
}

func TestAQuietDashboardSaysSoInLuxsVoice(t *testing.T) {
	m := newTestModel(t)
	m.now = at(14)
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

func TestAtNightLuxSendsYouBackToBed(t *testing.T) {
	m := newTestModel(t)
	m.now = at(3)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))

	if !strings.Contains(plainView(m), "All quiet. Go back to sleep, Lux keeps watch.") {
		t.Errorf("a quiet dashboard at 3 a.m. must say so:\n%s", plainView(m))
	}
}

func TestARecoveryIsCheeredOnceAndNeverOnTheFirstLoad(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	if strings.Contains(m.message, "healthy again") {
		t.Fatal("an application seen for the first time has not recovered")
	}

	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	if !strings.Contains(m.message, "payments is healthy again. Nice work.") || m.messageStatus != theme.StatusHealthy {
		t.Errorf("a recovery deserves one line; got %q (%v)", m.message, m.messageStatus)
	}

	m.message = ""
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	if m.message != "" {
		t.Errorf("staying healthy is not news; got %q", m.message)
	}
}

func TestADegradedExplanationOpensMoreGently(t *testing.T) {
	m := newTestModel(t)
	a := brokenApplication()
	a.Health = application.Degraded
	for i := range a.Pods {
		a.Pods[i].Reason, a.Pods[i].Ready = "", true
		a.Pods[i].Containers[0].State, a.Pods[i].Containers[0].Reason, a.Pods[i].Containers[0].Ready = "running", "", true
	}
	loadApplicationsInto(m, a)
	press(t, m, "ctrl+w")

	out := plainView(m)
	if strings.Contains(out, "Breathe") || !strings.Contains(out, "Nothing is down.") {
		t.Errorf("a degraded application is not an emergency:\n%s", out)
	}
}

func TestLuxOnlyClaimsToHaveLookedOnceTheEvidenceIsIn(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	press(t, m, "ctrl+w")
	if strings.Contains(plainView(m), "looked everywhere") {
		t.Fatal("before the evidence lands, Lux has not looked anywhere yet")
	}
	loadEvidenceInto(m, &application.Context{})
	if !strings.Contains(plainView(m), "Lux looked everywhere. Nothing is wrong with payments") {
		t.Errorf("once it has, it may say so:\n%s", plainView(m))
	}
}
