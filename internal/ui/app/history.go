package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/theme"
)

type objectObservation struct {
	At       time.Time
	Document []byte
	State    string
}

const observedObjectLimit = 32
const observationLimit = 8
const observationBytes = 64 << 10

func (m *Model) observationKey(ref objectRef) string {
	resource := ref.lookup()
	if res, ok := m.resourceFor(ref); ok {
		resource = res.FullName()
	}
	return m.contextName + "\x00" + resource + "\x00" + ref.Namespace + "\x00" + ref.Name
}

func observedState(raw []byte) string {
	var doc struct {
		Status struct {
			Phase      string                                  `json:"phase"`
			Conditions []struct{ Type, Status, Reason string } `json:"conditions"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return "unknown"
	}
	state := doc.Status.Phase
	for _, c := range doc.Status.Conditions {
		if c.Type == "Ready" || c.Type == "Available" || c.Type == "Progressing" || c.Type == "Failed" || c.Type == "Complete" {
			if state != "" {
				state += "; "
			}
			state += c.Type + "=" + c.Status + " " + c.Reason
		}
	}
	return state
}

func (m *Model) observeObject(obj *resources.Object) {
	if obj == nil {
		return
	}
	if _, ok := m.resourceFor(m.objectTarget); !ok {
		return
	}
	normalized, err := inspection.Normalized(obj.Raw)
	if err != nil || len(normalized) > observationBytes {
		return
	}
	if m.observations == nil {
		m.observations = map[string][]objectObservation{}
	}
	key := m.observationKey(m.objectTarget)
	history := m.observations[key]
	observation := objectObservation{At: time.Now(), Document: normalized, State: observedState(obj.Raw)}
	if len(history) > 0 {
		previous := history[len(history)-1]
		if bytes.Equal(previous.Document, normalized) && previous.State == observation.State {
			return
		}
	}
	if len(history) == 0 && len(m.observations) >= observedObjectLimit {
		oldest := ""
		var at time.Time
		for key, entries := range m.observations {
			if len(entries) > 0 && (oldest == "" || entries[len(entries)-1].At.Before(at)) {
				oldest = key
				at = entries[len(entries)-1].At
			}
		}
		delete(m.observations, oldest)
	}
	history = append(history, observation)
	if len(history) > observationLimit {
		history = history[len(history)-observationLimit:]
	}
	m.observations[key] = history
}

func (m *Model) captureSnapshot() tea.Cmd {
	obj := m.object.Get()
	if obj == nil {
		return nil
	}
	normalized, err := inspection.Normalized(obj.Raw)
	if err != nil || len(normalized) > observationBytes {
		m.notice("Cannot capture this object: invalid document or normalized size exceeds 64 KiB", theme.StatusWarning)
		return m.expireNotice()
	}
	if m.snapshots == nil {
		m.snapshots = map[string]objectObservation{}
	}
	key := m.observationKey(m.objectTarget)
	if _, exists := m.snapshots[key]; !exists && len(m.snapshots) >= observedObjectLimit {
		m.notice("32 snapshots are already captured; start a new session to clear them", theme.StatusWarning)
		return m.expireNotice()
	}
	m.snapshots[key] = objectObservation{At: time.Now(), Document: normalized, State: observedState(obj.Raw)}
	m.notice("Captured configuration snapshot for "+m.objectTarget.label()+" in this session", theme.StatusHealthy)
	return m.expireNotice()
}

func (m *Model) showSnapshotDiff() tea.Cmd {
	ref := m.objectTarget
	baseline, ok := m.snapshots[m.observationKey(ref)]
	if !ok {
		m.notice("Capture a configuration snapshot for this object first", theme.StatusWarning)
		return m.expireNotice()
	}
	section, err := inspection.Comparison(baseline.Document, m.object.Get().Raw)
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	report := inspection.Report{Title: "Changes since snapshot of " + ref.label(), Summary: m.contextName + " / " + ref.Namespace + "; baseline " + baseline.At.Format(time.RFC3339) + "; compared with the currently loaded object", At: time.Now(), Sections: []inspection.Section{section}, Gaps: []string{"Status and known credential-bearing fields are excluded. Custom-resource credentials may require additional redaction before exporting."}}
	return m.showReport(inspectedRef(ref, "snapshot-diff"), report)
}

func (m *Model) showObservedChanges() tea.Cmd {
	ref := m.objectTarget
	history := m.observations[m.observationKey(ref)]
	report := inspection.Report{Title: "Observed changes to " + ref.label(), Summary: m.contextName + " / " + ref.Namespace + "; only reads made in this Correlux session", At: time.Now(), Gaps: []string{"Not an audit log. Changes between reads, deleted objects and actors are not reconstructed.", fmt.Sprintf("Retains at most %d observations per object and %d objects; normalized objects larger than 64 KiB are not recorded.", observationLimit, observedObjectLimit)}}
	timeline := inspection.Section{Title: "Observations", Columns: []string{"Observed at", "State", "Configuration"}, Empty: "no observations were retained"}
	for i, entry := range history {
		change := "first observation"
		if i > 0 {
			change = "unchanged"
			if !bytes.Equal(entry.Document, history[i-1].Document) {
				change = "configuration changed"
			}
		}
		timeline.Add(entry.At.Format(time.RFC3339), entry.State, change)
	}
	report.Sections = append(report.Sections, timeline)
	if len(history) > 1 {
		section, err := inspection.Comparison(history[0].Document, history[len(history)-1].Document)
		if err == nil {
			report.Sections = append(report.Sections, section)
		}
	}
	return m.showReport(inspectedRef(ref, "timeline"), report)
}
