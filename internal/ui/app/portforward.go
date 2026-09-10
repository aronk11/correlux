package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const paletteForward palette.ActionID = "forward.action"

type forwardSession struct {
	cluster, namespace, pod, ports, label string
	cancel                                context.CancelFunc
}
type forwardStartedMsg struct {
	id     int
	events <-chan kubeclient.ForwardEvent
	err    error
}
type forwardEventMsg struct {
	id     int
	events <-chan kubeclient.ForwardEvent
	event  kubeclient.ForwardEvent
}

func (m *Model) forwardCommands() []palette.Command {
	var out []palette.Command
	if m.view != viewFleet && m.view != viewFleetResource {
		if ref, ok := m.execRef(); ok && ref.Kind == "Pod" {
			out = append(out, palette.Command{ID: "forward.start", Action: paletteForward, Arg: "start", Title: "Port-forward from this pod", Subtitle: "127.0.0.1 only", Category: "Troubleshoot", Enabled: true})
		}
	}
	ids := make([]int, 0, len(m.forwards))
	for id := range m.forwards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		session := m.forwards[id]
		out = append(out, palette.Command{ID: fmt.Sprintf("forward.stop.%d", id), Action: paletteForward, Arg: strconv.Itoa(id), Title: "Stop port-forward " + session.label, Subtitle: session.cluster + " / " + session.namespace + " / " + session.pod, Category: "Troubleshoot", Enabled: true})
	}
	return out
}

func parseForward(value string) (string, error) {
	local, remote, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return "", errors.New("enter local:remote ports, e.g. 8080:80 or 0:80")
	}
	l, err := strconv.Atoi(local)
	if err != nil || l < 0 || l > 65535 {
		return "", errors.New("local port must be between 0 and 65535")
	}
	r, err := strconv.Atoi(remote)
	if err != nil || r < 1 || r > 65535 {
		return "", errors.New("remote port must be between 1 and 65535")
	}
	return strconv.Itoa(l) + ":" + strconv.Itoa(r), nil
}

func (m *Model) openForward(action string) tea.Cmd {
	if action != "start" {
		id, err := strconv.Atoi(action)
		if err == nil {
			if s := m.forwards[id]; s != nil {
				s.cancel()
				delete(m.forwards, id)
				m.rebuildCommands()
			}
		}
		return nil
	}
	ref, ok := m.execRef()
	if !ok || ref.Kind != "Pod" {
		return nil
	}
	m.promptTitle = "Port-forward to " + ref.Name
	m.promptNote = "Enter local:remote ports. Local 0 chooses an available port. Binding is restricted to 127.0.0.1."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue("0:8080")
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		ports, err := parseForward(value)
		if err != nil {
			m.promptError = err.Error()
			return nil
		}
		m.cancelPrompt()
		return m.startForward(ref, ports)
	}
	return nil
}

func (m *Model) startForward(ref objectRef, ports string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	if m.forwards == nil {
		m.forwards = map[int]*forwardSession{}
	}
	m.forwardSequence++
	id := m.forwardSequence
	cluster, factory := m.contextName, m.factory
	m.forwards[id] = &forwardSession{cluster: cluster, namespace: ref.Namespace, pod: ref.Name, ports: ports, label: ports + " (starting)", cancel: cancel}
	m.rebuildCommands()
	return func() tea.Msg {
		events, err := factory.ForwardPod(ctx, cluster, ref.Namespace, ref.Name, []string{ports})
		return forwardStartedMsg{id: id, events: events, err: err}
	}
}

func waitForward(id int, events <-chan kubeclient.ForwardEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			event.Done = true
		}
		return forwardEventMsg{id: id, events: events, event: event}
	}
}

func (m *Model) applyForwardStarted(msg forwardStartedMsg) tea.Cmd {
	if m.forwards[msg.id] == nil {
		return nil
	}
	if msg.err != nil {
		return m.applyForwardEvent(forwardEventMsg{id: msg.id, event: kubeclient.ForwardEvent{Done: true, Err: msg.err}})
	}
	return waitForward(msg.id, msg.events)
}

func (m *Model) applyForwardEvent(msg forwardEventMsg) tea.Cmd {
	session := m.forwards[msg.id]
	if session == nil {
		return nil
	}
	if msg.event.Done {
		session.cancel()
		delete(m.forwards, msg.id)
		m.rebuildCommands()
		if msg.event.Err != nil {
			m.notice("Port-forward ended: "+shortError(msg.event.Err), theme.StatusWarning)
			return m.expireNotice()
		}
		return nil
	}
	if len(msg.event.Ports) > 0 {
		p := msg.event.Ports[0]
		session.label = fmt.Sprintf("127.0.0.1:%d → %d", p.Local, p.Remote)
		m.notice("Forwarding "+session.label+" to "+session.cluster+" / "+session.pod, theme.StatusHealthy)
		m.rebuildCommands()
	}
	return tea.Batch(waitForward(msg.id, msg.events), m.expireNotice())
}

// Close stops local connections when the terminal UI exits, including interrupts.
func (m *Model) Close() {
	for _, session := range m.forwards {
		session.cancel()
	}
}
