//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
)

// deploymentResource resolves the Deployment kind through real discovery, the
// same way the application does.
func deploymentResource(t *testing.T) discovery.Resource {
	t.Helper()
	res, ok := catalogFor(t).Lookup("deployments.apps")
	if !ok {
		t.Fatal("a cluster always serves Deployments")
	}
	if !res.Scalable {
		t.Error("a Deployment declares a scale subresource; discovery must have found it")
	}
	return res
}

func getDeployment(t *testing.T, name string) *resources.Object {
	t.Helper()
	obj, err := shared.factory.GetObject(ctx(t), shared.context, deploymentResource(t), seededNamespace, name)
	if err != nil {
		// The seeded namespace may be absent or half-written while somebody is
		// re-seeding the cluster; that is not this test's subject.
		t.Skipf("%s/%s is not there: %v — run `task kind:seed`", seededNamespace, name, err)
	}
	return obj
}

func TestScalingAWorkloadReachesTheCluster(t *testing.T) {
	const name = "app-01"
	before := getDeployment(t, name)
	original := replicasOf(t, before)

	t.Cleanup(func() {
		// Whatever this test does, the cluster is left as it was found: the
		// other tests read the same namespace.
		if err := shared.factory.Scale(ctx(t), shared.context, deploymentResource(t),
			seededNamespace, name, original); err != nil {
			t.Errorf("restoring %s to %d replicas: %v", name, original, err)
		}
	})

	wanted := original + 1
	if err := shared.factory.Scale(ctx(t), shared.context, deploymentResource(t),
		seededNamespace, name, wanted); err != nil {
		t.Fatalf("Scale: %v", err)
	}

	after := getDeployment(t, name)
	if got := replicasOf(t, after); got != wanted {
		t.Errorf("replicas = %d, want %d", got, wanted)
	}
}

func TestEditingAnObjectSendsTheDocumentBack(t *testing.T) {
	const name = "app-02"
	const key = "kubeui.dev/integration-test"

	// A previous run may have been interrupted before its cleanup; adding the
	// same key twice would produce a duplicate, which the strict reader
	// correctly refuses.
	removeAnnotation(t, name, key)
	before := getDeployment(t, name)

	// The smallest possible real edit: an annotation nobody else touches, added
	// to the block the object already has rather than as a second one.
	edited := strings.Replace(before.YAML,
		"  annotations:\n", "  annotations:\n    "+key+": \"1\"\n", 1)
	if edited == before.YAML {
		t.Fatalf("the fixture did not change; document was:\n%s", before.YAML)
	}

	after, err := shared.factory.UpdateObject(ctx(t), shared.context, deploymentResource(t),
		seededNamespace, name, []byte(edited))
	if err != nil {
		t.Fatalf("UpdateObject: %v", err)
	}
	if after.Annotations[key] != "1" {
		t.Errorf("the edit did not land: %v", after.Annotations)
	}
	if after.ResourceVersion == before.ResourceVersion {
		t.Error("a successful update must produce a new resource version")
	}

	t.Cleanup(func() { removeAnnotation(t, name, key) })
}

func TestAnEditWrittenAgainstAStaleVersionIsRefused(t *testing.T) {
	// This is the whole reason the resourceVersion is read and sent back:
	// somebody else changed the object while it sat in the editor, and losing
	// their change silently would be worse than being told to try again.
	const name = "app-03"
	removeAnnotation(t, name, "kubeui.dev/conflict")
	removeAnnotation(t, name, "kubeui.dev/second")
	stale := getDeployment(t, name)

	// Somebody else's change lands first.
	current := strings.Replace(stale.YAML,
		"  annotations:\n", "  annotations:\n    kubeui.dev/conflict: \"1\"\n", 1)
	if _, err := shared.factory.UpdateObject(ctx(t), shared.context, deploymentResource(t),
		seededNamespace, name, []byte(current)); err != nil {
		t.Fatalf("first update: %v", err)
	}
	t.Cleanup(func() { removeAnnotation(t, name, "kubeui.dev/conflict") })

	// Now the edit that was opened before it.
	edited := strings.Replace(stale.YAML,
		"  annotations:\n", "  annotations:\n    kubeui.dev/second: \"1\"\n", 1)
	_, err := shared.factory.UpdateObject(ctx(t), shared.context, deploymentResource(t),
		seededNamespace, name, []byte(edited))
	if err == nil {
		t.Fatal("an update against a stale version must be refused, not silently applied")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "conflict") &&
		!strings.Contains(err.Error(), "modified") {
		t.Errorf("the refusal must read as a conflict, got %q", err)
	}
}

// removeAnnotation puts the object back as it was found, re-reading it when the
// controller has bumped the version in the meantime — which is exactly the
// conflict the product is supposed to detect, and a nuisance in a cleanup.
func removeAnnotation(t *testing.T, name, key string) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		current := getDeployment(t, name)
		cleaned := strings.Replace(current.YAML, "    "+key+": \"1\"\n", "", 1)
		if cleaned == current.YAML {
			return // already gone
		}
		_, err := shared.factory.UpdateObject(ctx(t), shared.context, deploymentResource(t),
			seededNamespace, name, []byte(cleaned))
		if err == nil {
			return
		}
		if !strings.Contains(strings.ToLower(err.Error()), "conflict") {
			t.Errorf("removing %s: %v", key, err)
			return
		}
	}
	t.Errorf("could not remove %s: the object kept changing underneath", key)
}

// replicasOf reads the replica count out of a fetched Deployment.
func replicasOf(t *testing.T, obj *resources.Object) int32 {
	t.Helper()
	for _, line := range strings.Split(obj.YAML, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "replicas: "); ok {
			var n int32
			for _, r := range value {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + (r - '0')
			}
			return n
		}
	}
	t.Fatalf("no replica count in:\n%s", obj.YAML)
	return 0
}
