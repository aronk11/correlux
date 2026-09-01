package kubeconfig

import "testing"

func TestDefaultClassifier(t *testing.T) {
	c := DefaultClassifier()

	tests := []struct {
		name    string
		context string
		cluster string
		server  string
		want    bool
	}{
		{"plain prod", "prod", "", "", true},
		{"prefixed", "prod-eu", "", "", true},
		{"suffixed", "eu-prod", "", "", true},
		{"dotted", "eu.production.acme", "", "", true},
		{"abbreviated", "prd-01", "", "", true},
		{"live", "live-cluster", "", "", true},
		{"from cluster name", "eu", "prod-eu", "", true},
		{"from server url", "eu", "eu", "https://api.prod.example.com", true},
		{"staging is not production", "staging", "", "", false},
		{"substring must not match", "reproducer", "", "", false},
		{"product is not production", "product-team", "", "", false},
		{"development", "dev", "", "", false},
		{"empty", "", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.IsProduction(tc.context, tc.cluster, tc.server); got != tc.want {
				t.Errorf("IsProduction(%q, %q, %q) = %v, want %v",
					tc.context, tc.cluster, tc.server, got, tc.want)
			}
		})
	}
}

func TestExplicitContextsAreAlwaysProduction(t *testing.T) {
	c, errs := NewClassifier(nil, []string{"customer-cluster"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !c.IsProduction("customer-cluster", "", "") {
		t.Error("an explicitly listed context must be production")
	}
	if c.IsProduction("other", "", "") {
		t.Error("unlisted contexts must not become production")
	}
}

func TestInvalidPatternIsReportedButOthersStillWork(t *testing.T) {
	c, errs := NewClassifier([]string{"[unclosed", "prod"}, nil)
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
	if !c.IsProduction("prod-eu", "", "") {
		t.Error("a valid pattern must still apply when another one is broken")
	}
}

func TestNilClassifierIsSafe(t *testing.T) {
	var c *Classifier
	if c.IsProduction("prod", "", "") {
		t.Error("a nil classifier must not claim production")
	}
}
