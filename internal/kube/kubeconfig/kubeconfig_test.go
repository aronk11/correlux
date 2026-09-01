package kubeconfig

import (
	"path/filepath"
	"testing"
)

func load(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load(LoadOptions{ExplicitPath: filepath.Join("testdata", "kubeconfig.yaml")})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadContexts(t *testing.T) {
	cfg := load(t)

	if got, want := len(cfg.Contexts), 3; got != want {
		t.Fatalf("got %d contexts, want %d", got, want)
	}
	if cfg.CurrentContext != "staging" {
		t.Errorf("CurrentContext = %q, want staging", cfg.CurrentContext)
	}
	// Contexts must be sorted so the picker order is stable between runs.
	want := []string{"prod-eu", "sandbox", "staging"}
	for i, name := range cfg.Names() {
		if name != want[i] {
			t.Fatalf("Names() = %v, want %v", cfg.Names(), want)
		}
	}
}

func TestContextDetails(t *testing.T) {
	cfg := load(t)

	prod, ok := cfg.Context("prod-eu")
	if !ok {
		t.Fatal("prod-eu not found")
	}
	if prod.Namespace != "payments" {
		t.Errorf("namespace = %q, want payments", prod.Namespace)
	}
	if prod.Server != "https://api.prod-eu.example.com" {
		t.Errorf("server = %q", prod.Server)
	}
	if !prod.Production {
		t.Error("prod-eu must be classified as production")
	}

	staging, _ := cfg.Context("staging")
	if staging.Namespace != "default" {
		t.Errorf("a context without a namespace must default to %q, got %q", "default", staging.Namespace)
	}
	if staging.Production {
		t.Error("staging must not be classified as production")
	}
	if !staging.Current {
		t.Error("staging is current-context and must be marked as such")
	}
}

func TestProductionDetectedFromServerURL(t *testing.T) {
	cfg := load(t)

	// The context and cluster names are innocuous; only the server URL says
	// "live". Missing that is the expensive kind of mistake.
	sandbox, ok := cfg.Context("sandbox")
	if !ok {
		t.Fatal("sandbox not found")
	}
	if !sandbox.Production {
		t.Error("a context pointing at api.live.* must be classified as production")
	}
}

func TestResolveStartContext(t *testing.T) {
	cfg := load(t)

	tests := []struct {
		name       string
		requested  string
		configured string
		want       string
		wantErr    bool
	}{
		{name: "explicit request wins", requested: "prod-eu", configured: "sandbox", want: "prod-eu"},
		{name: "configured startup context", configured: "sandbox", want: "sandbox"},
		{name: "falls back to current-context", want: "staging"},
		{name: "unknown requested context errors", requested: "nope", wantErr: true},
		{name: "unknown configured context falls through", configured: "nope", want: "staging"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.ResolveStartContext(tc.requested, tc.configured)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveStartContext: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadMissingFileIsAnError(t *testing.T) {
	_, err := Load(LoadOptions{ExplicitPath: filepath.Join("testdata", "nope.yaml")})
	if err == nil {
		t.Fatal("an explicitly requested kubeconfig that does not exist must be an error")
	}
}
