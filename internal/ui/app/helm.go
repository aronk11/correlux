package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const helmResource = "helm.correlux.internal"
const paletteHelm palette.ActionID = "helm.action"
const helmPageSize = 200

func helmRef(name, namespace, mode string) objectRef {
	return objectRef{Kind: "Helm release", Name: name, Namespace: namespace, Resource: helmResource, HelmMode: mode}
}

func (m *Model) helmCommands() []palette.Command {
	if m.view == viewFleet || m.view == viewFleetResource {
		return nil
	}
	commands := []palette.Command{{ID: "helm.list", Action: paletteHelm, Arg: "list", Title: "Helm: browse releases", Subtitle: m.scopeLabel(), Category: "Delivery", Enabled: true}}
	if m.view != viewObject || m.objectTarget.Resource != helmResource || m.objectTarget.HelmMode == "list" {
		return commands
	}
	for _, entry := range [][2]string{{"status", "Inspect release"}, {"history", "Revision history"}, {"values", "Show supplied values"}, {"computed-values", "Show computed values"}, {"manifest", "Show rendered manifests"}, {"hooks", "Show hooks"}, {"notes", "Show chart notes"}, {"upgrade", "Upgrade release with a chart"}, {"rollback", "Roll back to a revision"}, {"test", "Run chart tests"}, {"uninstall", "Uninstall release"}} {
		commands = append(commands, palette.Command{ID: "helm." + entry[0], Action: paletteHelm, Arg: entry[0], Title: "Helm: " + entry[1], Subtitle: m.objectTarget.Name, Category: "Delivery", Enabled: true})
	}
	return commands
}

func (m *Model) openHelm(mode string) tea.Cmd {
	if mode == "list" {
		ns := m.namespace
		if m.allNamespaces {
			ns = ""
		}
		return m.openObject(helmRef("releases", ns, "list"))
	}
	ref := m.objectTarget
	if ref.Resource != helmResource {
		return nil
	}
	switch mode {
	case "upgrade":
		m.promptTitle = "Upgrade Helm release " + ref.Name
		m.promptNote = "Enter chart reference and optional version, e.g. oci://registry.example/charts/api 1.2.3. The current values open in your editor before confirmation."
		if m.cfg.AirGapped {
			m.promptNote = "Air-gapped: enter a local chart path (with dependencies bundled) or an internal registry reference. Values open in your editor before confirmation."
		}
		m.promptRef = objectRef{}
		m.promptError = ""
		m.promptInput.SetValue("")
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			fields := strings.Fields(value)
			if len(fields) < 1 || len(fields) > 2 || strings.HasPrefix(fields[0], "-") {
				m.promptError = "Enter a chart reference and optional version"
				return nil
			}
			args := []string{"upgrade", ref.Name, fields[0], "--reset-values", "--wait", "--timeout", "5m"}
			if len(fields) == 2 {
				args = append(args, "--version", fields[1])
			}
			m.cancelPrompt()
			return m.prepareHelmValues(ref, args, "Upgrades all release resources using chart "+value+" and your reviewed values. Chart defaults are applied, hooks may run, and workloads may be replaced.")
		}
		return nil
	case "rollback":
		m.promptTitle = "Rollback Helm release " + ref.Name
		m.promptNote = "Enter a positive revision from release history. Helm will create a new revision."
		m.promptRef = objectRef{}
		m.promptError = ""
		m.promptInput.SetValue("")
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 1 {
				m.promptError = "Enter a positive revision number"
				return nil
			}
			m.cancelPrompt()
			return m.confirmHelm(ref, "rollback", n)
		}
		return nil
	case "test", "uninstall":
		return m.confirmHelm(ref, mode, 0)
	}
	ref.HelmMode = mode
	return m.openObject(ref)
}

func helmReadArgs(ref objectRef) []string {
	args := helmBaseReadArgs(ref)
	if ref.HelmRevision > 0 && ref.HelmMode != "status" && ref.HelmMode != "history" && ref.HelmMode != "list" {
		args = append(args, "--revision", strconv.Itoa(ref.HelmRevision))
	}
	return args
}

