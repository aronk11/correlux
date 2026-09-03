package app

import (
	"context"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/aronk11/correlux/internal/kube/podexec"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// openExec opens an interactive shell in whatever the current screen points
// at: the open object, the row under the cursor, or a running pod of the
// selected workload.
//
// A production context asks first, the same confirmation gate every change
// goes through (ADR 20): the shell is not a change to a Kubernetes object, but
// a command typed into a production container can do anything a change could,
// so the one fact that must be on screen before it opens is which cluster it
// is (SPEC 16).
func (m *Model) openExec() tea.Cmd {
	target, title, ok := m.execTarget()
	if !ok {
		m.notice("Select a pod or a workload to open a shell in", theme.StatusWarning)
		return m.expireNotice()
	}

	if challenge := m.productionChallenge(); challenge != "" {
		return m.confirm(pendingAction{
			Title: title,
			Lines: []string{
				"This opens an interactive shell inside the container.",
				target.Label() + " in " + orNone(target.Namespace),
			},
			Challenge: challenge,
			Run:       func(m *Model) tea.Cmd { return m.startExec(target) },
		})
	}
	return m.startExec(target)
}

// execTarget resolves what "open a shell" means on the current screen: the
// object in hand if it is a Pod, or the first running pod of the workload it
// is otherwise. The container is left to the server, exactly as logSources
// leaves it for reading logs — Correlux does not guess which of a pod's
// containers a shell means, and says so when the guess would be ambiguous.
func (m *Model) execTarget() (podexec.Target, string, bool) {
	ref, ok := m.execRef()
	if !ok {
		return podexec.Target{}, "", false
	}
	if ref.Kind == "Pod" {
		return podexec.Target{Namespace: ref.Namespace, Pod: ref.Name}, "Shell in " + ref.label(), true
	}

	pods := m.podsOwnedBy(ref)
	for i := range pods {
		if pods[i].Terminal() {
			continue
		}
		target := podexec.Target{Namespace: pods[i].Namespace, Pod: pods[i].Name}
		return target, "Shell in " + ref.label() + " (" + pods[i].Name + ")", true
	}
	return podexec.Target{}, "", false
}

// execRef names the object the current screen points at, the same way
// scalableTarget and logSources each read their own screen.
func (m *Model) execRef() (objectRef, bool) {
	switch m.view {
	case viewObject:
		if m.objectTarget.empty() {
			return objectRef{}, false
		}
		return m.objectTarget, true
	case viewApplication:
		_, targets := m.applicationView()
		if m.detailPort.Cursor >= 0 && m.detailPort.Cursor < len(targets) {
			return targets[m.detailPort.Cursor], true
		}
	case viewTable:
		rows := m.visibleRows()
		if m.tablePort.Cursor < 0 || m.tablePort.Cursor >= len(rows) {
			return objectRef{}, false
		}
		row := rows[m.tablePort.Cursor]
		return objectRef{
			Kind:      m.resource.Kind(),
			Name:      row.Name,
			Namespace: rowNamespace(row.Namespace, m.resource.Namespaced, m.namespace, m.allNamespaces),
			Resource:  m.resource.FullName(),
		}, true
	}
	return objectRef{}, false
}

// startExec hands the terminal to a shell running in target, and takes it
// back once the shell exits.
func (m *Model) startExec(target podexec.Target) tea.Cmd {
	session := &execSession{model: m, target: target}
	return tea.Exec(session, func(err error) tea.Msg { return execEndedMsg{target: target, err: err} })
}

// execEndedMsg reports that the terminal is Correlux's again.
type execEndedMsg struct {
	target podexec.Target
	err    error
}

// applyExecEnded reports how the shell ended. A session the user exited on
// purpose is not a failure and says nothing; one the connection dropped on is.
func (m *Model) applyExecEnded(msg execEndedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Shell in "+msg.target.Label()+" ended: "+shortError(msg.err), theme.StatusWarning)
		return m.expireNotice()
	}
	return nil
}

// execSession implements tea.ExecCommand: Bubble Tea releases the terminal,
// calls Run, and takes it back when Run returns. Everything about *when* a
// shell is allowed to open lives in openExec, above; this is only the plumbing
// that connects the terminal Bubble Tea was drawing to the one the container
// now owns.
type execSession struct {
	model  *Model
	target podexec.Target

	stdin          io.Reader
	stdout, stderr io.Writer
}

func (s *execSession) SetStdin(r io.Reader)  { s.stdin = r }
func (s *execSession) SetStdout(w io.Writer) { s.stdout = w }
func (s *execSession) SetStderr(w io.Writer) { s.stderr = w }

// Run streams the shell. It blocks until the container exits, the connection
// breaks, or the terminal is not one Correlux can put in raw mode — a pipe in
// a test, say — in which case it runs without one rather than refusing.
func (s *execSession) Run() error {
	m := s.model
	restCfg, err := m.factory.RESTConfigForExec(m.contextName)
	if err != nil {
		return err
	}

	fd := os.Stdin.Fd()
	var sizeQueue remotecommand.TerminalSizeQueue
	if term.IsTerminal(fd) {
		state, rawErr := term.MakeRaw(fd)
		if rawErr == nil {
			defer func() { _ = term.Restore(fd, state) }()
		}
		queue := newResizeQueue(fd)
		defer queue.stop()
		sizeQueue = queue
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return podexec.Stream(ctx, restCfg, s.target, podexec.DefaultShellCommand,
		s.stdin, s.stdout, s.stderr, sizeQueue)
}
