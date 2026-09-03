package app

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const maxActivityEvents = 200

// openActivity shows the newest Kubernetes Events in the active scope. Events
// are operational breadcrumbs, not an audit trail, and the screen says so.
func (m *Model) openActivity() tea.Cmd {
	if m.view == viewActivity {
		return m.backToApplications()
	}
	m.stopLogs()
	m.stopFleet()
	m.view = viewActivity
	m.activityPort.Offset, m.activityPort.Cursor = 0, 0
	m.rebuildCommands()
	return m.loadEvidence()
}

func (m *Model) activityView() (screens.ApplicationData, []objectRef) {
	d := screens.ApplicationData{
		Name:         "Recent activity",
		Namespace:    m.scopeLabel(),
		Health:       "Kubernetes Events",
		HealthStatus: theme.StatusUnknown,
		Summary:      "newest first",
		Selected:     m.activityPort.Cursor,
		Offset:       m.activityPort.Offset,
		Notes: []string{
			"Events are retained briefly and are not an audit log; they may not identify who made a change.",
		},
	}
	switch m.evidence.State() {
	case async.Idle, async.Loading:
		d.Message = "Reading recent Kubernetes Events in " + m.scopeLabel() + "…"
		return d, nil
	case async.Failed:
		d.Message = "Recent activity is unavailable: " + shortError(m.evidence.Err())
		d.MessageStatus = theme.StatusCritical
		return d, nil
	}

	events := append([]application.Event(nil), m.evidence.Get().Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].LastSeen.After(events[j].LastSeen) })
	if len(events) > maxActivityEvents {
		events = events[:maxActivityEvents]
		d.Notes = append(d.Notes, "Showing the newest "+itoa(maxActivityEvents)+" events from this bounded read.")
	}

	section := screens.DetailSection{
		Title:   "Timeline",
		Columns: []string{"Age", "Namespace", "Type", "Reason", "Object", "Count", "Message"},
		Empty:   "No Kubernetes Events were returned for this scope.",
	}
	now := time.Now()
	targets := make([]objectRef, 0, len(events))
	for i := range events {
		e := &events[i]
		target := -1
		if e.About.Kind != "" && e.About.Name != "" {
			target = len(targets)
			targets = append(targets, objectRef{Kind: e.About.Kind, Name: e.About.Name, Namespace: e.Namespace})
		}
		row := screens.DetailRow{Cells: []string{
			formatAge(e.LastSeen, now), orNone(e.Namespace), e.Type, e.Reason,
			e.About.Kind + "/" + e.About.Name, itoa(int(e.Count)), e.Message,
		}, Target: target}
		if e.Type == "Warning" {
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	d.Sections = []screens.DetailSection{section}
	return d, targets
}

func (m *Model) activityData() screens.ApplicationData {
	d, _ := m.activityView()
	return d
}

func (m *Model) handleActivityKey(key string) (tea.Cmd, bool) {
	page := max(m.screen.Body.Height-1, 1)
	switch key {
	case "up", "k":
		m.moveActivityCursor(-1)
	case "down", "j":
		m.moveActivityCursor(1)
	case "pgup":
		m.activityPort.ScrollLines(-page, m.activityData().LineCount(m.screen.Body.Width), m.bodyHeight())
	case "pgdown", " ":
		m.activityPort.ScrollLines(page, m.activityData().LineCount(m.screen.Body.Width), m.bodyHeight())
	case "enter", "right":
		_, targets := m.activityView()
		if m.activityPort.Cursor >= 0 && m.activityPort.Cursor < len(targets) {
			return m.openObject(targets[m.activityPort.Cursor]), true
		}
	case "esc", "left", "h":
		return m.backToApplications(), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) moveActivityCursor(delta int) {
	d, targets := m.activityView()
	m.activityPort.MoveTarget(delta, d.TargetLines(m.screen.Body.Width), len(targets),
		d.LineCount(m.screen.Body.Width), m.bodyHeight())
}
