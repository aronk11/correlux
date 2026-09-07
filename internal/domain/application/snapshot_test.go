package application

import (
	"testing"
	"time"
)

// TestMergeSnapshotsKeepsEveryScopeAndTheOldestReading: a scope of several
// namespaces is one answer, and it is only as fresh as its oldest part.
func TestMergeSnapshotsKeepsEveryScopeAndTheOldestReading(t *testing.T) {
	old := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	recent := old.Add(30 * time.Second)

	merged := MergeSnapshots(
		Snapshot{
			Scope:     "payments",
			Workloads: []Workload{{Meta: Meta{Name: "api", Namespace: "payments"}}},
			FetchedAt: recent,
		},
		Snapshot{
			Scope:     "checkout",
			Workloads: []Workload{{Meta: Meta{Name: "web", Namespace: "checkout"}}},
			Truncated: true,
			FetchedAt: old,
		},
	)

	if merged.Scope != "payments, checkout" {
		t.Errorf("scope = %q, want both namespaces named", merged.Scope)
	}
	if len(merged.Workloads) != 2 {
		t.Errorf("workloads = %d, want everything from both", len(merged.Workloads))
	}
	if !merged.Truncated {
		t.Error("one truncated part makes the whole answer a subset, and it must say so")
	}
	if !merged.FetchedAt.Equal(old) {
		t.Errorf("fetched at %v, want the oldest reading %v", merged.FetchedAt, old)
	}
}

// TestMergeSnapshotsCountsOneDenialOnce: the same kind denied in three
// namespaces is one fact about the cluster, not three.
func TestMergeSnapshotsCountsOneDenialOnce(t *testing.T) {
	merged := MergeSnapshots(
		Snapshot{Gaps: []Gap{{Kind: "Ingress", Reason: "not permitted for this user"}}},
		Snapshot{Gaps: []Gap{{Kind: "Ingress", Reason: "not permitted for this user"}}},
		Snapshot{Gaps: []Gap{{Kind: "Ingress", Reason: "not permitted for this user", Scope: "checkout"}}},
	)

	if len(merged.Gaps) != 2 {
		t.Fatalf("gaps = %+v, want the identical pair collapsed and the scoped one kept",
			merged.Gaps)
	}
}
