package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSavedInvestigationsPreserveConfigAndRoundTripScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("# keep this\nfleet: [staging]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	saved := []SavedInvestigation{{Name: "routing", Context: "staging", Namespace: "team", View: "object", Resource: "inspection.correlux.internal", Mode: "network", SourceNamespace: "source", SourcePod: "client", AllNamespaces: true}}
	if err := SaveInvestigations(path, saved); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SavedInvestigations) != 1 || !reflect.DeepEqual(cfg.SavedInvestigations[0], saved[0]) {
		t.Fatalf("lost scope: %+v", cfg.SavedInvestigations)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# keep this") || !strings.Contains(string(raw), "fleet: [staging]") {
		t.Fatal(string(raw))
	}
	if err = SaveInvestigations(path, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil || len(cfg.SavedInvestigations) != 0 || len(cfg.Fleet) != 1 {
		t.Fatalf("delete affected config: %+v %v", cfg, err)
	}
}

func TestSavedInvestigationPanesRoundTripAndAreOmittedWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	saved := []SavedInvestigation{
		{Name: "split", Context: "staging", Namespace: "team", View: "application", Application: "web", Panes: []string{"why", "logs"}, Split: "below"},
		{Name: "plain", Context: "staging", Namespace: "team", View: "applications"},
	}
	if err := SaveInvestigations(path, saved); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SavedInvestigations
	if len(got) != 2 || !reflect.DeepEqual(got[0], saved[0]) || got[1].Panes != nil || got[1].Split != "" {
		t.Fatalf("layout lost: %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "panes:"); n != 1 {
		t.Fatalf("panes written %d times, want once:\n%s", n, raw)
	}
	if n := strings.Count(string(raw), "split:"); n != 1 {
		t.Fatalf("split written %d times, want once:\n%s", n, raw)
	}
}
