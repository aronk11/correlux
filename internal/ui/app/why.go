package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/diagnosis"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// rediagnose recomputes every application's findings.
//
// It runs when applications or evidence arrive, not while rendering: View is a
// pure function of the model and must stay cheap enough to run on every
// keystroke, and diagnosing a hundred applications is not.
func (m *Model) rediagnose() {
	apps := m.applications()
	if len(apps) == 0 {
		m.findings = nil
		return
	}
	list := m.apps.Get()
	evidence := m.evidence.Get()

	findings := make(map[string][]diagnosis.Diagnosis, len(apps))
	for i := range apps {
		out := diagnosis.Diagnose(&diagnosis.Input{
			App:     apps[i],
			Context: evidence,
			Scope:   list.Snapshot,
			Now:     time.Now(),
		})
		if len(out) > 0 {
			findings[apps[i].Key()] = out
		}
	}
	m.findings = findings
}

// findingsFor returns the diagnoses of one application.
func (m *Model) findingsFor(key string) []diagnosis.Diagnosis { return m.findings[key] }

// diagnosedHealth folds failures that live outside replica status (for
// example, a Service with no endpoints) into the status shown after evidence
// has been loaded. Before then the dashboard keeps the workload's observed
// health rather than pretending it has already checked delivery.
func diagnosedHealth(base application.Health, findings []diagnosis.Diagnosis) application.Health {
	out := base
	for _, finding := range findings {
		switch finding.Severity {
		case diagnosis.Critical:
			return application.Down
		case diagnosis.Warning:
			if out == application.Healthy || out == application.Unknown {
				out = application.Degraded
			}
		}
	}
	return out
}

// explain opens the WHY view for the application in hand: the one that is open,
// or the one the dashboard cursor is on.
func (m *Model) explain() tea.Cmd {
	if m.selectedApp == "" || m.view == viewApplications {
		apps := m.applications()
		if m.appPort.Cursor < 0 || m.appPort.Cursor >= len(apps) {
			m.notice("Select an application first", theme.StatusWarning)
			return m.expireNotice()
		}
		m.selectedApp = apps[m.appPort.Cursor].Key()
	}
	m.view = viewWhy
	m.whyPort.Offset = 0
	m.rebuildCommands()

	// The explanation is only as good as the evidence; fetch it if this scope
	// has not been read yet.
	if m.evidence.State() == async.Idle {
		return m.loadEvidence()
	}
	return nil
}

// explainApplication opens the WHY view for a named application.
func (m *Model) explainApplication(name string) tea.Cmd {
	cmd := m.openApplication(name)
	if m.view != viewApplication {
		return cmd
	}
	return tea.Batch(cmd, m.explain())
}

// whyVisible is the height available for the explanation.
func (m *Model) whyLines() int {
	return m.whyData().LineCount(m.screen.Body.Width)
}

func (m *Model) scrollWhy(delta int) {
	m.whyPort.ScrollLines(delta, m.whyLines(), m.bodyHeight())
}

// whyData assembles the explanation for the open application.
func (m *Model) whyData() screens.WhyData {
	d := screens.WhyData{Offset: m.whyPort.Offset}

	switch m.apps.State() {
	case async.Idle, async.Loading:
		d.Message = "Loading…"
		return d
	case async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d
	}

	app, ok := m.currentApplication()
	if !ok {
		d.Message = "Application " + m.selectedApp + " is no longer in " + m.scopeLabel() + "."
		d.MessageStatus = theme.StatusWarning
		return d
	}

	findings := m.findingsFor(app.Key())
	health := diagnosedHealth(app.Health, findings)
	status := healthStatus(health)
	d.Name = app.Name
	d.Namespace = app.Namespace
	d.Health = health.String()
	d.HealthGlyph = m.theme.Glyph(status)
	d.HealthStatus = status
	d.Summary = app.Summary
	d.Notes = m.evidenceNotes()
	d.Empty = "Nothing is wrong with " + app.Name + " that Correlux can see."

	now := time.Now()
	for _, f := range findings {
		d.Findings = append(d.Findings, m.whyFinding(f, now))
	}
	if len(findings) > 0 {
		d.Next = m.whyNextActions(&app)
	}
	return d
}

