package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSavingTheFleetLeavesTheRestOfTheFileAlone is the whole point of editing
// the node tree rather than re-marshalling the struct. Somebody who wrote a
// configuration by hand, with comments explaining why, must get that file back
// — not a machine-generated equivalent of it.
func TestSavingTheFleetLeavesTheRestOfTheFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `# Correlux, as configured for the platform team.
theme: dark

startup:
  # We always land in payments; that is where the pager points.
  namespace: payments

fleet: [prod-eu]

refresh:
  every: 5s # the API server is behind a slow VPN
`
	write(t, path, original)

	if err := SaveFleet(path, nil, []FleetGroup{
		{Name: "production", Contexts: []string{"prod-eu", "prod-us"}},
	}); err != nil {
		t.Fatalf("SaveFleet: %v", err)
	}

	saved := read(t, path)
	for _, want := range []string{
		"# Correlux, as configured for the platform team.",
		"# We always land in payments; that is where the pager points.",
		"every: 5s # the API server is behind a slow VPN",
		"theme: dark",
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("the save lost %q:\n%s", want, saved)
		}
	}
	if strings.Contains(saved, "prod-eu\n") && strings.Contains(saved, "fleet: ") {
		t.Errorf("the emptied fleet key should be gone, not written as empty:\n%s", saved)
	}

	// And it still parses back into what was asked for.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if len(cfg.FleetGroups) != 1 || cfg.FleetGroups[0].Name != "production" ||
		len(cfg.FleetGroups[0].Contexts) != 2 {
		t.Errorf("groups = %#v", cfg.FleetGroups)
	}
	if cfg.Theme != ThemeDark || cfg.Startup.Namespace != "payments" {
		t.Errorf("unrelated settings changed: theme=%q namespace=%q", cfg.Theme, cfg.Startup.Namespace)
	}
	if len(cfg.Fleet) != 0 {
		t.Errorf("fleet = %v, want it cleared", cfg.Fleet)
	}
}

// TestSavingWithNoConfigFileYetWritesOne: choosing clusters is often the first
// thing somebody does, and it must not require having written a file first.
func TestSavingWithNoConfigFileYetWritesOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	if err := SaveFleet(path, []string{"kind-correlux"}, nil); err != nil {
		t.Fatalf("SaveFleet: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Fleet) != 1 || cfg.Fleet[0] != "kind-correlux" {
		t.Errorf("fleet = %v", cfg.Fleet)
	}

	// Windows has no Unix permission bits to check; the chmod there is a
	// no-op and the file inherits the directory's ACL.
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600: the file names clusters", perm)
	}
}

// TestSavingRefusesAFileItDoesNotUnderstand. A top level that is not a mapping
// is a malformed config; replacing it with a valid one would throw away
// whatever the user meant to write.
func TestSavingRefusesAFileItDoesNotUnderstand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, "- this\n- is\n- a list\n")

	if err := SaveFleet(path, []string{"prod"}, nil); err == nil {
		t.Fatal("a malformed configuration must not be silently replaced")
	}
	if got := read(t, path); !strings.Contains(got, "a list") {
		t.Errorf("the file was overwritten anyway:\n%s", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}
