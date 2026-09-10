package config

import (
	"os"
	"path/filepath"
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
	if len(cfg.SavedInvestigations) != 1 || cfg.SavedInvestigations[0] != saved[0] {
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
