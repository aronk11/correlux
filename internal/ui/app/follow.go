package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/kube/logs"
	"github.com/aronk11/correlux/internal/ui/async"
)

// followDelay is how long the selection has to rest before companions catch
// up with it. Holding the down key through forty pods must not open forty log
// streams against somebody's API server; a pause shorter than a glance is
// enough to tell walking past a row from stopping on it.
const followDelay = 150 * time.Millisecond

// followDueMsg says the selection has rested for long enough.
type followDueMsg struct{ seq uint64 }

// id names a companion's content in the picker, the palette and a saved
// layout.
func (k followKind) id() string {
	switch k {
	case followLogs:
		return "logs"
	case followWhy:
		return "why"
	case followDetails:
		return "details"
	case followYAML:
		return "yaml"
	case followEvents:
		return "events"
	}
	return ""
}

// label is what the pane's title calls it.
func (k followKind) label() string {
	switch k {
	case followLogs:
		return "Logs"
	case followWhy:
		return "Why"
	case followDetails:
		return "Details"
	case followYAML:
		return "YAML"
	case followEvents:
		return "Events"
	}
	return ""
}

// followKindOf is the reverse of id.
func followKindOf(id string) (followKind, bool) {
	for _, k := range []followKind{followLogs, followWhy, followDetails, followYAML, followEvents} {
		if k.id() == id {
			return k, true
		}
	}
	return followNone, false
}

// subject is what a pane has selected, in the terms a companion needs: the
// application, the object, and the containers behind either.
type subject struct {
	// key changes exactly when the selection does.
	key   string
	label string
	// app is the application's key, when the selection is one or is inside
	// one.
	app       string
	unhealthy bool
	ref       objectRef
	hasRef    bool
	sources   []logs.Source
	logTitle  string
	hasLogs   bool
	// held is set on a screen that selects nothing — usage, the fleet, a log —
	// where companions keep what they have rather than going blank because
	// the pane beside them went somewhere else for a moment.
	held bool
}

// subjectOf reads what p has selected.
func (m *Model) subjectOf(p *pane) subject {
	var s subject
	m.withPane(p, func() { s = m.currentSubject() })
	return s
}

// subjectKey is the driver's selection, reduced to something to compare.
func (m *Model) subjectKey() string {
	s := m.subjectOf(m.driver())
	if s.held {
		return m.followKey
	}
	return s.key
}

// currentSubject reads the active pane's selection.
func (m *Model) currentSubject() subject {
	var s subject
	switch m.view {
	case viewApplications:
		apps := m.visibleApplications()
		if m.appPort.Cursor < 0 || m.appPort.Cursor >= len(apps) {
			return s
		}
		s.setApp(m, &apps[m.appPort.Cursor])
	case viewApplication, viewWhy:
		app, ok := m.currentApplication()
		if !ok {
			return s
		}
		s.setApp(m, &app)
		if m.view == viewApplication {
			if _, targets := m.applicationView(); m.detailPort.Cursor >= 0 && m.detailPort.Cursor < len(targets) {
				s.setRef(m, targets[m.detailPort.Cursor])
			}
		}
	case viewTable:
		if ref, ok := m.selectedRowRef(); ok {
			s.setRef(m, ref)
		}
	case viewObject:
		if app, ok := m.currentApplication(); ok {
			s.setApp(m, &app)
		}
		if !m.objectTarget.empty() {
			s.setRef(m, m.objectTarget)
		}
	default:
		s.held = true
	}
	return s
}

func (s *subject) setApp(m *Model, a *application.Application) {
	s.app = a.Key()
	s.key = "app/" + a.Key()
	s.label = a.Name
	s.unhealthy = diagnosedHealth(a.Health, m.findingsFor(a.Key())) != application.Healthy
	s.sources, s.logTitle, s.hasLogs = podSources(a.Pods), "Logs of "+a.Name, len(a.Pods) > 0
}

func (s *subject) setRef(m *Model, ref objectRef) {
	s.ref, s.hasRef = ref, true
	s.key += "|" + ref.Kind + "/" + ref.Namespace + "/" + ref.Name
	s.label = ref.label()
	if sources, title, ok := m.sourcesFor(ref); ok {
		s.sources, s.logTitle, s.hasLogs = sources, title, true
	} else if s.app == "" {
		s.sources, s.logTitle, s.hasLogs = nil, "", false
	}
}

// following reports whether any pane is waiting on the selection.
func (m *Model) following() bool {
	for _, p := range m.panes {
		if p.follow != followNone && !p.pinned {
			return true
		}
	}
	return false
}

