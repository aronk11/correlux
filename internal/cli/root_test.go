package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aronk11/correlux/internal/config"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
	"github.com/aronk11/correlux/internal/kube/kubeconfig"
)

func TestAirGappedStartupPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name, config string
		flag, want   bool
	}{
		{"default", "", false, false},
		{"flag", "", true, true},
		{"config", "airGapped: true\nupdate:\n  check: true\n", false, true},
		{"flag overrides config", "airGapped: false\n", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := prepare(globalFlags{configPath: path, kubeconfig: "../ui/app/testdata/kubeconfig.yaml", airGapped: tt.flag})
			if err != nil {
				t.Fatal(err)
			}
			if s.cfg.AirGapped != tt.want {
				t.Fatalf("air-gapped = %v, want %v", s.cfg.AirGapped, tt.want)
			}
		})
	}
}

func TestInvalidConfigCannotFallBackToOnlineStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("airGapped: true\nunknownSetting: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := prepare(globalFlags{configPath: path, kubeconfig: "../ui/app/testdata/kubeconfig.yaml"}); err == nil || s != nil {
		t.Fatal("invalid configuration silently enabled online defaults")
	}
}

func TestReadOnlyProductionLocksWhatTheHeaderCallsProduction(t *testing.T) {
	classifier := kubeconfig.DefaultClassifier()
	readOnly := readOnlyContexts(config.Safety{ReadOnlyProduction: true}, classifier)

	if !readOnly(kubeclient.Identity{Context: "eu-1", Cluster: "prod-eu", Server: "https://api.example.com"}) {
		t.Error("a context whose cluster is named prod is production, and must be locked")
	}
	if readOnly(kubeclient.Identity{Context: "staging", Cluster: "staging", Server: "https://api.staging.example.com"}) {
		t.Error("staging is not production and must stay writable")
	}
}
