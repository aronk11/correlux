package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing config must not be an error, got %v", err)
	}
	if cfg.Theme != ThemeAuto {
		t.Errorf("theme = %q, want %q", cfg.Theme, ThemeAuto)
	}
	if !cfg.Safety.ProductionConfirmation {
		t.Error("production confirmation must default to on")
	}
	if len(cfg.Safety.ProductionPatterns) == 0 {
		t.Error("default production patterns must be present")
	}
}

func TestLoadValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, `
theme: light
startup:
  context: prod-eu
  namespace: payments
dangerousActions:
  productionConfirmation: false
  productionContexts:
    - special-cluster
keybindings:
  palette: ctrl+space
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != ThemeLight {
		t.Errorf("theme = %q, want light", cfg.Theme)
	}
	if cfg.Startup.Context != "prod-eu" || cfg.Startup.Namespace != "payments" {
		t.Errorf("startup = %+v", cfg.Startup)
	}
	if cfg.Safety.ProductionConfirmation {
		t.Error("productionConfirmation: false must be honoured")
	}
	if got := cfg.Safety.ProductionContexts; len(got) != 1 || got[0] != "special-cluster" {
		t.Errorf("productionContexts = %v", got)
	}
	if cfg.Keybindings["palette"] != "ctrl+space" {
		t.Errorf("keybindings = %v", cfg.Keybindings)
	}
	if cfg.SourcePath != path {
		t.Errorf("SourcePath = %q, want %q", cfg.SourcePath, path)
	}
}

func TestLoadMalformedFileReportsErrorAndKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, "theme: [not, a, string\n")

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("malformed config must report an error")
	}
	if cfg.Theme != ThemeAuto {
		t.Errorf("defaults must survive a parse failure, got theme %q", cfg.Theme)
	}
}

func TestLoadUnknownFieldIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, "theme: dark\nnotAField: 3\n")

	if _, err := Load(path); err == nil {
		t.Fatal("an unknown field must be reported, so typos are not silently ignored")
	}
}

func TestUnknownThemeFallsBackToAuto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, "theme: neon\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != ThemeAuto {
		t.Errorf("theme = %q, want auto", cfg.Theme)
	}
}

func TestDirHonoursOverride(t *testing.T) {
	t.Setenv("KUBEUI_CONFIG_DIR", filepath.Join("custom", "kubeui"))
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != filepath.Join("custom", "kubeui") {
		t.Errorf("Dir() = %q", dir)
	}
}

func TestDirUsesOSAppropriateLocation(t *testing.T) {
	t.Setenv("KUBEUI_CONFIG_DIR", "")
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join("C:", "Users", "test", "AppData", "Roaming"))
	} else {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.TempDir(), "xdg"))
	}

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if filepath.Base(dir) != "kubeui" {
		t.Errorf("Dir() = %q, want a kubeui directory", dir)
	}
	if runtime.GOOS != "windows" && dir != filepath.Join(os.TempDir(), "xdg", "kubeui") {
		t.Errorf("XDG_CONFIG_HOME must be honoured, got %q", dir)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestRefreshInterval(t *testing.T) {
	cases := []struct {
		name    string
		every   string
		want    time.Duration
		wantErr bool
	}{
		{name: "unset falls back to the default", want: DefaultRefreshInterval},
		{name: "a plain duration", every: "30s", want: 30 * time.Second},
		{name: "below the floor is raised", every: "50ms", want: MinRefreshInterval},
		{name: "nonsense is reported", every: "soon", want: DefaultRefreshInterval, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Refresh{Every: tc.every}.Interval()
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("interval = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheFleetIsEmptyUntilItIsNamed(t *testing.T) {
	if len(Default().Fleet) != 0 {
		t.Error("kubeui must not fan out to every context on its own")
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("fleet:\n  - prod-eu\n  - prod-us\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Fleet) != 2 || cfg.Fleet[0] != "prod-eu" {
		t.Errorf("fleet = %v, want the two named contexts", cfg.Fleet)
	}
}
