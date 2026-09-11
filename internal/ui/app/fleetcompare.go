package app

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/fleet"
	"github.com/aronk11/correlux/internal/domain/inspection"
)

func fleetFingerprint(app *application.Application) string {
	var rows []string
	for i := range app.Pods {
		pod := &app.Pods[i]
		for _, container := range pod.Containers {
			raw, _ := json.Marshal(struct {
				Name, Image, ImageID string
				Requests, Limits     application.Amounts
			}{container.Name, container.Image, container.ImageID, container.Requests, container.Limits})
			rows = append(rows, string(raw))
		}
	}
	sort.Strings(rows)
	out := rows[:0]
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1] != row {
			out = append(out, row)
		}
	}
	return strings.Join(out, "\n")
}

func (m *Model) showFleetComparison() tea.Cmd {
	report := inspection.Report{Title: "Fleet application comparison", Summary: "Explicit matching rule: same application name and namespace across the loaded fleet group " + m.fleetGroupLabel(), At: time.Now(), Gaps: []string{"Compares observed container images, requests and limits, plus replica counts. This is not a complete desired-manifest comparison.", "Matching by namespace/name is a chosen convention, not proof of shared application identity. Use object comparison for an explicit mapping."}}
	type appKey struct{ namespace, name string }
	keys := map[appKey]bool{}
	for _, member := range m.fleetMembers {
		for i := range member.Applications {
			a := &member.Applications[i]
			keys[appKey{a.Namespace, a.Name}] = true
		}
	}
	sorted := make([]appKey, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].namespace != sorted[j].namespace {
			return sorted[i].namespace < sorted[j].namespace
		}
		return sorted[i].name < sorted[j].name
	})
	section := inspection.Section{Title: "Applications across clusters", Columns: []string{"Namespace", "Application", "Cluster", "State", "Ready/desired", "Observed images", "Compared with first observed"}, Empty: "no applications loaded; open and refresh the fleet first"}
	for _, key := range sorted {
		baseline := ""
		baselineReplicas := int32(0)
		baselineSet := false
		for _, member := range m.fleetMembers {
			if member.State != fleet.Ready {
				section.Add(key.namespace, key.name, member.Context, "unknown: "+member.State.String(), "—", "—", "not compared")
				continue
			}
			var app *application.Application
			for i := range member.Applications {
				if member.Applications[i].Name == key.name && member.Applications[i].Namespace == key.namespace {
					app = &member.Applications[i]
					break
				}
			}
			if app == nil {
				state := "not observed"
				if len(member.Gaps) > 0 {
					state = "unknown: partial read"
				}
				section.Add(key.namespace, key.name, member.Context, state, "—", "—", "not compared")
				continue
			}
			imageSet := map[string]bool{}
			for i := range app.Pods {
				for _, container := range app.Pods[i].Containers {
					label := container.Image
					if container.ImageID != "" {
						label += " [" + container.ImageID + "]"
					}
					imageSet[label] = true
				}
			}
			images := make([]string, 0, len(imageSet))
			for image := range imageSet {
				images = append(images, image)
			}
			sort.Strings(images)
			fingerprint := fleetFingerprint(app)
			comparison := "matches compared fields"
			switch {
			case len(images) == 0:
				comparison = "unknown: no container observations"
			case !baselineSet:
				baseline, baselineReplicas, baselineSet = fingerprint, app.DesiredPods, true
				comparison = "baseline"
			case baseline != fingerprint || baselineReplicas != app.DesiredPods:
				comparison = "different"
			}
			if len(member.Gaps) > 0 {
				comparison += "; partial evidence"
			}
			section.Add(key.namespace, key.name, member.Context, app.Health.String(), itoa(int(app.ReadyPods))+"/"+itoa(int(app.DesiredPods)), strings.Join(images, ", "), comparison)
		}
	}
	for _, member := range m.fleetMembers {
		if member.Err != nil {
			report.Gaps = append(report.Gaps, member.Context+": "+member.Err.Error())
		}
		for _, gap := range member.Gaps {
			report.Gaps = append(report.Gaps, member.Context+": "+gap.Kind+" "+gap.Reason)
		}
	}
	report.Sections = []inspection.Section{section}
	return m.showReport(objectRef{Kind: "Fleet", Name: m.fleetGroupLabel(), Resource: inspectionResource, InspectionMode: "fleet-comparison"}, report)
}
