package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	"github.com/aronk11/kubeui/internal/ui/async"
	"github.com/aronk11/kubeui/internal/ui/screens"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// applicationList is one load of the dashboard: the grouped applications and
// the snapshot they were derived from, kept together so the UI can qualify what
// it shows with what the snapshot could not read.
type applicationList struct {
	Apps     []application.Application
	Snapshot application.Snapshot
}

// applications returns the loaded applications, or nothing while the first load
// is still in flight.
func (m *Model) applications() []application.Application { return m.apps.Get().Apps }

// currentApplication resolves the detail view's subject by key. Looking it up
// on every render rather than copying it means a refresh updates the open
// application in place, which is exactly what a user watching a rollout wants.
func (m *Model) currentApplication() (application.Application, bool) {
	for _, a := range m.applications() {
		if a.Key() == m.selectedApp {
			return a, true
		}
	}
	return application.Application{}, false
}

// openApplication opens one application, addressed the way the user sees it:
// by name, optionally namespace-qualified.
func (m *Model) openApplication(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for _, a := range m.applications() {
		if a.Key() == name || a.Name == name {
			m.selectedApp = a.Key()
			m.view = viewApplication
			m.detailOffset = 0
			m.rebuildCommands()
			return nil
		}
	}
	m.notice("No application named "+name+" in "+m.scopeLabel(), theme.StatusWarning)
	return m.expireNotice()
}

// openSelectedApplication opens whatever the dashboard cursor is on.
func (m *Model) openSelectedApplication() tea.Cmd {
	apps := m.applications()
	if m.appCursor < 0 || m.appCursor >= len(apps) {
		return nil
	}
	return m.openApplication(apps[m.appCursor].Key())
}

// backToApplications returns to the dashboard from anywhere.
func (m *Model) backToApplications() tea.Cmd {
	if m.view == viewApplications {
		return nil
	}
	if m.view == viewTable && m.cancelTable != nil {
		m.cancelTable()
		m.table.Reset()
	}
	m.view = viewApplications
	m.detailOffset = 0
	m.rebuildCommands()
	return nil
}

// applicationsVisible is how many application rows fit under the column header.
func (m *Model) applicationsVisible() int { return max(m.screen.Body.Height-1, 1) }

// moveAppCursor scrolls the dashboard.
func (m *Model) moveAppCursor(delta int) {
	apps := m.applications()
	if len(apps) == 0 {
		return
	}
	m.appCursor = clampInt(m.appCursor+delta, len(apps)-1)

	visible := m.applicationsVisible()
	if m.appCursor < m.appOffset {
		m.appOffset = m.appCursor
	}
	if m.appCursor >= m.appOffset+visible {
		m.appOffset = m.appCursor - visible + 1
	}
	m.appOffset = clampInt(m.appOffset, max(len(apps)-visible, 0))
}

// keepCursorOnApplication restores the cursor after a reload.
//
// The list is sorted worst first, so a refresh can move every row: an
// application that just started failing jumps to the top and would drag the
// cursor onto a different application under the user's hands. The cursor
// follows the application it was on, not the position it was at.
func (m *Model) keepCursorOnApplication(previous string) {
	apps := m.applications()
	if len(apps) == 0 {
		m.appCursor, m.appOffset = 0, 0
		return
	}
	if previous != "" {
		for i, a := range apps {
			if a.Key() == previous {
				m.appCursor = i
				break
			}
		}
	}
	m.appCursor = clampInt(m.appCursor, len(apps)-1)

	visible := m.applicationsVisible()
	if m.appCursor < m.appOffset {
		m.appOffset = m.appCursor
	}
	if m.appCursor >= m.appOffset+visible {
		m.appOffset = m.appCursor - visible + 1
	}
	m.appOffset = clampInt(m.appOffset, max(len(apps)-visible, 0))
}

