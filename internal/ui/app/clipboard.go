package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/decode"
	"github.com/aronk11/correlux/internal/kube/logs"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// copyTarget names the one thing "c" copies right now, and what to call it in
// the notice once it has been. The richer, less common copies — YAML, JSON,
// the equivalent kubectl command, logs, a table — live in the palette only
// (SPEC 18), the same way cmd.session and the fleet-group commands do: a
// hotkey for every one of them would be a hotkey nobody could remember.
func (m *Model) copyTarget() (label, value string, ok bool) {
	switch m.view {
	case viewObject:
		if m.objectTarget.empty() {
			return "", "", false
		}
		return m.objectTarget.label(), copyRef(m.objectTarget), true
	case viewApplication:
		_, targets := m.applicationView()
		if m.detailPort.Cursor >= 0 && m.detailPort.Cursor < len(targets) {
			ref := targets[m.detailPort.Cursor]
			return ref.label(), copyRef(ref), true
		}
		if app, ok := m.currentApplication(); ok {
			return app.Name, copyRef(objectRef{Name: app.Name, Namespace: app.Namespace}), true
		}
	case viewTable:
		rows := m.visibleRows()
		if m.tablePort.Cursor < 0 || m.tablePort.Cursor >= len(rows) {
			return "", "", false
		}
		row := rows[m.tablePort.Cursor]
		ns := rowNamespace(row.Namespace, m.resource.Namespaced, m.namespace, m.allNamespaces)
		return row.Name, copyRef(objectRef{Name: row.Name, Namespace: ns}), true
	case viewLogs:
		if len(m.logLines) == 0 {
			return "", "", false
		}
		return "the logs on screen", joinLogLines(m.logLines), true
	}
	return "", "", false
}

// copyRef renders an object the way it is worth pasting elsewhere: namespaced
// as "namespace/name", cluster-scoped as just the name.
func copyRef(ref objectRef) string {
	if ref.Namespace == "" {
		return ref.Name
	}
	return ref.Namespace + "/" + ref.Name
}

func joinLogLines(lines []logs.Line) string {
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = l.Text
	}
	return strings.Join(parts, "\n")
}

// copyPrimary copies the one thing "c" means on the current screen.
func (m *Model) copyPrimary() tea.Cmd {
	label, value, ok := m.copyTarget()
	if !ok {
		m.notice("Nothing to copy here", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.copy(label, value)
}

// copy sends a value to the system clipboard over OSC 52, which works over
// SSH and does not assume pbcopy or xclip exist on the machine Correlux runs
// on (SPEC 18).
func (m *Model) copy(label, value string) tea.Cmd {
	m.notice("Copied "+label, theme.StatusHealthy)
	return tea.Batch(tea.SetClipboard(value), m.expireNotice())
}

// copyObjectYAML copies the document exactly as objectView is showing it: the
// server's own values, or the decoded ones when that toggle is on.
func (m *Model) copyObjectYAML() tea.Cmd {
	obj := m.object.Get()
	if obj == nil {
		m.notice("The object is still loading", theme.StatusWarning)
		return m.expireNotice()
	}
	document := obj.YAML
	if m.objectDecode {
		if decoded, values := decode.Document(obj.Raw); values > 0 {
			document = decoded
		}
	}
	return m.copy("the YAML", document)
}

// copyObjectJSON copies the document exactly as the server holds it: Raw is
// already the server's JSON, so there is nothing to convert.
func (m *Model) copyObjectJSON() tea.Cmd {
	obj := m.object.Get()
	if obj == nil {
		m.notice("The object is still loading", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.copy("the JSON", string(obj.Raw))
}

// copyKubectlCommand copies the command that reads the same object with
// kubectl, so a Correlux session someone else is on can be handed a command
// line instead of a namespace and a name.
func (m *Model) copyKubectlCommand() tea.Cmd {
	if m.objectTarget.empty() {
		m.notice("Nothing to copy here", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.copy("the kubectl command", kubectlGet(m, m.objectTarget))
}

// kubectlGet renders the kubectl command that reads ref the way Correlux just
// did: the resource's plural name when discovery knows it, the bare kind
// otherwise.
func kubectlGet(m *Model, ref objectRef) string {
	kind := strings.ToLower(ref.Kind)
	if res, ok := m.resourceFor(ref); ok {
		kind = res.Plural()
	}
	cmd := "kubectl --context " + m.contextName + " get " + kind + " " + ref.Name
	if ref.Namespace != "" {
		cmd += " -n " + ref.Namespace
	}
	return cmd + " -o yaml"
}

// copyVisibleLogs copies exactly the lines the log view has loaded.
func (m *Model) copyVisibleLogs() tea.Cmd {
	if len(m.logLines) == 0 {
		m.notice("Nothing to copy here", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.copy(itoa(len(m.logLines))+" log lines", joinLogLines(m.logLines))
}

// copyVisibleTable renders the table on screen as tab-separated text: a
// header row of column names, then one row per object, in the order shown.
func (m *Model) copyVisibleTable() tea.Cmd {
	rows := m.visibleRows()
	table := m.table.Get()
	if len(rows) == 0 || table == nil {
		m.notice("Nothing to copy here", theme.StatusWarning)
		return m.expireNotice()
	}
	return m.copy(itoa(len(rows))+" rows", tableAsText(table.Columns, rows))
}

// tableAsText renders a table as tab-separated text: a header row of column
// names, then one row per object, in the order given. It is separate from
// copyVisibleTable so the rendering can be checked without a loaded model.
func tableAsText(columns []resources.Column, rows []resources.Row) string {
	var b strings.Builder
	for i, col := range columns {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(col.Name)
	}
	for _, row := range rows {
		b.WriteByte('\n')
		b.WriteString(strings.Join(row.Cells, "\t"))
	}
	return b.String()
}
