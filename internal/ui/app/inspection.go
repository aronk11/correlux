package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const inspectionResource = "inspection.correlux.internal"
const paletteInspect palette.ActionID = "inspect.action"

func (r objectRef) subject() objectRef {
	r.Resource = r.SubjectResource
	r.InspectionMode = ""
	r.SourceNamespace = ""
	r.SourcePod = ""
	r.CompareContext = ""
	r.CompareNamespace = ""
	r.CompareName = ""
	r.SubjectResource = ""
	return r
}
func inspectedRef(ref objectRef, mode string) objectRef {
	ref.SubjectResource = ref.lookup()
	ref.Resource = inspectionResource
	ref.InspectionMode = mode
	return ref
}
func inspectRef(ref objectRef) inspection.Ref {
	return inspection.Ref{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Resource: ref.lookup()}
}

func (m *Model) inspectionCommands() []palette.Command {
	var out []palette.Command
	if m.view == viewFleet {
		return []palette.Command{{ID: "inspect.fleet", Action: paletteInspect, Arg: "fleet", Title: "Compare loaded fleet applications by namespace/name", Category: "Investigate", Enabled: true}}
	}
	if m.view == viewObject && m.objectTarget.Resource == inspectionResource {
		return []palette.Command{{ID: "inspect.export", Action: paletteInspect, Arg: "export", Title: "Export the visible investigation report", Subtitle: "preview is the report on screen", Category: "Investigate", Enabled: m.object.HasValue()}}
	}
	if m.view != viewObject || m.object.Get() == nil {
		return out
	}
	ref := m.objectTarget
	res, ok := m.resourceFor(ref)
	if !ok {
		return out
	}
	add := func(id, title string) {
		out = append(out, palette.Command{ID: "inspect." + id, Action: paletteInspect, Arg: id, Title: title, Subtitle: ref.label(), Category: "Investigate", Enabled: true})
	}
	switch ref.Kind {
	case "Secret", "ConfigMap", "PersistentVolumeClaim", "ServiceAccount":
		if res.GVR.Group == "" {
			add("references", "Find referencing workloads")
		}
	}
	switch ref.Kind {
	case "Pod", "Deployment", "StatefulSet", "DaemonSet", "Job":
		if res.GVR.Group == "" || res.GVR.Group == "apps" || res.GVR.Group == "batch" {
			add("workload", "Inspect rollout, scheduling and namespace constraints")
		}
	}
	if res.GVR.Group == "" && ref.Kind == "PersistentVolumeClaim" {
		add("storage", "Inspect storage and node attachments")
	}
	if res.GVR.Group == "" && ref.Kind == "Service" {
		add("network", "Inspect service routing and network policies")
	}
	add("compare", "Compare configuration with another cluster")
	add("timeline", "Show changes observed in this session")
	add("snapshot", "Capture a configuration snapshot")
	add("snapshot-diff", "Compare with the captured snapshot")
	add("snapshot-export", "Preview captured snapshot for export")
	add("snapshot-import", "Compare with a saved snapshot file")
	return out
}

func (m *Model) openInspection(action string) tea.Cmd {
	if action == "fleet" {
		return m.showFleetComparison()
	}
	if action == "export" {
		return m.exportInvestigation()
	}
	ref := m.objectTarget
	if m.view != viewObject || m.object.Get() == nil {
		return nil
	}
	switch action {
	case "snapshot-export":
		return m.previewSnapshotFile()
	case "snapshot-import":
		return m.compareSnapshotFile()
	case "snapshot":
		return m.captureSnapshot()
	case "snapshot-diff":
		return m.showSnapshotDiff()
	case "timeline":
		return m.showObservedChanges()
	case "network":
		m.promptTitle = "Source pod for routing inspection"
		m.promptNote = "Enter namespace/pod, or leave empty to inspect only destination routing. This reads configuration; it does not send probe traffic."
		m.promptRef = objectRef{}
		m.promptError = ""
		m.promptInput.SetValue("")
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			r := inspectedRef(ref, "network")
			if value = strings.TrimSpace(value); value != "" {
				ns, name, ok := strings.Cut(value, "/")
				if !ok || len(validation.IsDNS1123Label(ns)) > 0 || len(validation.IsDNS1123Subdomain(name)) > 0 {
					m.promptError = "Enter namespace/pod"
					return nil
				}
				r.SourceNamespace = ns
				r.SourcePod = name
			}
			m.cancelPrompt()
			return m.openObject(r)
		}
		return nil
	case "compare":
		return m.promptComparison(ref)
	}
	return m.openObject(inspectedRef(ref, action))
}

func (m *Model) promptComparison(ref objectRef) tea.Cmd {
	m.promptTitle = "Compare with cluster"
	m.promptNote = "Enter a kubeconfig context. Only this explicitly selected target is contacted."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue("")
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		cluster := strings.TrimSpace(value)
		if _, ok := m.kubeconfig.Context(cluster); !ok {
			m.promptError = "Choose a context present in kubeconfig"
			return nil
		}
		m.cancelPrompt()
		m.promptTitle = "Map the object in " + cluster
		m.promptNote = "Enter namespace/name, or /name for a cluster-scoped resource. Matching is your explicit choice."
		m.promptInput.SetValue(ref.Namespace + "/" + ref.Name)
		m.promptRef = objectRef{}
		m.promptError = ""
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			ns, name, ok := strings.Cut(strings.TrimSpace(value), "/")
			if !ok || (ns != "" && len(validation.IsDNS1123Label(ns)) > 0) || len(validation.IsDNS1123Subdomain(name)) > 0 {
				m.promptError = "Enter namespace/name"
				return nil
			}
			r := inspectedRef(ref, "compare")
			r.CompareContext = cluster
			r.CompareNamespace = ns
			r.CompareName = name
			m.cancelPrompt()
			return m.openObject(r)
		}
		return nil
	}
	return nil
}

