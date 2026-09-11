package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/domain/diff"
	"github.com/aronk11/correlux/internal/ui/theme"
)

type helmValuesLoadedMsg struct {
	cluster     string
	ref         objectRef
	args        []string
	consequence string
	values      []byte
	err         error
}
type helmValuesEditedMsg struct {
	draft helmValuesLoadedMsg
	path  string
	err   error
}

func (m *Model) prepareHelmValues(ref objectRef, args []string, consequence string) tea.Cmd {
	factory, cluster := m.factory, m.contextName
	m.notice("Loading Helm values for review…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		values, err := factory.Helm(ctx, cluster, ref.Namespace, []string{"get", "values", ref.Name, "--output", "yaml"})
		return helmValuesLoadedMsg{cluster: cluster, ref: ref, args: args, consequence: consequence, values: values, err: err}
	}
}

func (m *Model) applyHelmValuesLoaded(msg *helmValuesLoadedMsg) tea.Cmd {
	if msg.cluster != m.contextName || msg.ref != m.objectTarget || m.view != viewObject {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not load Helm values: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	file, err := os.CreateTemp("", "correlux-review-helm-values-*.yaml")
	if err != nil {
		m.notice(err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	if _, err = file.Write(msg.values); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		m.notice(err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(file.Name())
		m.notice(closeErr.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	editor, args := editorCommand()
	cmd := exec.CommandContext(context.Background(), editor, append(args, file.Name())...) //nolint:gosec // user's configured editor, no shell
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return helmValuesEditedMsg{draft: *msg, path: file.Name(), err: err} })
}

func (m *Model) applyHelmValuesEdited(msg *helmValuesEditedMsg) tea.Cmd {
	defer os.Remove(msg.path)
	if msg.draft.cluster != m.contextName || msg.draft.ref != m.objectTarget {
		return nil
	}
	if msg.err != nil {
		m.notice("Helm values editor failed: "+msg.err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	values, err := os.ReadFile(msg.path) //nolint:gosec // private file created immediately before launching the editor
	if err != nil {
		m.notice(err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	raw, err := yaml.YAMLToJSONStrict(values)
	var mapping map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &mapping)
	}
	if err != nil {
		m.notice("Helm values must be a YAML mapping without duplicate keys: "+err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	changes := diff.Lines(splitDocument(string(msg.draft.values)), splitDocument(string(values)))
	cmd := m.confirmHelmValuesArgs(msg.draft.ref, "upgrade", msg.draft.args, msg.draft.consequence, values)
	if m.pending != nil {
		m.pending.Diff = diff.Hunks(changes, 1)
	}
	return cmd
}
