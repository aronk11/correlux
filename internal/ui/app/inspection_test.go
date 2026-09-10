package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/kube/resources"
)

func TestObservationsBoundedScopedAndCanonical(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.view = viewObject
	ref := objectRef{Kind: "Pod", Name: "api", Namespace: "team"}
	m.objectTarget = ref
	for i := 0; i < 20; i++ {
		raw, _ := json.Marshal(map[string]any{"spec": map[string]any{"revision": i}})
		m.observeObject(&resources.Object{Raw: raw})
	}
	key := m.observationKey(ref)
	if len(m.observations[key]) != observationLimit {
		t.Fatal("history was not bounded")
	}
	ref.Resource = "pods"
	if m.observationKey(ref) != key {
		t.Fatal("navigation alias lost history")
	}
	m.contextName = "production"
	if len(m.observations[m.observationKey(ref)]) != 0 {
		t.Fatal("history leaked across clusters")
	}
}

func TestLocalReportsRemainReadableAfterReopening(t *testing.T) {
	m := newTestModel(t)
	ref := inspectedRef(objectRef{Kind: "Pod", Name: "api", Namespace: "team"}, "timeline")
	m.showReport(ref, inspection.Report{Title: "Observed changes", At: time.Now()})
	if m.object.Get() == nil || m.objectLoading {
		t.Fatal("local report stuck loading")
	}
	m.object.Reset()
	m.loadObject()
	if m.object.Get() == nil || m.objectLoading {
		t.Fatal("reopened report lost")
	}
	m.contextName = "production"
	m.object.Reset()
	m.loadObject()
	if m.object.Get() != nil {
		t.Fatal("local report crossed contexts")
	}
}

func TestSnapshotFilesValidateVersionSizeAndDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	report := inspection.Report{Snapshot: &inspection.Snapshot{Version: 1, Document: json.RawMessage(`{"spec":{"replicas":2}}`)}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readSnapshotFile(path); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte(`{"snapshot":{"version":2,"document":{}}}`), []byte(`{}`), make([]byte, (2<<20)+1)} {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = readSnapshotFile(path); err == nil {
			t.Fatal("accepted invalid snapshot")
		}
	}
}