// movesSelection reports the messages after which the selection may be
// somewhere else. A log batch is not one of them, and a busy container sends
// hundreds a second.
func movesSelection(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseWheelMsg,
		applicationsLoadedMsg, tableLoadedMsg, objectLoadedMsg:
		return true
	case paneMsg:
		return movesSelection(msg.msg)
	}
	return false
}

// noteSelection schedules the companions to catch up when the selection has
// moved.
func (m *Model) noteSelection() tea.Cmd {
	if !m.following() {
		return nil
	}
	key := m.subjectKey()
	if key == m.followKey {
		return nil
	}
	m.followKey = key
	m.followSeq++
	seq := m.followSeq
	return tea.Tick(followDelay, func(time.Time) tea.Msg { return followDueMsg{seq: seq} })
}

// syncFollowers points every companion that is not pinned at the selection.
func (m *Model) syncFollowers() tea.Cmd {
	s := m.subjectOf(m.driver())
	if s.held {
		return nil
	}
	var cmds []tea.Cmd
	for _, p := range m.panes {
		if p.follow != followNone && !p.pinned {
			cmds = append(cmds, m.syncPane(p, &s))
		}
	}
	return tea.Batch(cmds...)
}

// syncPane shows s in companion p. A selection the companion has nothing to
// say about leaves a sentence in its place: an empty pane reads as one that
// is still loading.
func (m *Model) syncPane(p *pane, s *subject) tea.Cmd {
	if p.follow == followNone || (p.subject == s.key && p.subject != "" && p.follow != followEvents) {
		return nil
	}
	return m.inPane(p, func() tea.Cmd {
		m.subject, m.subjectLabel, m.blank = s.key, s.label, ""
		switch m.follow {
		case followLogs:
			return m.followLogs(s)
		case followWhy:
			return m.followWhy(s)
		case followDetails:
			return m.followObject(s, false)
		case followYAML:
			return m.followObject(s, true)
		case followEvents:
			return m.followActivity()
		}
		return nil
	})
}

func (m *Model) followLogs(s *subject) tea.Cmd {
	m.stopLogs()
	m.view, m.logFrom = viewLogs, viewLogs
	if !s.hasLogs {
		m.blank = "Nothing selected to read logs from."
		if s.label != "" {
			m.blank = s.label + " has no containers to read logs from."
		}
		return nil
	}
	m.logTitle, m.logTargets = s.logTitle, s.sources
	m.logLines, m.logDropped, m.logErr = nil, 0, nil
	m.logPort.Offset = 0
	m.logFollow, m.logClosed = true, false
	return m.startLogs()
}

func (m *Model) followWhy(s *subject) tea.Cmd {
	m.view, m.whyFrom = viewWhy, viewWhy
	if s.app == "" {
		m.blank = "A diagnosis is about an application. Select one, or an object inside one."
		return nil
	}
	if m.whySubject != s.app {
		m.whyPort.Offset = 0
	}
	m.whySubject, m.selectedApp = s.app, s.app
	if m.evidence.State() == async.Idle {
		return m.loadEvidence()
	}
	return nil
}

func (m *Model) followObject(s *subject, yaml bool) tea.Cmd {
	m.objectTrail = nil
	m.objectFrom = viewObject
	if !s.hasRef {
		if s.app == "" || yaml {
			m.view = viewObject
			m.blank = "Select an object to read it here."
			return nil
		}
		// An application has no document of its own; what it is made of is
		// the application view.
		if m.selectedApp != s.app {
			m.detailPort.Offset, m.detailPort.Cursor = 0, 0
		}
		m.selectedApp, m.view = s.app, viewApplication
		if m.evidence.State() == async.Idle {
			return m.loadEvidence()
		}
		return nil
	}
	m.selectedApp = s.app
	cmd := m.showObject(s.ref)
	m.objectYAML = yaml
	return cmd
}

func (m *Model) followActivity() tea.Cmd {
	m.view, m.activityFrom = viewActivity, viewActivity
	if m.evidence.State() == async.Idle {
		return m.loadEvidence()
	}
	return nil
}

// atFollowRoot reports that the active pane is a companion showing exactly
// what it was opened for, which is where "back" has nowhere to go.
func (m *Model) atFollowRoot() bool {
	switch m.follow {
	case followLogs:
		return m.view == viewLogs
	case followWhy:
		return m.view == viewWhy
	case followDetails, followYAML:
		return (m.view == viewObject && len(m.objectTrail) == 0) || (m.view == viewApplication && m.follow == followDetails)
	case followEvents:
		return m.view == viewActivity
	}
	return false
}