// cursorApplicationKey is the application the cursor is on, for restoring it
// across a reload.
func (m *Model) cursorApplicationKey() string {
	apps := m.applications()
	if m.appCursor >= 0 && m.appCursor < len(apps) {
		return apps[m.appCursor].Key()
	}
	return ""
}

// healthStatus maps an application's health onto the theme's four states, so
// the glyph, the word and the colour always agree.
func healthStatus(h application.Health) theme.Status {
	switch h {
	case application.Healthy:
		return theme.StatusHealthy
	case application.Degraded:
		return theme.StatusWarning
	case application.Down:
		return theme.StatusCritical
	default:
		return theme.StatusUnknown
	}
}

// applicationsData renders the dashboard as a table, reusing the same component
// the resource browser uses: sorting, column fitting and cursor behaviour must
// not differ between two screens that look like tables (SPEC 14).
func (m *Model) applicationsData() screens.TableData {
	d := screens.TableData{Cursor: m.appCursor, Offset: m.appOffset, ShowWide: m.tableWide}

	switch m.apps.State() {
	case async.Idle, async.Loading:
		d.Message = "Looking for applications in " + m.scopeLabel() + "…"
		return d
	case async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d
	}

	apps := m.applications()
	if len(apps) == 0 {
		d.Message = "No applications in " + m.scopeLabel() + "."
		if gaps := m.apps.Get().Snapshot.Gaps; len(gaps) > 0 {
			d.Message += " " + gapSummary(gaps)
			d.MessageStatus = theme.StatusWarning
		}
		return d
	}

	d.Columns = []screens.TableColumn{
		{Title: "Status"},
		{Title: "Application"},
		{Title: "Namespace", Wide: !m.allNamespaces},
		{Title: "Pods"},
		{Title: "Workloads", Wide: true},
		{Title: "Restarts", Right: true},
		{Title: "Age"},
		{Title: "Detail"},
	}

	now := time.Now()
	d.Rows = make([]screens.TableRow, 0, len(apps))
	for _, a := range apps {
		status := healthStatus(a.Health)
		d.Rows = append(d.Rows, screens.TableRow{
			Status: status,
			Cells: []string{
				m.theme.Glyph(status) + " " + a.Health.String(),
				a.Name,
				a.Namespace,
				itoa(int(a.ReadyPods)) + "/" + itoa(int(a.DesiredPods)),
				workloadSummary(a),
				itoa(int(a.Restarts)),
				formatAge(a.CreatedAt, now),
				applicationDetail(a),
			},
		})
	}
	return d
}

// applicationDetail is the one thing worth reading about a row: what is wrong
// with it, or nothing at all when it is healthy.
func applicationDetail(a application.Application) string {
	if problems := a.ProblemSummary(); problems != "" {
		return problems
	}
	if a.Health != application.Healthy {
		return a.Summary
	}
	return ""
}

// workloadSummary names the workload when there is one, and counts them when
// there are several: "Deployment" says more than "1 workload".
func workloadSummary(a application.Application) string {
	switch len(a.Workloads) {
	case 0:
		return "—"
	case 1:
		return a.Workloads[0].Kind
	default:
		kinds := map[string]int{}
		for _, w := range a.Workloads {
			kinds[w.Kind]++
		}
		if len(kinds) == 1 {
			return itoa(len(a.Workloads)) + " " + a.Workloads[0].Kind + "s"
		}
		return itoa(len(a.Workloads)) + " workloads"
	}
}