// whyNextActions is what a person can actually do next, in the keys this build
// actually binds. A hint for a key that does nothing here would be a lie the
// screen tells about itself, so every entry is checked against what is really
// reachable before it is offered.
func (m *Model) whyNextActions(app *application.Application) []screens.WhyAction {
	var actions []screens.WhyAction
	if key := m.keys.Key(ActionLogs); key != "" && logsExist(app) {
		text := "read the pods' logs"
		if previousRunExists(app) {
			// Pressing the key opens exactly this: openLogs sets Previous once
			// it sees the same signal (logs.go), so the hint stays true.
			text = "read the previous run's logs"
		}
		actions = append(actions, screens.WhyAction{Key: key, Text: text})
	}
	// Enter is not part of the configurable key map — it is wired directly in
	// handleWhyKey — but it is always reachable from here, to the objects the
	// explanation is about.
	actions = append(actions, screens.WhyAction{Key: "enter", Text: "inspect the objects involved"})
	return actions
}

func logsExist(app *application.Application) bool {
	for i := range app.Pods {
		for _, c := range app.Pods[i].Containers {
			if c.State == "running" || c.State == "terminated" || c.LastReason != "" || c.LastExitCode != 0 {
				return true
			}
		}
	}
	return false
}

// previousRunExists reports whether any of the application's pods carries a
// previous container run worth reading — the exact signal the crash-loop and
// out-of-memory rules read to explain a failure (pods.go). It is what makes
// "read the previous run's logs" true rather than assumed.
func previousRunExists(app *application.Application) bool {
	for i := range app.Pods {
		for _, c := range app.Pods[i].Containers {
			if c.OOMKilled || c.LastExitCode != 0 {
				return true
			}
		}
	}
	return false
}

// evidenceNotes says what the answer is missing, so a thin explanation is never
// mistaken for a healthy cluster.
func (m *Model) evidenceNotes() []string {
	var notes []string
	switch m.evidence.State() {
	case async.Idle:
		notes = append(notes, "Events, endpoints and node state have not been read yet.")
	case async.Loading:
		notes = append(notes, "Reading events, endpoints and node state…")
	case async.Failed:
		notes = append(notes, "Evidence unavailable: "+shortError(m.evidence.Err()))
	default:
		if gaps := m.evidence.Get().Gaps; len(gaps) > 0 {
			notes = append(notes, gapSummary(gaps))
		}
	}
	if gaps := m.apps.Get().Snapshot.Gaps; len(gaps) > 0 {
		notes = append(notes, gapSummary(gaps))
	}
	return notes
}

func (m *Model) whyFinding(f diagnosis.Diagnosis, now time.Time) screens.WhyFinding {
	status := severityStatus(f.Severity)
	out := screens.WhyFinding{
		Glyph:      m.theme.Glyph(status),
		Status:     status,
		Severity:   f.Severity.String(),
		Problem:    f.Problem,
		Cause:      f.Cause,
		Unknown:    f.Unknown,
		Chain:      f.Chain,
		Confidence: f.Confidence.String(),
	}
	for _, e := range f.Evidence {
		item := screens.WhyEvidence{Source: e.Kind + "/" + e.Name, Detail: e.Detail}
		if !e.At.IsZero() {
			item.At = formatAge(e.At, now) + " ago"
		}
		out.Evidence = append(out.Evidence, item)
	}
	for _, s := range f.Suggestions {
		out.Suggestions = append(out.Suggestions, screens.WhySuggestion{Text: s.Text, Command: s.Command})
	}
	return out
}

// severityStatus maps a finding's severity onto the theme's states.
func severityStatus(s diagnosis.Severity) theme.Status {
	switch s {
	case diagnosis.Critical:
		return theme.StatusCritical
	case diagnosis.Warning:
		return theme.StatusWarning
	default:
		return theme.StatusUnknown
	}
}

// incidentLabel is the one-line version of an application's worst finding, for
// the dashboard's detail column.
func (m *Model) incidentLabel(app *application.Application) string {
	primary, ok := diagnosis.Primary(m.findingsFor(app.Key()))
	if !ok {
		return ""
	}
	if primary.Cause == "" {
		return primary.Problem
	}
	return primary.Problem + " — " + strings.TrimSuffix(primary.Cause, ".")
}
