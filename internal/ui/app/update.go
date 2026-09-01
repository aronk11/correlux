package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	"github.com/aronk11/kubeui/internal/ui/layout"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// noticeTTL is how long a transient status message stays on screen.
const noticeTTL = 5 * time.Second

// Update is the single place where state changes. It must never block: any
// work that can take time is returned as a command.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleResize(msg)

	case resizeSettledMsg:
		run, retry := m.resize.Ready(time.Now())
		if !run {
			if retry > 0 {
				return m, scheduleResize(retry)
			}
			return m, nil
		}
		m.applyLayout()
		return m, nil

	case tea.BackgroundColorMsg:
		// The terminal told us its real background; rebuild the theme so the
		// palette matches instead of guessing from environment variables.
		dark := msg.IsDark()
		if dark != m.caps.Dark {
			m.caps.Dark = dark
			m.theme = theme.New(m.caps, m.cfg.Theme)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.MouseWheelMsg:
		m.handleWheel(msg)
		return m, nil

	case tea.MouseClickMsg:
		return m, m.handleClick(msg)

	case clusterProbedMsg:
		if m.cluster.Succeed(msg.gen, msg.info) && msg.info.State != kubeclient.ConnOK {
			m.notice(connectionNotice(msg.info), theme.StatusCritical)
			return m, m.expireNotice()
		}
		return m, nil

	case namespacesLoadedMsg:
		if msg.err != nil {
			m.namespaces.Fail(msg.gen, msg.err)
		} else if m.namespaces.Succeed(msg.gen, msg.list) {
			m.rebuildCommands()
		}
		m.nsPicker.Refresh()
		return m, nil

	case kubeconfigReloadedMsg:
		return m, m.applyReloadedKubeconfig(msg)

	case messageExpiredMsg:
		if msg.seq == m.messageSeq {
			m.message = ""
			m.messageStatus = theme.StatusUnknown
		}
		return m, nil
	}
	return m, nil
}

// handleResize coalesces resize bursts: dragging a window emits dozens of
// events per second and re-laying out on each one tears the frame.
func (m *Model) handleResize(msg tea.WindowSizeMsg) tea.Cmd {
	m.width, m.height = msg.Width, msg.Height
	if !m.ready {
		// The first size must be applied immediately or there is nothing to
		// draw at all.
		m.ready = true
		m.applyLayout()
		return nil
	}
	if m.resize.Observe(time.Now()) {
		return scheduleResize(m.resize.Interval())
	}
	return nil
}

func (m *Model) applyLayout() {
	m.screen = layout.Compute(m.width, m.height)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.Key()
	keystroke := key.Keystroke()

	// Overlays get first refusal, so typing "q" into a filter does not quit.
	if m.overlay != overlayNone {
		if cmd, handled := m.handleOverlayKey(keystroke, key.Text); handled {
			return cmd
		}
	}

	action, ok := m.keys.Action(keystroke)
	if !ok {
		return nil
	}
	switch action {
	case ActionQuit:
		m.quitting = true
		return tea.Quit
	case ActionHelp:
		return m.openOverlay(overlayHelp)
	case ActionPalette:
		return m.openOverlay(overlayPalette)
	case ActionContextPicker:
		return m.openOverlay(overlayContexts)
	case ActionNamespacePicker:
		return m.openOverlay(overlayNamespaces)
	case ActionRefresh:
		return m.refresh()
	case ActionClose:
		m.closeOverlay()
		return nil
	}
	return nil
}

func (m *Model) handleWheel(msg tea.MouseWheelMsg) {
	sel := m.activeSelector()
	if sel == nil {
		return
	}
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		sel.ScrollBy(-1)
	case tea.MouseWheelDown:
		sel.ScrollBy(1)
	}
}

func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	sel := m.activeSelector()
	if sel == nil || msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	rect := m.overlayRect()
	// The list starts after the border, the title and the input line.
	listTop := rect.Y + 1 + 2
	row := msg.Mouse().Y - listTop
	if row < 0 || msg.Mouse().X < rect.X || msg.Mouse().X >= rect.X+rect.Width {
		return nil
	}
	if sel.ClickRow(row) {
		return m.confirmSelection()
	}
	return nil
}

// applyReloadedKubeconfig swaps in a freshly read kubeconfig, keeping the
// session on its current context when that context still exists.
func (m *Model) applyReloadedKubeconfig(msg kubeconfigReloadedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Reload failed: "+kubeclient.FriendlyError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.kubeconfig = msg.cfg
	m.factory.Reset()

	if _, ok := m.kubeconfig.Context(m.contextName); !ok {
		name, err := m.kubeconfig.ResolveStartContext("", "")
		if err != nil {
			m.notice("Active context disappeared from the kubeconfig", theme.StatusCritical)
			m.rebuildCommands()
			return m.expireNotice()
		}
		m.rebuildCommands()
		return m.switchContext(name)
	}

	m.rebuildCommands()
	m.ctxPicker.Refresh()
	m.notice("Kubeconfig reloaded", theme.StatusHealthy)
	return tea.Batch(m.probeCluster(), m.loadNamespaces(), m.expireNotice())
}

// notice sets the transient status message.
func (m *Model) notice(text string, status theme.Status) {
	m.message = text
	m.messageStatus = status
	m.messageSeq++
}

// expireNotice schedules the current message to disappear.
func (m *Model) expireNotice() tea.Cmd { return expireMessage(m.messageSeq, noticeTTL) }

// connectionNotice renders a failed probe as one actionable line.
func connectionNotice(info kubeclient.ClusterInfo) string {
	msg := "Cluster " + info.State.String()
	if info.Hint != "" {
		return msg + ": " + info.Hint
	}
	if info.Err != nil {
		return msg + ": " + kubeclient.FriendlyError(info.Err)
	}
	return msg
}

func shortError(err error) string { return kubeclient.FriendlyError(err) }
