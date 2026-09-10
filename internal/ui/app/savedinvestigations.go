package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const paletteSaved palette.ActionID = "investigation.saved"

func (m *Model) savedCommands() []palette.Command {
	out := make([]palette.Command, 0, 1+2*len(m.cfg.SavedInvestigations))
	out = append(out, palette.Command{ID: "investigation.save", Action: paletteSaved, Arg: "save", Title: "Save current investigation", Category: "Investigate", Enabled: true})
	for i := range m.cfg.SavedInvestigations {
		saved := &m.cfg.SavedInvestigations[i]
		out = append(out, palette.Command{ID: "investigation.open." + saved.Name, Action: paletteSaved, Arg: "open:" + saved.Name, Title: "Open investigation " + saved.Name, Subtitle: saved.Context + " / " + saved.Namespace, Category: "Investigate", Enabled: true}, palette.Command{ID: "investigation.delete." + saved.Name, Action: paletteSaved, Arg: "delete:" + saved.Name, Title: "Remove saved investigation " + saved.Name, Category: "Investigate", Enabled: true})
	}
	return out
}

func (m *Model) currentInvestigation() config.SavedInvestigation {
	saved := config.SavedInvestigation{Context: m.contextName, Namespace: m.namespace, AllNamespaces: m.allNamespaces, View: "applications", Filter: m.search.Value()}
	switch m.view {
	case viewFleet, viewFleetResource:
		saved.View = "fleet"
		saved.FleetGroup = m.fleetGroupLabel()
	case viewTable:
		saved.View = "table"
		saved.Resource = m.resource.FullName()
	case viewApplication, viewWhy:
		saved.View = "application"
		saved.Application = m.selectedApp
	case viewObject:
		if m.objectTarget.InspectionMode == "fleet-comparison" {
			saved.View = "fleet"
			saved.FleetGroup = m.fleetGroupLabel()
			return saved
		}
		ref := m.objectTarget
		if localReport(ref) {
			ref = ref.subject()
		}
		saved.View = "object"
		saved.Kind = ref.Kind
		saved.Object = ref.Name
		saved.Namespace = ref.Namespace
		saved.Resource = ref.Resource
		saved.Mode = ref.InspectionMode
		saved.SubjectResource = ref.SubjectResource
		saved.SourceNamespace = ref.SourceNamespace
		saved.SourcePod = ref.SourcePod
		saved.TargetContext = ref.CompareContext
		saved.TargetNamespace = ref.CompareNamespace
		saved.TargetName = ref.CompareName
		// Helm/debug session views are reopenable, but revision pagination is not a saved filter.
		if ref.Resource == helmResource {
			saved.Mode = ref.HelmMode
		}
	}
	return saved
}

func (m *Model) openSaved(action string) tea.Cmd {
	if action == "save" {
		saved := m.currentInvestigation()
		m.promptTitle = "Name this investigation"
		m.promptNote = "Saves cluster, scope and navigation in your local configuration; no cluster data is saved."
		m.promptRef = objectRef{}
		m.promptError = ""
		m.promptInput.SetValue("")
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			name := strings.TrimSpace(value)
			if name == "" {
				m.promptError = "Enter a name"
				return nil
			}
			for i := range m.cfg.SavedInvestigations {
				s := &m.cfg.SavedInvestigations[i]
				if s.Name == name {
					m.promptError = "That name already exists"
					return nil
				}
			}
			saved.Name = name
			updated := append(append([]config.SavedInvestigation(nil), m.cfg.SavedInvestigations...), saved)
			if err := m.writeInvestigations(updated); err != nil {
				m.promptError = err.Error()
				return nil
			}
			m.cancelPrompt()
			m.rebuildCommands()
			m.notice("Saved investigation "+name, theme.StatusHealthy)
			return m.expireNotice()
		}
		return nil
	}
	operation, name, _ := strings.Cut(action, ":")
	for i := range m.cfg.SavedInvestigations {
		saved := &m.cfg.SavedInvestigations[i]
		if saved.Name != name {
			continue
		}
		if operation == "delete" {
			updated := append([]config.SavedInvestigation(nil), m.cfg.SavedInvestigations...)
			updated = append(updated[:i], updated[i+1:]...)
			if err := m.writeInvestigations(updated); err != nil {
				m.notice(err.Error(), theme.StatusCritical)
				return m.expireNotice()
			}
			m.rebuildCommands()
			return nil
		}
		return m.restoreInvestigation(saved)
	}
	return nil
}

func (m *Model) writeInvestigations(saved []config.SavedInvestigation) error {
	path := m.cfg.SourcePath
	if path == "" {
		var err error
		path, err = config.Path()
		if err != nil {
			return err
		}
	}
	if err := config.SaveInvestigations(path, saved); err != nil {
		return err
	}
	m.cfg.SavedInvestigations = saved
	m.cfg.SourcePath = path
	return nil
}

func (m *Model) restoreInvestigation(saved *config.SavedInvestigation) tea.Cmd {
	if saved.View == "fleet" {
		m.restorePending = nil
		return m.switchFleetGroup(saved.FleetGroup)
	}
	if _, ok := m.kubeconfig.Context(saved.Context); !ok {
		m.notice("Saved cluster "+saved.Context+" is not in kubeconfig", theme.StatusWarning)
		return m.expireNotice()
	}
	cmd := m.switchContextScoped(saved.Context, saved.Namespace)
	m.namespace = saved.Namespace
	m.allNamespaces = saved.AllNamespaces
	reload := m.reloadScopedViews()
	m.view = viewApplications
	copy := *saved
	m.restorePending = &copy
	return tea.Batch(cmd, reload, m.tryRestoreInvestigation())
}

func (m *Model) tryRestoreInvestigation() tea.Cmd {
	saved := m.restorePending
	if saved == nil {
		return nil
	}
	if m.contextName != saved.Context || m.namespace != saved.Namespace || m.allNamespaces != saved.AllNamespaces || m.view != viewApplications {
		m.restorePending = nil
		return nil
	}
	if m.catalog.State() != async.Ready || m.apps.State() != async.Ready {
		return nil
	}
	m.restorePending = nil
	var cmd tea.Cmd
	switch saved.View {
	case "table":
		cmd = m.openResource(saved.Resource)
	case "application":
		cmd = m.openApplication(saved.Application)
	case "object":
		ref := objectRef{Kind: saved.Kind, Name: saved.Object, Namespace: saved.Namespace, Resource: saved.Resource, InspectionMode: saved.Mode, SubjectResource: saved.SubjectResource, SourceNamespace: saved.SourceNamespace, SourcePod: saved.SourcePod, CompareContext: saved.TargetContext, CompareNamespace: saved.TargetNamespace, CompareName: saved.TargetName}
		if ref.Resource == helmResource {
			ref.InspectionMode = ""
			ref.HelmMode = saved.Mode
		}
		cmd = m.openObject(ref)
	default:
		cmd = m.backToApplications()
	}
	m.search.SetValue(saved.Filter)
	m.rebuildCommands()
	return cmd
}
