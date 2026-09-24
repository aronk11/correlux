package app

import (
	"strings"

	"github.com/aronk11/correlux/internal/domain/diagnosis"
	"github.com/aronk11/correlux/internal/ui/screens"
)

// renderDashboard draws the application table and, in the room a short table
// leaves below it, what needs attention (SPEC 4).
//
// The strip is only drawn when every row already fits: it must never take a
// line from the table, because the table is what is scrolled, selected and
// clicked, and an application pushed below the fold by a summary of itself is
// worse than no summary.
func (m *Model) renderDashboard(width, height int) string {
	d := m.frameTableFor()
	table := func(h int) string { return screens.RenderTable(m.theme, d, width, h) }
	if d.Message != "" || len(d.Rows) == 0 {
		return table(height)
	}
	tableHeight := len(d.Rows) + 1 // the heading
	spare := height - tableHeight - 1
	incidents := m.incidentLines(width, spare)
	if len(incidents) == 0 {
		return table(height)
	}
	return table(tableHeight) + "\n\n" + strings.Join(incidents, "\n")
}

// incidentLines is the strip itself: one line per application with a finding,
// in the table's order, each saying what is wrong and why. It needs a heading
// and at least one entry to be worth drawing at all.
func (m *Model) incidentLines(width, height int) []string {
	if height < 2 {
		return nil
	}
	apps := m.visibleApplications()
	type entry struct {
		text   string
		status diagnosis.Severity
	}
	var entries []entry
	nameWidth := 0
	for i := range apps {
		primary, ok := diagnosis.Primary(m.findingsFor(apps[i].Key()))
		if !ok || primary.Severity == diagnosis.Info {
			continue
		}
		nameWidth = max(nameWidth, len([]rune(apps[i].Name)))
	}
	for i := range apps {
		a := &apps[i]
		primary, ok := diagnosis.Primary(m.findingsFor(a.Key()))
		if !ok || primary.Severity == diagnosis.Info {
			continue
		}
		status := severityStatus(primary.Severity)
		text := m.theme.Glyph(status) + " " + padTo(a.Name, nameWidth) + "  " + m.incidentLabel(a)
		entries = append(entries, entry{text: text, status: primary.Severity})
	}
	if len(entries) == 0 {
		return nil
	}

	lines := []string{m.theme.PanelTitle.Render("NEEDS ATTENTION")}
	room := height - 1
	for i, e := range entries {
		if i == room-1 && len(entries) > room {
			more := len(entries) - i
			lines = append(lines, m.theme.Muted.Render(clipTo("…"+itoa(more)+" more; "+
				m.keys.Key(ActionWhy)+" on a row explains it", width, 1)))
			break
		}
		lines = append(lines, m.theme.Style(severityStatus(e.status)).Render(clipTo(e.text, width, 1)))
	}
	return lines
}