// applicationData assembles the detail view.
func (m *Model) applicationData() screens.ApplicationData {
	d := screens.ApplicationData{Offset: m.detailOffset}

	switch m.apps.State() {
	case async.Idle, async.Loading:
		d.Message = "Loading…"
		return d
	case async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d
	}

	a, ok := m.currentApplication()
	if !ok {
		// It was deleted, or the scope moved out from under it. Saying so beats
		// an empty screen that looks like a bug.
		d.Message = "Application " + m.selectedApp + " is no longer in " + m.scopeLabel() + "."
		d.MessageStatus = theme.StatusWarning
		return d
	}

	status := healthStatus(a.Health)
	d.Name = a.Name
	d.Namespace = a.Namespace
	d.Health = a.Health.String()
	d.HealthGlyph = m.theme.Glyph(status)
	d.HealthStatus = status
	d.Summary = a.Summary
	if gaps := m.apps.Get().Snapshot.Gaps; len(gaps) > 0 {
		d.Notes = append(d.Notes, gapSummary(gaps))
	}

	now := time.Now()
	workloads := screens.DetailSection{
		Title:   "Workloads",
		Columns: []string{"Kind", "Name", "Ready", "Age"},
		Empty:   "no workload owns these pods",
	}
	for _, w := range a.Workloads {
		ready := "—"
		if w.Replicated {
			ready = itoa(int(w.Ready)) + "/" + itoa(int(w.Desired))
		}
		row := screens.DetailRow{Cells: []string{w.Kind, w.Name, ready, formatAge(w.CreatedAt, now)}}
		switch {
		case w.Replicated && w.Desired > 0 && w.Ready == 0:
			row.Status = theme.StatusCritical
		case w.Replicated && w.Ready < w.Desired:
			row.Status = theme.StatusWarning
		}
		workloads.Rows = append(workloads.Rows, row)
	}

	pods := screens.DetailSection{
		Title:   "Pods",
		Columns: []string{"Name", "Phase", "Ready", "Restarts", "Node", "State"},
		Empty:   "no pods",
	}
	for _, p := range a.Pods {
		row := screens.DetailRow{Cells: []string{
			p.Name, p.Phase, readyLabel(p.Ready), itoa(int(p.Restarts)), orNone(p.Node), orNone(p.Reason),
		}}
		switch {
		case p.Reason != "" && !p.Terminal():
			row.Status = theme.StatusCritical
		case !p.Ready && !p.Terminal():
			row.Status = theme.StatusWarning
		}
		pods.Rows = append(pods.Rows, row)
	}

	network := screens.DetailSection{
		Title:   "Network",
		Columns: []string{"Kind", "Name", "Detail"},
		Empty:   "no service or ingress belongs to this application",
	}
	for _, s := range a.Services {
		detail := s.Type
		if len(s.Ports) > 0 {
			detail += "  " + strings.Join(s.Ports, ",")
		}
		network.Rows = append(network.Rows, screens.DetailRow{Cells: []string{"Service", s.Name, detail}})
	}
	for _, i := range a.Ingresses {
		network.Rows = append(network.Rows, screens.DetailRow{
			Cells: []string{"Ingress", i.Name, strings.Join(i.Hosts, ", ")},
		})
	}

	d.Sections = []screens.DetailSection{workloads, pods, network}
	return d
}

func readyLabel(ready bool) string {
	if ready {
		return "yes"
	}
	return "no"
}

// gapSummary states what the snapshot could not read, so an application that
// looks thin is never mistaken for one that is thin.
func gapSummary(gaps []application.Gap) string {
	parts := make([]string, 0, len(gaps))
	for _, g := range gaps {
		parts = append(parts, g.Kind+"s "+g.Reason)
	}
	return "Not shown: " + strings.Join(parts, "; ") + "."
}

// applicationsNotice turns a failed load into one actionable line.
func applicationsNotice(err error) string {
	return "Could not list applications: " + kubeclient.FriendlyError(err)
}

// formatAge renders an object's age the way kubectl does: one or two units,
// largest first, so a column of ages is scannable.
func formatAge(created, now time.Time) string {
	if created.IsZero() {
		return "—"
	}
	d := now.Sub(created)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h" + itoa(int(d%time.Hour/time.Minute)) + "m"
	case d < 100*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d" + itoa(int(d%(24*time.Hour)/time.Hour)) + "h"
	default:
		return itoa(int(d/(24*time.Hour))) + "d"
	}
}
