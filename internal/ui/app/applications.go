package app

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
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
	apps := m.applications()
	for i := range apps {
		if apps[i].Key() == m.selectedApp {
			return apps[i], true
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
	apps := m.applications()
	for i := range apps {
		a := &apps[i]
		if a.Key() == name || a.Name == name {
			m.selectedApp = a.Key()
			m.view = viewApplication
			m.detailPort.Offset, m.detailPort.Cursor = 0, 0
			m.rebuildCommands()
			// Opening an application is the moment its evidence becomes worth
			// fetching: the events belong on this screen, and the explanation
			// behind Ctrl+W is then already there.
			if m.evidence.State() == async.Idle {
				return m.loadEvidence()
			}
			return nil
		}
	}
	m.notice("No application named "+name+" in "+m.scopeLabel(), theme.StatusWarning)
	return m.expireNotice()
}

// openSelectedApplication opens whatever the dashboard cursor is on.
func (m *Model) openSelectedApplication() tea.Cmd {
	apps := m.visibleApplications()
	if m.appPort.Cursor < 0 || m.appPort.Cursor >= len(apps) {
		return nil
	}
	return m.openApplication(apps[m.appPort.Cursor].Key())
}

// toggleGrouping shows or hides why each object in the application belongs to
// it: the signal the grouper used, and whether that signal is a fact or a
// guess (ADR 16 — correlation must be explainable).
//
// Pressed from the dashboard it opens the application under the cursor and
// turns the section on, the same two-context pattern explain uses for the WHY
// view; pressed inside an open application it just toggles the section.
func (m *Model) toggleGrouping() tea.Cmd {
	if m.view == viewApplications {
		cmd := m.openSelectedApplication()
		if m.view != viewApplication {
			return cmd
		}
		m.groupingShown = true
		m.rebuildCommands()
		return cmd
	}
	if m.view != viewApplication {
		return nil
	}
	m.groupingShown = !m.groupingShown
	m.rebuildCommands()
	return nil
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
	m.detailPort.Offset = 0
	m.rebuildCommands()
	return nil
}

// applicationsVisible is how many application rows fit under the column header.
func (m *Model) applicationsVisible() int { return max(m.screen.Body.Height-1, 1) }

// moveAppCursor scrolls the dashboard.
func (m *Model) moveAppCursor(delta int) {
	m.appPort.MoveCursor(delta, len(m.visibleApplications()), m.applicationsVisible())
}

// keepCursorOnApplication restores the cursor after a reload.
//
// The list is sorted worst first, so a refresh can move every row: an
// application that just started failing jumps to the top and would drag the
// cursor onto a different application under the user's hands. The cursor
// follows the application it was on, not the position it was at.
func (m *Model) keepCursorOnApplication(previous string) {
	apps := m.visibleApplications()
	found := -1
	if previous != "" {
		for i := range apps {
			if apps[i].Key() == previous {
				found = i
				break
			}
		}
	}
	m.appPort.KeepCursor(found, len(apps), m.applicationsVisible())
}

// cursorApplicationKey is the application the cursor is on, for restoring it
// across a reload.
func (m *Model) cursorApplicationKey() string {
	apps := m.visibleApplications()
	if m.appPort.Cursor >= 0 && m.appPort.Cursor < len(apps) {
		return apps[m.appPort.Cursor].Key()
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
	d := screens.TableData{Cursor: m.appPort.Cursor, Offset: m.appPort.Offset, ShowWide: m.tableWide}

	switch m.apps.State() {
	case async.Idle, async.Loading:
		d.Message = "Looking for applications in " + m.scopeLabel() + "…"
		return d
	case async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d
	}

	apps := m.visibleApplications()
	if len(apps) == 0 && m.filtering() {
		d.Message = "Nothing matches " + m.query() + " among " +
			itoa(len(m.applications())) + " applications."
		return d
	}
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
		{Title: "Managed by", Wide: true},
		{Title: "Restarts", Right: true},
		{Title: "Age"},
		{Title: "Detail"},
	}

	now := time.Now()
	d.Rows = make([]screens.TableRow, 0, len(apps))
	for i := range apps {
		a := &apps[i]
		status := healthStatus(a.Health)
		d.Rows = append(d.Rows, screens.TableRow{
			Status: status,
			Cells: []string{
				m.theme.Glyph(status) + " " + a.Health.String(),
				a.Name,
				a.Namespace,
				itoa(int(a.ReadyPods)) + "/" + itoa(int(a.DesiredPods)),
				workloadSummary(a),
				managerCell(a.Manager),
				itoa(int(a.Restarts)),
				formatAge(a.CreatedAt, now),
				applicationDetail(a),
			},
		})
	}
	return d
}

// managerCell names who deployed an application, or says plainly that nothing
// claims it — which on a cluster run by Flux is itself worth noticing.
func managerCell(m application.Manager) string {
	if !m.Known() {
		return "—"
	}
	return m.Label()
}

// applicationDetail is the one thing worth reading about a row: what is wrong
// with it, or nothing at all when it is healthy.
func applicationDetail(a *application.Application) string {
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
func workloadSummary(a *application.Application) string {
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
	d, _ := m.applicationView()
	return d
}

// detailRow is one row of the detail view before its targets are numbered: it
// names the object the row opens, not an index into a list that does not exist
// until every section is in place.
type detailRow struct {
	Cells  []string
	Status theme.Status
	// Ref is what the row opens. An empty ref is a row to read rather than
	// somewhere to go.
	Ref objectRef
}

// detailSection is a section of those rows.
type detailSection struct {
	Title   string
	Columns []string
	Empty   string
	Rows    []detailRow
}

// numberTargets numbers the navigable rows in the order the sections are drawn,
// and returns the objects those numbers index.
//
// The numbering comes from the finished list of sections rather than from the
// code that fills them, because the two orders are not the same one: a number
// handed out while a section is being built follows the order the sections
// happen to be written in, and the cursor then moves down the list in an order
// nobody can see on screen.
func numberTargets(sections []detailSection) ([]screens.DetailSection, []objectRef) {
	rendered := make([]screens.DetailSection, 0, len(sections))
	var targets []objectRef
	for _, section := range sections {
		out := screens.DetailSection{Title: section.Title, Columns: section.Columns, Empty: section.Empty}
		for _, row := range section.Rows {
			target := -1
			if !row.Ref.empty() {
				targets = append(targets, row.Ref)
				target = len(targets) - 1
			}
			out.Rows = append(out.Rows, screens.DetailRow{Cells: row.Cells, Status: row.Status, Target: target})
		}
		rendered = append(rendered, out)
	}
	return rendered, targets
}

// applicationView assembles the detail view and, in the same pass, the list of
// objects its rows point at. One function for both, because a cursor that
// indexes a list built somewhere else is a bug waiting for the first time the
// two disagree.
func (m *Model) applicationView() (screens.ApplicationData, []objectRef) {
	d := screens.ApplicationData{Offset: m.detailPort.Offset, Selected: m.detailPort.Cursor}

	switch m.apps.State() {
	case async.Idle, async.Loading:
		d.Message = "Loading…"
		return d, nil
	case async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d, nil
	}

	a, ok := m.currentApplication()
	if !ok {
		// It was deleted, or the scope moved out from under it. Saying so beats
		// an empty screen that looks like a bug.
		d.Message = "Application " + m.selectedApp + " is no longer in " + m.scopeLabel() + "."
		d.MessageStatus = theme.StatusWarning
		return d, nil
	}

	status := healthStatus(a.Health)
	d.Name = a.Name
	d.Namespace = a.Namespace
	d.Health = a.Health.String()
	d.HealthGlyph = m.theme.Glyph(status)
	d.HealthStatus = status
	d.Summary = a.Summary
	if incident := m.incidentLabel(&a); incident != "" {
		d.Notes = append(d.Notes, incident)
	}
	if gaps := m.apps.Get().Snapshot.Gaps; len(gaps) > 0 {
		d.Notes = append(d.Notes, gapSummary(gaps))
	}

	now := time.Now()
	workloads := detailSection{
		Title:   "Workloads",
		Columns: []string{"Kind", "Name", "Ready", "Age"},
		Empty:   "no workload owns these pods",
	}
	for _, w := range a.Workloads {
		ready := "—"
		if w.Replicated {
			ready = itoa(int(w.Ready)) + "/" + itoa(int(w.Desired))
		}
		row := detailRow{
			Cells: []string{w.Kind, w.Name, ready, formatAge(w.CreatedAt, now)},
			Ref:   objectRef{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace},
		}
		switch {
		case w.Replicated && w.Desired > 0 && w.Ready == 0:
			row.Status = theme.StatusCritical
		case w.Replicated && w.Ready < w.Desired:
			row.Status = theme.StatusWarning
		}
		workloads.Rows = append(workloads.Rows, row)
	}

	pods := detailSection{
		Title:   "Pods",
		Columns: []string{"Name", "Phase", "Ready", "Restarts", "Node", "State"},
		Empty:   "no pods",
	}
	for i := range a.Pods {
		p := &a.Pods[i]
		row := detailRow{
			Cells: []string{
				p.Name, p.Phase, readyLabel(p.Ready), itoa(int(p.Restarts)), orNone(p.Node), orNone(p.Reason),
			},
			Ref: objectRef{Kind: "Pod", Name: p.Name, Namespace: p.Namespace},
		}
		switch {
		case p.Reason != "" && !p.Terminal():
			row.Status = theme.StatusCritical
		case !p.Ready && !p.Terminal():
			row.Status = theme.StatusWarning
		}
		pods.Rows = append(pods.Rows, row)
	}

	delivery := detailSection{
		Title:   "Delivered by",
		Columns: []string{"Tool", "Object", "Namespace"},
		Empty:   "nothing claims this application: no Helm release, Flux object or Argo CD application",
	}
	if a.Manager.Known() {
		row := detailRow{
			Cells: []string{a.Manager.Tool, deliveryObject(a.Manager), orNone(a.Manager.Namespace)},
		}
		// The Flux and Argo objects are real resources: opening one shows its
		// reconciliation conditions like any other object.
		if a.Manager.Kind != "" {
			row.Ref = objectRef{Kind: a.Manager.Kind, Name: a.Manager.Name, Namespace: a.Manager.Namespace}
		}
		delivery.Rows = append(delivery.Rows, row)
	}

	network := detailSection{
		Title:   "Network",
		Columns: []string{"Kind", "Name", "Detail"},
		Empty:   "no service or ingress belongs to this application",
	}
	for i := range a.Services {
		s := &a.Services[i]
		detail := s.Type
		if len(s.Ports) > 0 {
			detail += "  " + strings.Join(s.Ports, ",")
		}
		network.Rows = append(network.Rows, detailRow{
			Cells: []string{"Service", s.Name, detail},
			Ref:   objectRef{Kind: "Service", Name: s.Name, Namespace: s.Namespace},
		})
	}
	for _, i := range a.Ingresses {
		network.Rows = append(network.Rows, detailRow{
			Cells: []string{"Ingress", i.Name, strings.Join(i.Hosts, ", ")},
			Ref:   objectRef{Kind: "Ingress", Name: i.Name, Namespace: i.Namespace},
		})
	}

	sectionList := []detailSection{workloads, pods, network, delivery}
	if m.groupingShown {
		sectionList = append(sectionList, m.groupingSection(&a))
	}
	sectionList = append(sectionList, m.eventsSection(&a, now))

	sections, targets := numberTargets(sectionList)
	d.Sections = sections
	return d, targets
}

// groupingSection lists every object in the application next to the reason
// the grouper attached it, so "why is this part of payments?" always has a
// real, specific answer (ADR 16).
//
// An owner reference is the only relationship Kubernetes itself guarantees;
// everything else — a label, a selector match, an ingress backend, or nothing
// at all — is a convention Correlux is reading, and is marked a guess rather
// than dressed up as a fact.
func (m *Model) groupingSection(a *application.Application) detailSection {
	section := detailSection{
		Title:   "Grouped by",
		Columns: []string{"Object", "Certainty", "Reason"},
		Empty:   "nothing belongs to this application yet",
	}
	add := func(kind, name, namespace string, reason application.Reason) {
		row := detailRow{
			Cells: []string{kind + "/" + name, m.certaintyCell(reason), reason.Describe()},
			Ref:   objectRef{Kind: kind, Name: name, Namespace: namespace},
		}
		if !reason.Certain() {
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	for _, w := range a.Workloads {
		add(w.Kind, w.Name, w.Namespace, w.GroupedBy)
	}
	for i := range a.Pods {
		p := &a.Pods[i]
		add("Pod", p.Name, p.Namespace, p.GroupedBy)
	}
	for i := range a.Services {
		s := &a.Services[i]
		add("Service", s.Name, s.Namespace, s.GroupedBy)
	}
	for _, ing := range a.Ingresses {
		add("Ingress", ing.Name, ing.Namespace, ing.GroupedBy)
	}
	return section
}

// certaintyCell renders a reason's certainty as a glyph and a word together,
// never colour alone (SPEC 26): an owner reference earns "certain", anything
// else — a label, a selector match, a backend reference, or nothing at all —
// earns "guess".
func (m *Model) certaintyCell(r application.Reason) string {
	status := theme.StatusHealthy
	word := "certain"
	if !r.Certain() {
		status, word = theme.StatusWarning, "guess"
	}
	return m.theme.Glyph(status) + " " + word
}

// eventsSection shows what the cluster said about this application, which is
// the difference between "the pod is not ready" and "the probe is refused".
func (m *Model) eventsSection(a *application.Application, now time.Time) detailSection {
	section := detailSection{
		Title:   "Recent events",
		Columns: []string{"Age", "Type", "Object", "Reason", "Message"},
	}
	switch m.evidence.State() {
	case async.Idle:
		section.Empty = "not read yet"
		return section
	case async.Loading:
		section.Empty = "loading…"
		return section
	case async.Failed:
		section.Empty = "unavailable — " + shortError(m.evidence.Err())
		return section
	}

	evidence := m.evidence.Get()
	events := make([]application.Event, 0, maxDetailEvents)
	for i := range a.Pods {
		events = append(events, evidence.EventsAbout(a.Pods[i].UID, a.Pods[i].Name)...)
	}
	for i := range a.Workloads {
		events = append(events, evidence.EventsAbout(a.Workloads[i].UID, a.Workloads[i].Name)...)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].LastSeen.After(events[j].LastSeen) })
	if len(events) > maxDetailEvents {
		events = events[:maxDetailEvents]
	}
	if len(events) == 0 {
		section.Empty = "none in this namespace"
		return section
	}

	for i := range events {
		e := &events[i]
		// No ref: an event is something to read, not somewhere to go.
		row := detailRow{
			Cells: []string{
				formatAge(e.LastSeen, now), e.Type, e.About.Kind + "/" + e.About.Name, e.Reason, e.Message,
			},
		}
		if e.Type == "Warning" {
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	return section
}

// maxDetailEvents bounds the events on the detail screen. The newest handful
// explain the current state; older ones are history.
const maxDetailEvents = 8

// deliveryObject names what to look at: the Flux or Argo object when there is
// one, the Helm release otherwise.
func deliveryObject(m application.Manager) string {
	if m.Kind != "" {
		return m.Kind + "/" + m.Name
	}
	if m.Name == "" {
		return "—"
	}
	return "release " + m.Name
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
