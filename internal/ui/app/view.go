package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/akiesel/kubeui/internal/config"
	kubeclient "github.com/akiesel/kubeui/internal/kube/client"
	"github.com/akiesel/kubeui/internal/ui/async"
	"github.com/akiesel/kubeui/internal/ui/components"
	"github.com/akiesel/kubeui/internal/ui/layout"
	"github.com/akiesel/kubeui/internal/ui/screens"
	"github.com/akiesel/kubeui/internal/ui/theme"
)

// View renders the frame. It is a pure function of the model: nothing is
// fetched, computed lazily or mutated here.
func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "kubeui — " + m.contextName

	if m.quitting {
		v.SetContent("")
		return v
	}
	if !m.ready {
		v.SetContent("Starting kubeui…")
		return v
	}
	if m.screen.TooSmall {
		v.SetContent(m.renderTooSmall())
		return v
	}

	base := strings.Join([]string{
		components.RenderHeader(m.theme, m.headerData(), m.screen.Header.Width),
		m.renderBody(),
		components.RenderStatus(m.theme, m.statusData(), m.screen.Status.Width),
	}, "\n")

	if m.overlay == overlayNone {
		v.SetContent(base)
		return v
	}

	rect := m.overlayRect()
	overlay := m.renderOverlay(rect)
	canvas := lipgloss.NewCanvas(m.screen.Width, m.screen.Height)
	canvas.Compose(lipgloss.NewLayer(base))
	canvas.Compose(lipgloss.NewLayer(overlay).X(rect.X).Y(rect.Y).Z(1))
	v.SetContent(canvas.Render())
	return v
}

func (m *Model) renderTooSmall() string {
	msg := "kubeui needs at least " +
		itoa(layout.MinWidth) + "x" + itoa(layout.MinHeight) + " — this terminal is " +
		itoa(m.screen.Width) + "x" + itoa(m.screen.Height)
	return m.theme.Warning.Render(msg)
}

func (m *Model) headerData() components.HeaderData {
	kctx := m.currentContext()
	d := components.HeaderData{
		Context:    m.contextName,
		Production: kctx.Production,
		Scope:      m.scopeLabel(),
		Version:    m.version(),
		Breadcrumb: []string{"Cluster", m.scopeLabel(), "Overview"},
	}

	info := m.cluster.Get()
	switch m.cluster.State() {
	case async.Idle, async.Loading:
		d.ConnStatus = theme.StatusUnknown
		d.ConnGlyph = m.theme.Glyphs.Pending
		d.ConnLabel = "connecting"
	case async.Failed:
		d.ConnStatus = theme.StatusCritical
		d.ConnLabel = "unreachable"
	case async.Ready:
		switch info.State {
		case kubeclient.ConnOK:
			d.ConnStatus = theme.StatusHealthy
			d.ConnLabel = "connected"
			detail := info.ServerVersion
			if info.Latency > 0 {
				detail += " " + formatLatency(info.Latency)
			}
			d.ConnDetail = strings.TrimSpace(detail)
		case kubeclient.ConnForbidden:
			d.ConnStatus = theme.StatusWarning
			d.ConnLabel = info.State.String()
		default:
			d.ConnStatus = theme.StatusCritical
			d.ConnLabel = info.State.String()
		}
	}
	return d
}

func (m *Model) statusData() components.StatusData {
	if m.message != "" {
		return components.StatusData{Message: m.message, MessageStatus: m.messageStatus}
	}

	hints := []components.KeyHint{
		{Key: m.keys.Key(ActionPalette), Desc: "Commands"},
		{Key: m.keys.Key(ActionContextPicker), Desc: "Cluster"},
		{Key: m.keys.Key(ActionNamespacePicker), Desc: "Namespace"},
		{Key: m.keys.Key(ActionRefresh), Desc: "Refresh"},
		{Key: m.keys.Key(ActionHelp), Desc: "Help"},
		{Key: m.keys.Key(ActionQuit), Desc: "Quit"},
	}
	if m.overlay != overlayNone {
		hints = []components.KeyHint{
			{Key: "↑↓", Desc: "Navigate"},
			{Key: "Enter", Desc: "Select"},
			{Key: "Esc", Desc: "Cancel"},
		}
	}
	return components.StatusData{Hints: hints}
}

func (m *Model) renderBody() string {
	body := m.screen.Body
	content := screens.RenderOverview(m.theme, m.overviewData(), body.Width, body.Height)
	return padBlock(content, body.Width, body.Height)
}

