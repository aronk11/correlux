package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
)

// Lux's small talk. Every line here is decoration: it never replaces a word
// the screen needs, it rotates by the day rather than by the frame so nothing
// on screen flickers, and it always gives way to news.

// quietDays are the quiet-dashboard lines, one per day in turn.
var quietDays = []string{
	"All quiet. Lux is keeping watch, so you don't have to.",
	"All quiet. Nothing to see here, Lux checked twice.",
	"All quiet. Every pod is in its nest.",
	"All quiet. Lux is pretending to be busy.",
	"All quiet. A fine day to finally clean up that backlog.",
	"All quiet. Lux approves of this cluster.",
	"All quiet. Even the CrashLoopBackOffs took the day off.",
}

// quietNight is the one line for the small hours: there is only one thing
// worth saying to somebody watching a healthy cluster at 3 a.m.
const quietNight = "All quiet. Go back to sleep, Lux keeps watch."

// cheers close a recovery notice.
var cheers = []string{
	"Nice work.",
	"Lux does a little happy dance.",
	"Hoo-ray!",
	"That's the spirit.",
	"Lux knew you could.",
}

// pick chooses a line for the day, offset so two lists read on the same day
// do not march in step.
func pick(lines []string, t time.Time, offset int) string {
	return lines[(t.YearDay()+offset)%len(lines)]
}

// quietLine is what a dashboard with nothing to say says instead.
func quietLine(t time.Time) string {
	if lateNight(t) {
		return quietNight
	}
	return pick(quietDays, t, 0)
}

// greeting is Lux saying hello for the time of day.
func greeting(t time.Time) string {
	switch h := t.Hour(); {
	case h >= 5 && h < 11:
		return "Good morning. Lux has the coffee on."
	case h >= 11 && h < 17:
		return "Good afternoon. Lux is on watch."
	case h >= 17 && h < 22:
		return "Good evening. Lux will keep you company."
	default:
		return "Burning the midnight oil? Lux is right here."
	}
}

// greetingTTL is how long the hello stays before the quiet line stands alone.
const greetingTTL = 8 * time.Second

type greetingDoneMsg struct{}

// greet says hello once per session, on the first list that lands. It sits in
// front of the quiet line rather than on the status line, so it never hides a
// key or a warning, and a cluster that is not quiet is not greeted at all.
func (m *Model) greet() tea.Cmd {
	if m.greeted {
		return nil
	}
	m.greeted = true
	m.greeting = greeting(m.clock()) + " "
	return tea.Tick(greetingTTL, func(time.Time) tea.Msg { return greetingDoneMsg{} })
}

// luxFace is Lux's expression for the moment. It mirrors what the screen
// already says: wide-eyed while something visible is down, pleased while a
// recovery is being cheered, sleepy over a quiet dashboard at night.
func (m *Model) luxFace() string {
	g := m.theme.Glyphs
	switch {
	case m.anyDown():
		return g.OwlAlarm
	case m.message != "" && m.messageSeq == m.cheerSeq:
		return g.OwlHappy
	case lateNight(m.clock()) && m.allQuiet():
		return g.OwlSleepy
	}
	return g.Owl
}

func (m *Model) anyDown() bool {
	apps := m.visibleApplications()
	for i := range apps {
		if diagnosedHealth(apps[i].Health, m.findingsFor(apps[i].Key())) == application.Down {
			return true
		}
	}
	return false
}
