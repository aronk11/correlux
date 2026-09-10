package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func (m *Model) previewSnapshotFile() tea.Cmd {
	ref := m.objectTarget
	baseline, ok := m.snapshots[m.observationKey(ref)]
	if !ok {
		m.notice("Capture a configuration snapshot first", theme.StatusWarning)
		return m.expireNotice()
	}
	target := inspectRef(ref)
	if res, ok := m.resourceFor(ref); ok {
		target.Resource = res.FullName()
	}
	snapshot := &inspection.Snapshot{Version: 1, Cluster: m.contextName, Ref: target, At: baseline.At, Document: baseline.Document}
	section := inspection.Section{Title: "Snapshot projection", Columns: []string{"Content"}}
	for _, line := range strings.Split(string(baseline.Document), "\n") {
		section.Add(line)
	}
	report := inspection.Report{Title: "Snapshot of " + ref.label(), Summary: "Review this projection, then choose Export the visible investigation report", At: time.Now(), Snapshot: snapshot, Sections: []inspection.Section{section}, Gaps: []string{"Known credential fields are redacted; inspect custom fields for secrets before exporting. This file can later be loaded with Compare with a saved snapshot file."}}
	return m.showReport(inspectedRef(ref, "snapshot-file"), report)
}

type snapshotReadMsg struct {
	cluster  string
	ref      objectRef
	snapshot *inspection.Snapshot
	err      error
}

func (m *Model) compareSnapshotFile() tea.Cmd {
	ref, cluster := m.objectTarget, m.contextName
	m.promptTitle = "Compare with saved snapshot file"
	m.promptNote = "Loads a Correlux snapshot report and compares its redacted projection with this loaded object."
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
			snapshot, err := readSnapshotFile(path)
			return snapshotReadMsg{cluster: cluster, ref: ref, snapshot: snapshot, err: err}
		}
	}
	return nil
}

func readSnapshotFile(path string) (*inspection.Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 2<<20 {
		return nil, errors.New("snapshot report exceeds 2 MiB")
	}
	var report inspection.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	if report.Snapshot == nil || report.Snapshot.Version != 1 || len(report.Snapshot.Document) > observationBytes || !json.Valid(report.Snapshot.Document) {
		return nil, errors.New("not a supported Correlux snapshot report")
	}
	return report.Snapshot, nil
}

func (m *Model) applySnapshotRead(msg *snapshotReadMsg) tea.Cmd {
	if msg.cluster != m.contextName || msg.ref != m.objectTarget || m.view != viewObject || m.object.Get() == nil {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not read snapshot: "+msg.err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	snapshot := msg.snapshot
	res, ok := m.resourceFor(msg.ref)
	if !ok || snapshot.Ref.Kind != msg.ref.Kind || snapshot.Ref.Resource != res.FullName() {
		m.notice("Snapshot kind/API group does not match this object", theme.StatusWarning)
		return m.expireNotice()
	}
	section, err := inspection.Comparison(snapshot.Document, m.object.Get().Raw)
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	report := inspection.Report{Title: "Compare with saved snapshot", Summary: snapshot.Cluster + " / " + snapshot.Ref.Namespace + " / " + snapshot.Ref.Name + " at " + snapshot.At.Format(time.RFC3339) + " → " + m.contextName + " / " + msg.ref.Namespace + " / " + msg.ref.Name, At: time.Now(), Sections: []inspection.Section{section}, Gaps: []string{"Compares normalized configuration only. Status and redacted fields are not compared."}}
	return m.showReport(inspectedRef(msg.ref, "snapshot-diff"), report)
}
