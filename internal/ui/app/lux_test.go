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
	m.now = at(14)
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
	m.now = at(14)
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

	if !strings.Contains(plainView(m), quietLine(at(14)())) || !strings.HasPrefix(quietLine(at(14)()), "All quiet.") {
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
	if !strings.Contains(m.message, "payments is healthy again. ") || m.messageStatus != theme.StatusHealthy {
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

func TestLuxSaysHelloOnceAndOnlyToAQuietCluster(t *testing.T) {
	m := newTestModel(t)
	m.now = at(8)
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	if !strings.Contains(plainView(m), "Good morning. Lux has the coffee on. All quiet.") {
		t.Errorf("the first quiet dashboard opens with a hello:\n%s", plainView(m))
	}
	if m.message != "" {
		t.Errorf("a hello never takes the status line from the keys; got %q", m.message)
	}
	m.Update(greetingDoneMsg{})
	if strings.Contains(plainView(m), "Good morning") {
		t.Error("the hello goes away")
	}
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	if strings.Contains(plainView(m), "Good morning") {
		t.Error("and it is said once per session, not on every reload")
	}

	broken := newTestModel(t)
	broken.now = at(8)
	loadApplicationsInto(broken, brokenApplication())
	if strings.Contains(plainView(broken), "Good morning") {
		t.Error("an incident is not the moment for small talk")
	}
}

func TestLuxsFaceEchoesTheScreen(t *testing.T) {
	m := newTestModel(t)
	g := m.theme.Glyphs
	m.now = at(14)
	loadApplicationsInto(m, brokenApplication())
	if m.luxFace() != g.OwlAlarm {
		t.Errorf("something down: face %q, want %q", m.luxFace(), g.OwlAlarm)
	}
	loadApplicationsInto(m, testApplication("payments", application.Healthy, 3, 3))
	if m.luxFace() != g.OwlHappy {
		t.Errorf("a recovery being cheered: face %q, want %q", m.luxFace(), g.OwlHappy)
	}
	m.message = ""
	if m.luxFace() != g.Owl {
		t.Errorf("a quiet afternoon: face %q, want %q", m.luxFace(), g.Owl)
	}
	m.now = at(2)
	if m.luxFace() != g.OwlSleepy {
		t.Errorf("a quiet night: face %q, want %q", m.luxFace(), g.OwlSleepy)
	}
}

func TestLuxsLinesChangeByTheDayNotByTheFrame(t *testing.T) {
	monday := time.Date(2026, 10, 19, 14, 0, 0, 0, time.Local)
	if quietLine(monday) != quietLine(monday.Add(time.Hour)) {
		t.Error("the same day keeps the same line, so nothing on screen flickers")
	}
	if quietLine(monday) == quietLine(monday.AddDate(0, 0, 1)) {
		t.Error("the next day brings a new one")
	}
	for i := range 2 * len(quietDays) {
		if line := quietLine(monday.AddDate(0, 0, i)); !strings.HasPrefix(line, "All quiet.") {
			t.Errorf("every quiet line still says it is quiet first: %q", line)
		}
	}
}

func TestEveryOwlHasAnASCIIFace(t *testing.T) {
	g := theme.GlyphsFor(false)
	for _, face := range []string{g.Owl, g.OwlAlarm, g.OwlSleepy, g.OwlHappy} {
		for _, r := range face {
			if r > 127 {
				t.Errorf("%q is not ASCII", face)
			}
		}
	}
}