// overviewData assembles the Phase 1 body: what kubeui knows for certain about
// the session, and an explicit list of what it cannot do yet. An honest gap is
// better than a plausible-looking placeholder.
func (m *Model) overviewData() screens.OverviewData {
	kctx := m.currentContext()
	info := m.cluster.Get()

	connection := screens.Panel{Title: "Connection"}
	switch m.cluster.State() {
	case async.Idle, async.Loading:
		connection.Fields = append(connection.Fields, screens.Field{
			Label: "Status", Value: "Connecting…", Status: theme.StatusUnknown, Glyph: true,
		})
	case async.Ready, async.Failed:
		status := theme.StatusCritical
		switch info.State {
		case kubeclient.ConnOK:
			status = theme.StatusHealthy
		case kubeclient.ConnForbidden:
			status = theme.StatusWarning
		}
		connection.Fields = append(connection.Fields, screens.Field{
			Label: "Status", Value: info.State.String(), Status: status, Glyph: true,
		})
		if info.ServerVersion != "" {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Version", Value: info.ServerVersion,
			})
		}
		if info.Latency > 0 {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Latency", Value: formatLatency(info.Latency),
			})
		}
		if info.Err != nil {
			connection.Fields = append(connection.Fields, screens.Field{
				Label: "Error", Value: kubeclient.FriendlyError(info.Err), Status: theme.StatusCritical,
			})
		}
		if info.Hint != "" {
			connection.Note = info.Hint
		}
	}
	connection.Fields = append(connection.Fields, screens.Field{Label: "Server", Value: orNone(kctx.Server)})

	session := screens.Panel{Title: "Session"}
	contextValue := m.contextName
	contextStatus := theme.StatusUnknown
	if kctx.Production {
		contextValue += "  (production)"
		contextStatus = theme.StatusCritical
	}
	session.Fields = []screens.Field{
		{Label: "Context", Value: contextValue, Status: contextStatus},
		{Label: "Scope", Value: m.scopeLabel()},
		{Label: "User", Value: orNone(kctx.User)},
		{Label: "Namespaces", Value: m.namespacesSummary()},
	}
	session.Note = "Switching context here does not change your kubectl context."

	environment := screens.Panel{Title: "Environment"}
	environment.Fields = []screens.Field{
		{Label: "Kubeconfig", Value: orNone(strings.Join(m.kubeconfig.Sources, ", "))},
		{Label: "Config", Value: configLabel(m.cfg.SourcePath, m.configPath)},
		{Label: "Theme", Value: themeLabel(m.cfg.Theme, m.caps)},
		{Label: "Terminal", Value: terminalLabel(m.caps, m.screen)},
	}

	return screens.OverviewData{
		Panels: []screens.Panel{connection, session, environment},
		Roadmap: []string{
			"Application-first dashboard (Phase 2)",
			"WHY diagnosis engine (Phase 3)",
			"Logs, exec and resource inspection (Phase 4)",
		},
	}
}

// namespacesSummary states the namespace count without ever implying that a
// pending request means an empty cluster.
func (m *Model) namespacesSummary() string {
	switch m.namespaces.State() {
	case async.Idle:
		return "not loaded"
	case async.Loading:
		return "loading…"
	case async.Failed:
		return "unavailable — " + kubeclient.FriendlyError(m.namespaces.Err())
	}
	list := m.namespaces.Get()
	switch {
	case list.Restricted:
		return "listing not permitted for this user"
	case len(list.Names) == 0:
		return "none visible"
	case list.Truncated:
		return itoa(len(list.Names)) + "+ visible"
	default:
		return itoa(len(list.Names)) + " visible"
	}
}

func (m *Model) renderOverlay(rect layout.Rect) string {
	inner := rect.Width - 4   // border + horizontal padding
	innerH := rect.Height - 2 // border
	if inner < 8 {
		inner = 8
	}
	if innerH < 3 {
		innerH = 3
	}

	var content string
	if m.overlay == overlayHelp {
		content = m.renderHelp(inner, innerH)
	} else if sel := m.activeSelector(); sel != nil {
		content = sel.Render(m.theme, inner, innerH)
	}
	return m.theme.Overlay.Width(rect.Width).Render(content)
}

func (m *Model) renderHelp(width, height int) string {
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"Navigate", [][2]string{
			{m.keys.Key(ActionPalette), "Command palette — every action, by name"},
			{m.keys.Key(ActionContextPicker), "Switch cluster"},
			{m.keys.Key(ActionNamespacePicker), "Switch namespace"},
		}},
		{"Cluster", [][2]string{
			{m.keys.Key(ActionRefresh), "Refresh cluster status"},
		}},
		{"General", [][2]string{
			{m.keys.Key(ActionHelp), "This help"},
			{"Esc", "Close overlay"},
			{m.keys.Key(ActionQuit), "Quit"},
		}},
		{"In lists", [][2]string{
			{"↑ ↓", "Move"},
			{"PgUp PgDn", "Page"},
			{"Enter", "Select"},
			{"Type", "Fuzzy filter"},
		}},
	}

	var b strings.Builder
	b.WriteString(m.theme.OverlayTitle.Render("Keyboard shortcuts"))
	for _, s := range sections {
		b.WriteString("\n\n" + m.theme.Muted.Render(s.title))
		for _, row := range s.rows {
			if row[0] == "" {
				continue
			}
			b.WriteString("\n  " + m.theme.Key.Render(padTo(row[0], 12)) + m.theme.KeyDesc.Render(row[1]))
		}
	}
	b.WriteString("\n\n" + m.theme.Muted.Render("Keys are configurable in "+orNone(m.configPath)))
	return clipTo(b.String(), width, height)
}

func padBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		if lipgloss.Width(l) < width {
			lines[i] = l + strings.Repeat(" ", width-lipgloss.Width(l))
		}
	}
	return strings.Join(lines, "\n")
}

func clipTo(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func padTo(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return itoa(int(d.Round(time.Millisecond)/time.Millisecond)) + "ms"
}

// configLabel names the config file, saying plainly when none exists rather
// than showing an empty value.
func configLabel(source, path string) string {
	if source != "" {
		return source
	}
	if path == "" {
		return "—"
	}
	return path + " (not created)"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// themeLabel reports the effective theme, naming the preference only when it
// differs from what was detected.
func themeLabel(pref config.Theme, caps theme.Capabilities) string {
	if !caps.Color {
		return "no colour"
	}
	mode := "light"
	if caps.Dark {
		mode = "dark"
	}
	if pref == config.ThemeAuto || pref == "" {
		return mode + " (auto)"
	}
	return mode
}

func terminalLabel(caps theme.Capabilities, screen layout.Screen) string {
	label := itoa(screen.Width) + "x" + itoa(screen.Height)
	if caps.Unicode {
		return label + ", unicode"
	}
	return label + ", ascii"
}
