package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/kube/resources"
)

func TestHelmNavigationKeepsNamespaceAndRevision(t *testing.T) {
	m := newTestModel(t)
	m.view = viewObject
	m.objectTarget = helmRef("releases", "", "list")
	loadObjectInto(m, &resources.Object{Raw: []byte(`[{"name":"api","namespace":"production","revision":"5","status":"deployed","chart":"api-1.0"}]`)})
	d, targets := m.objectView()
	if len(targets) != 1 || targets[0].Namespace != "production" || targets[0].HelmMode != "status" {
		t.Fatalf("wrong release targets: %+v %+v", d, targets)
	}
	m.objectTarget = helmRef("api", "production", "history")
	loadObjectInto(m, &resources.Object{Raw: []byte(`[{"revision":3,"status":"superseded","chart":"api-0.9"}]`)})
	_, targets = m.objectView()
	if len(targets) != 1 || targets[0].HelmRevision != 3 || targets[0].Namespace != "production" {
		t.Fatalf("wrong history target: %+v", targets)
	}
	if got := strings.Join(helmReadArgs(targets[0]), " "); got != "status api --output json --revision 3" {
		t.Fatal(got)
	}
}

func TestHelmScopeAndLifecycleGate(t *testing.T) {
	ref := helmRef("releases", "", "list")
	if !strings.Contains(strings.Join(helmReadArgs(ref), " "), "--all-namespaces") {
		t.Fatal("all namespace scope lost")
	}
	ref.Namespace = "team"
	if strings.Contains(strings.Join(helmReadArgs(ref), " "), "--all-namespaces") {
		t.Fatal("scope widened")
	}
	m := newTestModel(t)
	ref = helmRef("api", "team", "status")
	if cmd := m.confirmHelm(ref, "uninstall", 0); cmd != nil {
		t.Fatal("uninstall ran before consent")
	}
	if m.pending == nil || !m.pending.Danger || !strings.Contains(strings.Join(m.pending.Lines, " "), "workloads will stop") {
		t.Fatal("missing uninstall confirmation")
	}
}

func TestHistoricalHelmViewsKeepTheSelectedRevision(t *testing.T) {
	for _, mode := range []string{"values", "computed-values", "manifest", "hooks", "notes"} {
		ref := helmRef("api", "team", mode)
		ref.HelmRevision = 4
		if got := strings.Join(helmReadArgs(ref), " "); !strings.HasSuffix(got, "--revision 4") {
			t.Fatalf("%s lost revision: %s", mode, got)
		}
	}
}

func TestHelmManifestNavigationUsesExplicitIdentities(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	raw := []byte(`{"manifest":"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: team\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: api\n"}`)
	var refs []objectRef
	section := m.helmManifestSection(raw, "team", func(ref objectRef) int { refs = append(refs, ref); return len(refs) - 1 })
	if len(section.Rows) != 2 || len(refs) != 2 || refs[0].Namespace != "team" || refs[0].Resource != "deployments.apps" || refs[1].Namespace != "team" {
		t.Fatalf("incorrect manifest navigation: %+v", refs)
	}
}

func TestHelmValuesAreValidatedBeforeConfirmationAndCleanedUp(t *testing.T) {
	for _, tt := range []struct {
		values string
		valid  bool
	}{{"replicas: 3\n", true}, {"replicas: 1\nreplicas: 2\n", false}, {"- invalid\n", false}} {
		t.Run(tt.values, func(t *testing.T) {
			m := newTestModel(t)
			ref := helmRef("api", "team", "status")
			m.view = viewObject
			m.objectTarget = ref
			path := filepath.Join(t.TempDir(), "values.yaml")
			if err := os.WriteFile(path, []byte(tt.values), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := m.applyHelmValuesEdited(&helmValuesEditedMsg{draft: helmValuesLoadedMsg{cluster: m.contextName, ref: ref, values: []byte("replicas: 1\n"), args: []string{"upgrade", "api", "example/api"}}, path: path})
			if tt.valid && (cmd != nil || m.pending == nil || len(m.pending.Diff) == 0) {
				t.Fatal("valid edit did not enter review gate")
			}
			if !tt.valid && m.pending != nil {
				t.Fatal("invalid values reached confirmation")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("values file left on disk")
			}
		})
	}
}