func helmBaseReadArgs(ref objectRef) []string {
	switch ref.HelmMode {
	case "list":
		args := []string{"list", "--all", "--output", "json", "--max", strconv.Itoa(helmPageSize), "--offset", strconv.Itoa(ref.HelmOffset)}
		if ref.Namespace == "" {
			args = append(args, "--all-namespaces")
		}
		return args
	case "history":
		return []string{"history", ref.Name, "--output", "json", "--max", "256"}
	case "status":
		args := []string{"status", ref.Name, "--output", "json"}
		if ref.HelmRevision > 0 {
			args = append(args, "--revision", strconv.Itoa(ref.HelmRevision))
		}
		return args
	case "values", "computed-values":
		args := []string{"get", "values", ref.Name, "--output", "yaml"}
		if ref.HelmMode == "computed-values" {
			args = append(args, "--all")
		}
		return args
	default:
		return []string{"get", ref.HelmMode, ref.Name}
	}
}

func (m *Model) loadHelmObject(ref objectRef, gen uint64) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelObject = cancel
	m.objectLoading = true
	factory, cluster := m.factory, m.contextName
	return func() tea.Msg {
		defer cancel()
		raw, err := factory.Helm(ctx, cluster, ref.Namespace, helmReadArgs(ref))
		if err != nil {
			return objectLoadedMsg{gen: gen, err: err}
		}
		document := string(raw)
		if converted, e := yaml.JSONToYAML(raw); e == nil {
			document = string(converted)
		}
		return objectLoadedMsg{gen: gen, object: &resources.Object{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Raw: raw, YAML: document}}
	}
}