func (m *Model) loadInspection(ref objectRef, gen uint64) tea.Cmd {
	if ref.InspectionMode == "timeline" || ref.InspectionMode == "snapshot-diff" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelObject = cancel
	m.objectLoading = true
	factory, cluster := m.factory, m.contextName
	subject := ref.subject()
	res, served := m.resourceFor(subject)
	return func() tea.Msg {
		defer cancel()
		var report inspection.Report
		var err error
		switch ref.InspectionMode {
		case "workload":
			if !served {
				err = errors.New("source resource is no longer served")
			} else {
				report, err = factory.WorkloadContext(ctx, cluster, inspectRef(subject), res)
			}
		case "storage":
			report, err = factory.StorageContext(ctx, cluster, ref.Namespace, ref.Name)
		case "references":
			report, err = factory.ReferencedBy(ctx, cluster, inspectRef(subject))
		case "network":
			report, err = factory.ServiceRouting(ctx, cluster, ref.Namespace, ref.Name, ref.SourceNamespace, ref.SourcePod)
		case "compare":
			if !served {
				err = errors.New("source resource is no longer served")
			} else {
				right := inspection.Ref{Kind: ref.Kind, Name: ref.CompareName, Namespace: ref.CompareNamespace, Resource: ref.SubjectResource}
				report, err = factory.CompareObjects(ctx, cluster, ref.CompareContext, inspectRef(subject), right, res)
			}
		default:
			err = errors.New("unknown investigation")
		}
		if err != nil {
			return objectLoadedMsg{gen: gen, err: err}
		}
		return objectLoadedMsg{gen: gen, object: reportObject(ref, report)}
	}
}

func reportObject(ref objectRef, report inspection.Report) *resources.Object {
	raw, _ := json.Marshal(report)
	document, _ := yaml.JSONToYAML(raw)
	return &resources.Object{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Raw: raw, YAML: string(document)}
}

func (m *Model) inspectionView(d screens.ObjectData, obj *resources.Object) (screens.ObjectData, []objectRef) {
	var report inspection.Report
	if err := json.Unmarshal(obj.Raw, &report); err != nil {
		d.Message = "Could not decode investigation: " + err.Error()
		return d, nil
	}
	d.Kind = "Investigation"
	d.Name = report.Title
	d.Subtitle = report.Summary + " • " + report.At.Local().Format(time.RFC3339)
	d.YAML = splitDocument(obj.YAML)
	var refs []objectRef
	for _, section := range report.Sections {
		s := screens.DetailSection{Title: section.Title, Columns: section.Columns, Empty: section.Empty}
		for _, row := range section.Rows {
			r := screens.DetailRow{Cells: row.Cells, Target: -1}
			if row.Ref != nil {
				ref := row.Ref
				refs = append(refs, objectRef{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Resource: ref.Resource})
				r.Target = len(refs) - 1
			}
			s.Rows = append(s.Rows, r)
		}
		d.Sections = append(d.Sections, s)
	}
	if len(report.Gaps) > 0 {
		s := screens.DetailSection{Title: "Evidence gaps and limits", Columns: []string{"Detail"}}
		for _, gap := range report.Gaps {
			s.Rows = append(s.Rows, screens.DetailRow{Cells: []string{gap}, Target: -1})
		}
		d.Sections = append(d.Sections, s)
	}
	return d, refs
}

func (m *Model) reportKey(ref objectRef) string {
	raw, _ := json.Marshal(ref)
	return m.contextName + "\x00" + string(raw)
}
func localReport(ref objectRef) bool {
	return ref.Resource == inspectionResource && (ref.InspectionMode == "timeline" || ref.InspectionMode == "snapshot-diff" || ref.InspectionMode == "fleet-comparison" || ref.InspectionMode == "snapshot-file")
}
func (m *Model) showReport(ref objectRef, report inspection.Report) tea.Cmd {
	if m.localReports == nil {
		m.localReports = map[string]inspection.Report{}
	}
	if len(m.localReports) >= 32 {
		oldest := ""
		var at time.Time
		for key, r := range m.localReports {
			if oldest == "" || r.At.Before(at) {
				oldest = key
				at = r.At
			}
		}
		delete(m.localReports, oldest)
	}
	m.localReports[m.reportKey(ref)] = report
	return m.openObject(ref)
}

type reportExportedMsg struct {
	path string
	err  error
}

func (m *Model) exportInvestigation() tea.Cmd {
	obj := m.object.Get()
	if m.objectTarget.Resource != inspectionResource || obj == nil {
		return nil
	}
	// Export the report the person just inspected, not raw objects, logs or Secrets.
	document := append([]byte(nil), obj.Raw...)
	m.promptTitle = "Export visible investigation report"
	m.promptNote = "Contains the facts and messages visible in this report. Existing files are never overwritten."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue("correlux-investigation.json")
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		path := strings.TrimSpace(value)
		if path == "" {
			m.promptError = "Enter a file path"
			return nil
		}
		m.cancelPrompt()
		return func() tea.Msg {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return reportExportedMsg{path: path, err: err}
			}
			_, err = f.Write(document)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			return reportExportedMsg{path: path, err: err}
		}
	}
	return nil
}

func (m *Model) applyReportExported(msg reportExportedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Could not export report: "+msg.err.Error(), theme.StatusCritical)
	} else {
		m.notice("Exported investigation to "+msg.path, theme.StatusHealthy)
	}
	return m.expireNotice()
}
