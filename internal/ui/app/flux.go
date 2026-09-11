package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/gitops"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const paletteFlux palette.ActionID = "flux.action"

func (m *Model) fluxCommands() []palette.Command {
	var commands []palette.Command
	if cat := m.catalog.Get(); cat != nil {
		for _, res := range cat.Resources {
			if !gitops.Identifies(res.GroupVersion(), res.Kind()) {
				continue
			}
			commands = append(commands, palette.Command{ID: "flux.browse." + res.FullName(), Action: paletteOpenResource, Arg: res.FullName(), Title: "Flux: browse " + res.Kind(), Category: "Delivery", Keywords: []string{"gitops", "flux", "source", "reconciliation"}, Enabled: true})
		}
	}
	if m.view != viewObject || !m.object.HasValue() {
		return commands
	}
	obj := m.object.Get()
	if obj == nil {
		return commands
	}
	for _, action := range gitops.Actions(obj.Raw) {
		commands = append(commands, palette.Command{ID: "flux." + action, Action: paletteFlux, Arg: action, Title: fluxTitle(action), Subtitle: m.objectTarget.label(), Category: "Delivery", Keywords: []string{"flux", "gitops", "sync"}, Enabled: true})
	}
	return commands
}

func fluxTitle(action string) string {
	switch action {
	case "reconcile":
		return "Flux: reconcile now"
	case "suspend":
		return "Flux: suspend reconciliation"
	case "resume":
		return "Flux: resume reconciliation"
	case "reset":
		return "Flux: reset Helm failure retries and reconcile"
	case "force":
		return "Flux: force Helm install or upgrade"
	}
	return "Flux: " + action
}

func (m *Model) confirmFlux(action string) tea.Cmd {
	if m.view != viewObject || m.object.Get() == nil {
		return nil
	}
	ref, obj := m.objectTarget, m.object.Get()
	patch, err := gitops.Patch(obj.Raw, action, time.Now())
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	consequence := "Requests reconciliation; controller completion is reported in the object's conditions."
	switch action {
	case "suspend":
		consequence = "Pauses future reconciliation. Workloads keep running; an in-progress reconciliation may finish."
	case "resume":
		consequence = "Resumes reconciliation; Flux may apply changes to managed resources."
	case "reset":
		consequence = "Resets Helm failure counters and requests another reconciliation attempt."
	case "force":
		consequence = "Forces a Helm install or upgrade even without a spec change; workloads may be replaced."
	}
	cluster := m.contextName
	return m.confirm(pendingAction{Title: fluxTitle(action), Lines: []string{consequence, ref.label() + " in " + ref.Namespace, "A GitOps parent may restore fields from its source."}, Challenge: m.productionChallenge(), Danger: action == "force" || action == "suspend", Run: func(m *Model) tea.Cmd {
		if m.contextName != cluster {
			return nil
		}
		res, ok := m.resourceFor(ref)
		if !ok {
			return nil
		}
		factory := m.factory
		m.notice(strings.TrimPrefix(fluxTitle(action), "Flux: ")+"…", theme.StatusUnknown)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
			defer cancel()
			obj, err := factory.PatchObject(ctx, cluster, res, ref.Namespace, ref.Name, patch)
			return editedMsg{ref: ref, object: obj, err: err}
		}
	}})
}

// Only link release identity reported by Flux; default release-name generation
// and remote kubeConfig targets must never be guessed.
func fluxHelmReleaseRef(raw []byte) (objectRef, bool) {
	var doc struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			StorageNamespace string          `json:"storageNamespace"`
			KubeConfig       json.RawMessage `json:"kubeConfig"`
		} `json:"spec"`
		Status struct {
			StorageNamespace string `json:"storageNamespace"`
			History          []struct {
				Name string `json:"name"`
			} `json:"history"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Kind != "HelmRelease" || !gitops.Identifies(doc.APIVersion, doc.Kind) || len(doc.Spec.KubeConfig) > 0 || len(doc.Status.History) == 0 || doc.Status.History[0].Name == "" {
		return objectRef{}, false
	}
	ns := doc.Status.StorageNamespace
	if ns == "" {
		ns = doc.Spec.StorageNamespace
	}
	if ns == "" {
		ns = doc.Metadata.Namespace
	}
	return helmRef(doc.Status.History[0].Name, ns, "status"), true
}
