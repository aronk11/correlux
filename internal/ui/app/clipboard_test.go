package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/kube/resources"
)

func TestCopyRefNamesANamespacedObjectAsNamespaceSlashName(t *testing.T) {
	if got := copyRef(objectRef{Name: "payments", Namespace: "shop"}); got != "shop/payments" {
		t.Errorf("copyRef = %q, want %q", got, "shop/payments")
	}
	if got := copyRef(objectRef{Name: "some-node"}); got != "some-node" {
		t.Errorf("copyRef of a cluster-scoped object = %q, want just the name", got)
	}
}

func TestTableAsTextIsTabSeparatedWithAHeaderRow(t *testing.T) {
	columns := []resources.Column{{Name: "Name"}, {Name: "Status"}}
	rows := []resources.Row{
		{Name: "payments-0", Cells: []string{"payments-0", "Running"}},
		{Name: "payments-1", Cells: []string{"payments-1", "CrashLoopBackOff"}},
	}

	got := tableAsText(columns, rows)
	want := "Name\tStatus\npayments-0\tRunning\npayments-1\tCrashLoopBackOff"
	if got != want {
		t.Errorf("tableAsText =\n%q\nwant\n%q", got, want)
	}
}

func TestKubectlGetNamesTheContextAndUsesTheDiscoveredPlural(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	cmd := kubectlGet(m, objectRef{Kind: "Pod", Name: "payments-0", Namespace: "shop", Resource: "pods"})
	for _, want := range []string{"kubectl", "--context staging", "get pods payments-0", "-n shop", "-o yaml"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q must contain %q", cmd, want)
		}
	}
}

func TestKubectlGetOmitsTheNamespaceFlagForClusterScopedObjects(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())

	cmd := kubectlGet(m, objectRef{Kind: "Node", Name: "node-1"})
	if strings.Contains(cmd, "-n ") {
		t.Errorf("command %q must not namespace a cluster-scoped object", cmd)
	}
}

func TestCopyingWithNothingToTargetSaysSo(t *testing.T) {
	m := newTestModel(t)

	cmd := press(t, m, "c")
	if cmd == nil {
		t.Fatal("pressing c with nothing to copy must still return a command, to show the notice")
	}
	if out := plainView(m); !strings.Contains(out, "Nothing to copy here") {
		t.Errorf("the refusal must say so:\n%s", out)
	}
}

func TestCopyingAnOpenObjectNamesItInTheNotice(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	m.OpenObjectForTest("Pod", "payments-7d8f-0", "default")
	loadObjectInto(m, podObject("payments-7d8f-0", "payments-7d8f"))

	cmd := press(t, m, "c")
	if cmd == nil {
		t.Fatal("c on an open object must copy it")
	}
	if out := plainView(m); !strings.Contains(out, "Copied Pod/payments-7d8f-0") {
		t.Errorf("the notice must name what was copied:\n%s", out)
	}
}

func TestCopyingATableRowUsesTheRowsOwnNamespace(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	press(t, m, "ctrl+b")
	typeInto(t, m, "pods")
	press(t, m, "enter")
	m.Update(tableLoadedMsg{gen: m.table.Generation(), table: podTablePage("payments-0")})

	label, value, ok := m.copyTarget()
	if !ok {
		t.Fatal("a table row must be a copy target")
	}
	if label != "payments-0" {
		t.Errorf("label = %q, want the row's name", label)
	}
	if value != "default/payments-0" {
		t.Errorf("value = %q, want the row's namespace and name", value)
	}
}
