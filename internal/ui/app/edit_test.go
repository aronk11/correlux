package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aronk11/kubeui/internal/kube/resources"
)

const editableYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: registry/payments:1.4
        name: payments
`

// openEditableObject puts a Deployment on the object screen, ready to edit.
func openEditableObject(t *testing.T, m *Model) {
	t.Helper()
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)
	press(t, m, "enter")
	loadObjectInto(m, &resources.Object{
		Target:          resources.Target{GVR: testCatalog().Resources[2].GVR, Namespaced: true},
		Kind:            "Deployment",
		Name:            "payments",
		Namespace:       "default",
		UID:             "dep-uid",
		ResourceVersion: "40213",
		YAML:            editableYAML,
	})
}

// edited simulates the user's editor: it writes the given document to the file
// kubeui prepared and reports the editor as having exited cleanly.
func edited(t *testing.T, m *Model, document string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "edited.yaml")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write edit buffer: %v", err)
	}
	m.editOriginal = editableYAML
	m.Update(editFinishedMsg{path: path})
}

func TestAnEditIsPreviewedBeforeItIsApplied(t *testing.T) {
	m := newTestModel(t)
	openEditableObject(t, m)

	edited(t, m, strings.ReplaceAll(editableYAML, "replicas: 3", "replicas: 5"))

	if m.overlay != overlayConfirm {
		t.Fatalf("an edit must be confirmed before it is sent, overlay = %v", m.overlay)
	}
	out := plainView(m)
	for _, want := range []string{
		"Apply your changes to Deployment/payments",
		"replaces 1 line with 1 line",
		"- ", "+ ", // the change itself, as a pair
		"replicas: 5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview must contain %q:\n%s", want, out)
		}
	}
}

func TestAnEditThatChangesNothingIsNotAnAction(t *testing.T) {
	m := newTestModel(t)
	openEditableObject(t, m)

	edited(t, m, editableYAML)

	if m.overlay == overlayConfirm {
		t.Fatal("there is nothing to confirm when nothing changed")
	}
	if out := plainView(m); !strings.Contains(out, "Nothing changed") {
		t.Errorf("the user must be told why nothing happened:\n%s", out)
	}
}

func TestAnEditThatRenamesTheObjectIsRefused(t *testing.T) {
	cases := []struct {
		name     string
		document string
		want     string
	}{
		{"renamed", strings.ReplaceAll(editableYAML, "name: payments", "name: payments-v2"), "renamed"},
		{"other kind", strings.ReplaceAll(editableYAML, "kind: Deployment", "kind: StatefulSet"), "kind"},
		{"moved", strings.ReplaceAll(editableYAML, "namespace: default", "namespace: other"), "namespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			openEditableObject(t, m)
			edited(t, m, tc.document)

			if m.overlay == overlayConfirm {
				t.Fatal("this is not an edit of the object on screen and must not be offered as one")
			}
			if out := plainView(m); !strings.Contains(out, tc.want) {
				t.Errorf("the refusal must say what is wrong, want %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestBrokenYAMLIsRefusedBeforeItIsSent(t *testing.T) {
	m := newTestModel(t)
	openEditableObject(t, m)

	edited(t, m, "spec:\n  replicas: [unclosed\n")

	if m.overlay == overlayConfirm {
		t.Fatal("a document that is not YAML must never reach the cluster")
	}
	if out := plainView(m); !strings.Contains(out, "not valid YAML") {
		t.Errorf("the parse failure must be reported:\n%s", out)
	}
}

func TestTheEditBufferIsRemovedAfterwards(t *testing.T) {
	m := newTestModel(t)
	openEditableObject(t, m)

	path := filepath.Join(t.TempDir(), "edited.yaml")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(editableYAML, "3", "5")), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m.editOriginal = editableYAML
	m.Update(editFinishedMsg{path: path})

	// A Kubernetes object routinely carries secrets; the copy must not be left
	// behind in the temp directory.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the edit buffer is still on disk at %s", path)
	}
}

func TestProductionDemandsTheClusterNameForAnEditToo(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.ContextName = "prod-eu" })
	openEditableObject(t, m)

	edited(t, m, strings.ReplaceAll(editableYAML, "replicas: 3", "replicas: 5"))

	if out := plainView(m); !strings.Contains(out, "Type prod-eu") {
		t.Errorf("every change to production is guarded the same way:\n%s", out)
	}
}

func TestEditorIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("KUBE_EDITOR", "")
	t.Setenv("EDITOR", "code --wait")

	editor, args := editorCommand()
	if editor != "code" || len(args) != 1 || args[0] != "--wait" {
		t.Errorf("editor = %q %v, want the arguments to survive", editor, args)
	}

	// KUBE_EDITOR wins, as it does for kubectl.
	t.Setenv("KUBE_EDITOR", "vim -f")
	if editor, _ := editorCommand(); editor != "vim" {
		t.Errorf("editor = %q, want KUBE_EDITOR to take precedence", editor)
	}
}

// Decoding is a way of reading, and reading must not change what an edit
// sends. A decoded Secret applied back to the cluster would store every value
// doubly encoded, and the application reading it would get nonsense out — the
// one way this feature could destroy data.
func TestAnEditSendsTheServersDocumentEvenWhileTheValuesAreDecoded(t *testing.T) {
	m := newTestModel(t)
	openSecret(t, m)

	press(t, m, "b")
	if out := plainView(m); !strings.Contains(out, "password: hunter2") {
		t.Fatalf("the decoded document must be on screen for this to prove anything:\n%s", out)
	}

	press(t, m, "e")
	path := m.editPath
	if path == "" {
		t.Fatal("e must have prepared a document for the editor")
	}
	buffer, err := os.ReadFile(path) //nolint:gosec // the path kubeui just wrote
	if err != nil {
		t.Fatalf("read the edit buffer: %v", err)
	}
	if string(buffer) != secretYAML {
		t.Errorf("the editor was handed:\n%s\nwant the document the server holds:\n%s", buffer, secretYAML)
	}

	// The editor gives back exactly what it was handed. Nothing changed, so
	// nothing is sent: the comparison is against the server's document too.
	m.Update(editFinishedMsg{path: path})
	if m.overlay == overlayConfirm {
		t.Fatal("an untouched document is not a change and must not be offered as one")
	}
	if out := plainView(m); !strings.Contains(out, "Nothing changed") {
		t.Errorf("the edit was measured against something other than the stored document:\n%s", out)
	}
}

func TestEditingRequiresTheObjectToBeOpen(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, scalableCatalog())
	openWorkload(t, m)

	// On the application screen, with nothing fetched yet.
	m.editObject(objectRef{Kind: "Deployment", Name: "payments", Namespace: "default"})
	if out := plainView(m); !strings.Contains(out, "first") {
		t.Errorf("kubeui must not edit a document it has not read:\n%s", out)
	}
}
