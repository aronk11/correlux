package app

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/logs"
	"github.com/aronk11/kubeui/internal/ui/screens"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// maxLogLines is how much output kubeui keeps. A container that writes a
// thousand lines a second would otherwise turn a log view into a memory leak;
// the oldest lines are dropped, and the view says so.
const maxLogLines = 5000

// logBatchMsg carries what arrived since the last read.
type logBatchMsg struct {
	gen    uint64
	events []logs.Event
	// closed is true when the stream ended: the log was fully read, or the
	// container is gone.
	closed bool
}

// logsStartedMsg reports that a stream is open, or why it is not.
type logsStartedMsg struct {
	gen    uint64
	stream <-chan logs.Event
	err    error
}

// openLogs starts reading the logs of whatever the current screen points at.
func (m *Model) openLogs() tea.Cmd {
	sources, title, ok := m.logSources()
	if !ok {
		m.notice("Select a pod, a workload or an application to read logs from", theme.StatusWarning)
		return m.expireNotice()
	}

	m.logTitle = title
	m.logTargets = sources
	m.logLines = nil
	m.logDropped = 0
	m.logPort.Offset = 0
	m.logFollow = true
	m.logClosed = false
	m.logErr = nil
	m.view = viewLogs
	m.rebuildCommands()
	return m.startLogs()
}

// startLogs opens the stream, cancelling whatever was open before.
func (m *Model) startLogs() tea.Cmd {
	if m.cancelLogs != nil {
		m.cancelLogs()
	}
	gen := m.logGeneration + 1
	m.logGeneration = gen
	m.logClosed = false
	m.logErr = nil

	// No timeout: a followed log is meant to stay open. It ends when the user
	// leaves, which cancels this context.
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelLogs = cancel

	factory := m.factory
	name := m.contextName
	sources := m.logTargets
	opts := logs.Options{
		Follow:     m.logFollow,
		Timestamps: m.logTimestamps,
		Previous:   m.logPrevious,
		Tail:       logs.DefaultTail,
	}

	return func() tea.Msg {
		stream, err := factory.Tail(ctx, name, sources, opts)
		return logsStartedMsg{gen: gen, stream: stream, err: err}
	}
}

// waitForLogs blocks until something arrives, then drains whatever else is
// already waiting.
//
// Draining matters: a busy container delivers thousands of lines a second, and
// one message per line would spend the whole frame budget in the update loop.
func waitForLogs(gen uint64, stream <-chan logs.Event) tea.Cmd {
	return func() tea.Msg {
		first, open := <-stream
		if !open {
			return logBatchMsg{gen: gen, closed: true}
		}

		batch := []logs.Event{first}
		for len(batch) < logBatchSize {
			select {
			case event, stillOpen := <-stream:
				if !stillOpen {
					return logBatchMsg{gen: gen, events: batch, closed: true}
				}
				batch = append(batch, event)
			default:
				return logBatchMsg{gen: gen, events: batch}
			}
		}
		return logBatchMsg{gen: gen, events: batch}
	}
}

// logBatchSize bounds one trip through the update loop.
const logBatchSize = 512

// applyLogBatch stores what arrived and asks for the next batch.
func (m *Model) applyLogBatch(msg logBatchMsg) tea.Cmd {
	if msg.gen != m.logGeneration {
		// A stream the user has already left.
		return nil
	}

	for _, event := range msg.events {
		if event.Err != nil {
			m.logLines = append(m.logLines, logs.Line{
				Source: event.Source,
				Text:   "[" + event.Source.Label() + ": " + shortError(event.Err) + "]",
			})
			m.logFailed = append(m.logFailed, event.Source.Label())
			continue
		}
		m.logLines = append(m.logLines, event.Line)
	}

	if extra := len(m.logLines) - maxLogLines; extra > 0 {
		m.logLines = append([]logs.Line(nil), m.logLines[extra:]...)
		m.logDropped += extra
	}

	if msg.closed {
		m.logClosed = true
		return nil
	}
	return waitForLogs(msg.gen, m.logStream)
}