func (m *Model) helmView(d screens.ObjectData, obj *resources.Object) (screens.ObjectData, []objectRef) {
	ref := m.objectTarget
	d.Kind = "Helm"
	d.Subtitle = ref.HelmMode + " • " + m.contextName
	d.YAML = splitDocument(obj.YAML)
	if ref.HelmMode != "list" && ref.HelmMode != "status" && ref.HelmMode != "history" {
		d.ShowYAML = true
		return d, nil
	}
	var targets []objectRef
	add := func(r objectRef) int { targets = append(targets, r); return len(targets) - 1 }
	switch ref.HelmMode {
	case "list":
		var entries []struct {
			Name       string `json:"name"`
			Namespace  string `json:"namespace"`
			Revision   string `json:"revision"`
			Status     string `json:"status"`
			Chart      string `json:"chart"`
			AppVersion string `json:"app_version"`
		}
		if err := json.Unmarshal(obj.Raw, &entries); err != nil {
			d.Message = "Could not decode Helm release list: " + err.Error()
			return d, nil
		}
		section := screens.DetailSection{Title: "Releases", Columns: []string{"Release", "Namespace", "Revision", "Status", "Chart", "App version"}, Empty: "no Helm releases in this scope"}
		for _, entry := range entries {
			section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{entry.Name, entry.Namespace, entry.Revision, entry.Status, entry.Chart, entry.AppVersion}, Target: add(helmRef(entry.Name, entry.Namespace, "status"))})
		}
		if len(entries) == helmPageSize {
			next := ref
			next.HelmOffset += helmPageSize
			section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{"Next page…", "", "", "", "", ""}, Target: add(next)})
			d.Subtitle += " • more releases may exist"
		}
		d.Subtitle += fmt.Sprintf(" • offset %d, %d loaded", ref.HelmOffset, len(entries))
		d.Sections = []screens.DetailSection{section}
	case "history":
		var entries []struct {
			Revision    int    `json:"revision"`
			Status      string `json:"status"`
			Chart       string `json:"chart"`
			Updated     string `json:"updated"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(obj.Raw, &entries); err != nil {
			d.Message = "Could not decode Helm history: " + err.Error()
			return d, nil
		}
		section := screens.DetailSection{Title: "History (up to 256 retained revisions)", Columns: []string{"Revision", "Status", "Chart", "Updated", "Description"}, Empty: "no retained revisions"}
		for _, entry := range entries {
			next := ref
			next.HelmMode = "status"
			next.HelmRevision = entry.Revision
			section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{strconv.Itoa(entry.Revision), entry.Status, entry.Chart, entry.Updated, entry.Description}, Target: add(next)})
		}
		d.Sections = []screens.DetailSection{section}
	case "status":
		var release struct {
			Version int `json:"version"`
			Info    struct {
				Status        string `json:"status"`
				Description   string `json:"description"`
				FirstDeployed string `json:"first_deployed"`
				LastDeployed  string `json:"last_deployed"`
			} `json:"info"`
			Chart struct {
				Metadata struct{ Name, Version, AppVersion string } `json:"metadata"`
			} `json:"chart"`
		}
		if err := json.Unmarshal(obj.Raw, &release); err != nil {
			d.Message = "Could not decode Helm status: " + err.Error()
			return d, nil
		}
		d.Headline = release.Info.Status
		d.Subtitle += " • revision " + strconv.Itoa(release.Version)
		s := screens.DetailSection{Title: "Release", Columns: []string{"Field", "Value"}}
		for _, row := range [][2]string{{"Chart", release.Chart.Metadata.Name}, {"Chart version", release.Chart.Metadata.Version}, {"App version", release.Chart.Metadata.AppVersion}, {"Description", release.Info.Description}, {"First deployed", release.Info.FirstDeployed}, {"Last deployed", release.Info.LastDeployed}} {
			s.Rows = append(s.Rows, screens.DetailRow{Cells: []string{row[0], row[1]}, Target: -1})
		}
		links := screens.DetailSection{Title: "Inspect", Columns: []string{"View"}}
		for _, mode := range []string{"history", "values", "computed-values", "manifest", "hooks", "notes"} {
			next := ref
			next.HelmMode = mode
			next.HelmRevision = ref.HelmRevision
			links.Rows = append(links.Rows, screens.DetailRow{Cells: []string{mode}, Target: add(next)})
		}
		d.Sections = []screens.DetailSection{s, links, m.helmManifestSection(obj.Raw, ref.Namespace, add)}
	}
	return d, targets
}

type helmChangedMsg struct {
	cluster string
	ref     objectRef
	action  string
	output  []byte
	err     error
}

func (m *Model) confirmHelm(ref objectRef, action string, revision int) tea.Cmd {
	args := []string{action, ref.Name}
	consequence := "Runs chart test hooks, which can create workloads and affect application data."
	if action == "rollback" {
		args = append(args, strconv.Itoa(revision))
		consequence = fmt.Sprintf("Restores release %s to revision %d; workloads and hooks may change.", ref.Name, revision)
	}
	if action == "uninstall" {
		consequence = "Removes this Helm release's managed resources and release history; workloads will stop."
	}
	args = append(args, "--timeout", "5m")
	return m.confirmHelmArgs(ref, action, args, consequence)
}

func (m *Model) confirmHelmArgs(ref objectRef, action string, args []string, consequence string) tea.Cmd {
	return m.confirmHelmValuesArgs(ref, action, args, consequence, nil)
}

func (m *Model) confirmHelmValuesArgs(ref objectRef, action string, args []string, consequence string, values []byte) tea.Cmd {
	cluster := m.contextName
	return m.confirm(pendingAction{Title: "Helm: " + action + " " + ref.Name, Lines: []string{consequence, "Release " + ref.Name + " in " + ref.Namespace, "If Flux manages this release, it may restore desired state. Prefer changing its HelmRelease source."}, Challenge: m.productionChallenge(), Danger: true, Run: func(m *Model) tea.Cmd {
		if cluster != m.contextName {
			return nil
		}
		factory := m.factory
		m.notice("Helm "+action+" running…", theme.StatusUnknown)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			output, err := factory.HelmWithValues(ctx, cluster, ref.Namespace, args, values)
			return helmChangedMsg{cluster: cluster, ref: ref, action: action, output: output, err: err}
		}
	}})
}

func (m *Model) applyHelmChanged(msg *helmChangedMsg) tea.Cmd {
	if msg.cluster != m.contextName {
		return nil
	}
	if msg.err != nil {
		m.notice("Helm "+msg.action+" failed: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Helm "+msg.action+" completed for "+msg.ref.Name, theme.StatusHealthy)
	if m.view == viewObject && m.objectTarget.Resource == helmResource {
		if msg.action == "uninstall" {
			return tea.Batch(m.showObject(helmRef("releases", msg.ref.Namespace, "list")), m.loadApplications(), m.expireNotice())
		}
		return tea.Batch(m.loadObject(), m.loadApplications(), m.expireNotice())
	}
	return tea.Batch(m.loadApplications(), m.expireNotice())
}
