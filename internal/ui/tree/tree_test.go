package tree

import (
	"strings"
	"testing"
)

const kanzlei = `apiVersion: tenancy.kanzleisuite.de/v1alpha1
kind: Kanzlei
metadata:
  name: frisch
  finalizers: [tenancy.kanzleisuite.de/cleanup]
  managedFields:
  - manager: kubectl
    operation: Apply
status:
  connector:
    clientSecret: kanzleisuite-connector-cert
  dbInit:
    job: db-init-frisch-82cabad4
    succeeded: true
  observedGeneration: 1
  conditions:
  - type: Ready
    status: "True"
spec:
  note: |
    first
    second
`

func paths(nodes []*Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Path
	}
	return out
}

func TestADocumentReadsInTheOrderPeopleReadIt(t *testing.T) {
	roots := Parse(kanzlei)
	got := strings.Join(paths(roots), ",")
	if got != "apiVersion,kind,metadata,spec,status" {
		t.Errorf("top level = %s", got)
	}
}

func TestASmallObjectUnfoldsEverythingButItsMetadata(t *testing.T) {
	roots := Parse(kanzlei)
	visible := strings.Join(paths(Visible(roots, DefaultOpen)), " ")
	for _, want := range []string{"status.connector.clientSecret", "status.dbInit.job", "status.conditions[0].type", "spec.note#1"} {
		if !strings.Contains(visible, want) {
			t.Errorf("%s must be visible without a keystroke: %s", want, visible)
		}
	}
	for _, hidden := range []string{"metadata.name", "metadata.finalizers", "metadata.managedFields"} {
		if strings.Contains(visible, hidden) {
			t.Errorf("%s is noise until asked for: %s", hidden, visible)
		}
	}
}

func TestFoldedNodesSayWhatTheyHold(t *testing.T) {
	roots := Parse(kanzlei)
	if s := Find(roots, "status.dbInit").Summary; s != "{2 fields}" {
		t.Errorf("summary = %q", s)
	}
	if s := Find(roots, "status.conditions[0]").Summary; s != "Ready" {
		t.Errorf("a list item is called by its name or type; got %q", s)
	}
	if s := Find(roots, "spec.note").Summary; s != "(2 lines)" {
		t.Errorf("summary = %q", s)
	}
	if v := Find(roots, "status.observedGeneration").Value; v != "1" {
		t.Errorf("numbers keep their text: %q", v)
	}
}

func TestEveryNodeKnowsItsParent(t *testing.T) {
	n := Find(Parse(kanzlei), "status.connector.clientSecret")
	if n == nil || n.Parent == nil || n.Parent.Path != "status.connector" || n.Parent.Parent.Path != "status" || n.Depth != 2 {
		t.Fatalf("node = %+v", n)
	}
}

func TestADocumentThatIsNotYAMLIsNoTree(t *testing.T) {
	if Parse("a: [b") != nil || Parse("just a line of text") != nil {
		t.Error("an unreadable document must be shown as text instead")
	}
}