// logSources decides what to read from: the open pod, every pod of a workload,
// or every pod of an application.
func (m *Model) logSources() ([]logs.Source, string, bool) {
	switch m.view {
	case viewObject:
		return m.sourcesFor(m.objectTarget)
	case viewApplication, viewWhy:
		app, ok := m.currentApplication()
		if !ok {
			return nil, "", false
		}
		if m.view == viewApplication {
			if _, targets := m.applicationView(); m.detailPort.Cursor >= 0 && m.detailPort.Cursor < len(targets) {
				if sources, title, found := m.sourcesFor(targets[m.detailPort.Cursor]); found {
					return sources, title, true
				}
			}
		}
		return podSources(app.Pods), "Logs of " + app.Name, len(app.Pods) > 0
	case viewTable:
		rows := m.tableRows()
		if m.tablePort.Cursor < 0 || m.tablePort.Cursor >= len(rows) {
			return nil, "", false
		}
		return m.sourcesFor(objectRef{
			Kind: m.resource.Kind(), Name: rows[m.tablePort.Cursor].Name,
			Namespace: rowNamespace(rows[m.tablePort.Cursor].Namespace, m.resource.Namespaced, m.namespace, m.allNamespaces),
			Resource:  m.resource.FullName(),
		})
	}
	return nil, "", false
}

// sourcesFor turns one object into the containers to read.
func (m *Model) sourcesFor(ref objectRef) ([]logs.Source, string, bool) {
	if ref.empty() {
		return nil, "", false
	}
	if ref.Kind == "Pod" {
		return []logs.Source{{Namespace: ref.Namespace, Pod: ref.Name}}, "Logs of " + ref.label(), true
	}

	// A workload has no logs of its own; its pods do. Following them by owner
	// is what makes "the logs of this Deployment" mean anything (SPEC 15).
	pods := m.podsOwnedBy(ref)
	if len(pods) == 0 {
		return nil, "", false
	}
	return podSources(pods), "Logs of " + ref.label(), true
}

// podsOwnedBy finds the pods of a workload in the loaded snapshot, through the
// same ownership the dashboard is built from.
func (m *Model) podsOwnedBy(ref objectRef) []application.Pod {
	snapshot := m.apps.Get().Snapshot
	workload, ok := m.workloadFor(ref)
	if !ok {
		return nil
	}

	var out []application.Pod
	for i := range snapshot.Pods {
		pod := snapshot.Pods[i]
		if pod.Namespace != ref.Namespace || pod.Terminal() {
			continue
		}
		if len(workload.Selector) > 0 && matchesLabels(workload.Selector, pod.Labels) {
			out = append(out, pod)
		}
	}
	return out
}

func matchesLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func podSources(pods []application.Pod) []logs.Source {
	out := make([]logs.Source, 0, len(pods))
	for i := range pods {
		if pods[i].Terminal() {
			continue
		}
		out = append(out, logs.Source{Namespace: pods[i].Namespace, Pod: pods[i].Name})
	}
	return out
}

// rowNamespace fills in the namespace a table row did not need to repeat.
func rowNamespace(rowNS string, namespaced bool, scope string, allNamespaces bool) string {
	if rowNS != "" || !namespaced || allNamespaces {
		return rowNS
	}
	return scope
}

// stopLogs ends the stream without changing the view. Leaving the scope a log
// belongs to must not leave its connections open.
func (m *Model) stopLogs() {
	if m.cancelLogs != nil {
		m.cancelLogs()
		m.cancelLogs = nil
	}
	m.logGeneration++
	m.logStream = nil
	m.logLines = nil
	m.logTargets = nil
	m.logFailed = nil
	m.logDropped = 0
	m.logErr = nil
}

// closeLogs stops the stream and leaves the view.
func (m *Model) closeLogs() tea.Cmd {
	if m.cancelLogs != nil {
		m.cancelLogs()
		m.cancelLogs = nil
	}
	m.logGeneration++
	m.logStream = nil
	m.view = viewObject
	if m.objectTarget.empty() {
		m.view = viewApplications
	}
	m.rebuildCommands()
	return nil
}

// toggleFollow stops or resumes reading new output. Stopping matters: you
// cannot read something that keeps moving.
func (m *Model) toggleFollow() tea.Cmd {
	m.logFollow = !m.logFollow
	m.rebuildCommands()
	if m.logFollow {
		return m.startLogs()
	}
	// Pausing does not need a new request; the stream simply stops being read
	// to the bottom. Cancel it so nothing is buffered for a view nobody scrolls
	// back to.
	if m.cancelLogs != nil {
		m.cancelLogs()
		m.cancelLogs = nil
	}
	m.logGeneration++
	m.logClosed = true
	return nil
}

// toggleLogTimestamps and togglePrevious both change what is asked for, so both
// reopen the stream.
func (m *Model) toggleLogTimestamps() tea.Cmd {
	m.logTimestamps = !m.logTimestamps
	m.logLines = nil
	m.rebuildCommands()
	return m.startLogs()
}

func (m *Model) togglePrevious() tea.Cmd {
	m.logPrevious = !m.logPrevious
	m.logLines = nil
	m.logFollow = m.logFollow && !m.logPrevious // a previous run writes nothing more
	m.rebuildCommands()
	return m.startLogs()
}

func (m *Model) toggleWrap() tea.Cmd {
	m.logWrap = !m.logWrap
	m.rebuildCommands()
	return nil
}

// logsData assembles the log view.
func (m *Model) logsData() screens.LogsData {
	d := screens.LogsData{
		Title:      m.logTitle,
		Follow:     m.logFollow && !m.logClosed,
		Wrap:       m.logWrap,
		Offset:     m.logPort.Offset,
		ShowSource: len(m.logTargets) > 1,
	}
	if m.logErr != nil {
		d.Message = "Could not read logs: " + shortError(m.logErr)
		d.MessageStatus = theme.StatusCritical
		return d
	}

	d.Subtitle = m.logSubtitle()
	d.Lines = make([]screens.LogLine, 0, len(m.logLines))
	for _, line := range m.logLines {
		rendered := screens.LogLine{Source: line.Source.Label(), Text: line.Text}
		if !line.At.IsZero() {
			rendered.Time = line.At.Local().Format("15:04:05.000")
		}
		if strings.HasPrefix(line.Text, "[") && line.Source.Pod != "" && strings.Contains(line.Text, ": ") {
			// A line kubeui wrote about a source it could not read.
			rendered.Status = theme.StatusWarning
		}
		d.Lines = append(d.Lines, rendered)
	}
	return d
}

// logSubtitle states what is being read and what is missing from it.
func (m *Model) logSubtitle() string {
	parts := []string{itoa(len(m.logTargets)) + " " + containerWord(len(m.logTargets))}
	switch {
	case m.logPrevious:
		parts = append(parts, "previous run")
	case m.logFollow && !m.logClosed:
		parts = append(parts, "following")
	case m.logClosed:
		parts = append(parts, "paused")
	}
	if m.logTimestamps {
		parts = append(parts, "timestamps")
	}
	parts = append(parts, itoa(len(m.logLines))+" lines")
	if m.logDropped > 0 {
		parts = append(parts, itoa(m.logDropped)+" older lines dropped")
	}
	if len(m.logFailed) > 0 {
		parts = append(parts, "unreadable: "+strings.Join(unique(m.logFailed), ", "))
	}
	return strings.Join(parts, "   ")
}

func containerWord(n int) string {
	if n == 1 {
		return "container"
	}
	return "containers"
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// scrollLogs moves the viewport, and stops following: scrolling back is a
// statement that the user wants to read what is already there.
func (m *Model) scrollLogs(delta int) {
	total := m.logsData().LineCount(m.screen.Body.Width)
	height := max(m.screen.Body.Height-2, 1) // the title and the state line
	if delta < 0 {
		m.logFollow = false
	}
	if m.logFollow {
		// Following pins the view to the end; nothing else decides the offset.
		m.logPort.Offset = max(total-height, 0)
		return
	}
	m.logPort.ScrollLines(delta, total, height)
}
