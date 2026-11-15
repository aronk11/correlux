package workloads

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aronk11/correlux/internal/domain/application"
)

// collectRollouts reads Argo Rollouts where the cluster serves them.
//
// A Rollout is a Deployment by another name: it owns ReplicaSets, has a
// replica count and a pod selector. Without it the pods of a canary still
// showed up — grouped by their labels — but as an application with no
// workload, no desired count and therefore no way to be "2 of 5 ready".
func collectRollouts(ctx context.Context, g *group, cs kubernetes.Interface, opts Options, snap *application.Snapshot) {
	rc := restOf(cs)
	if rc == nil {
		return
	}
	key := absentKey{cs, "Rollout"}
	if _, absent := routesAbsent.Load(key); absent {
		return
	}
	g.run("Rollout", func() (bool, error) {
		path := collectionPath("/apis/argoproj.io/v1alpha1", opts.Namespace, "rollouts")
		served := true
		truncated, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			var list itemList[rolloutItem]
			found, err := getJSON(ctx, rc, path, o, &list)
			if optionalRefused(err) {
				found, err = false, nil
			}
			if err != nil {
				return "", err
			}
			if !found {
				served = false
				return "", nil
			}
			g.collect(func() {
				for i := range list.Items {
					snap.Workloads = append(snap.Workloads, list.Items[i].toWorkload())
				}
			})
			return list.Metadata.Continue, nil
		})
		if err == nil && !served {
			routesAbsent.Store(key, struct{}{})
		}
		return truncated, err
	})
}

type rolloutItem struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		Replicas *int32 `json:"replicas"`
		Paused   bool   `json:"paused"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas   int32  `json:"readyReplicas"`
		UpdatedReplicas int32  `json:"updatedReplicas"`
		Phase           string `json:"phase"`
		Message         string `json:"message"`
	} `json:"status"`
}

func (r *rolloutItem) toWorkload() application.Workload {
	w := application.Workload{
		Meta:       r.Metadata.toMeta("Rollout"),
		Desired:    replicas(r.Spec.Replicas),
		Ready:      r.Status.ReadyReplicas,
		Updated:    r.Status.UpdatedReplicas,
		Selector:   r.Spec.Selector.MatchLabels,
		Replicated: true,
		// A Rollout pauses itself at a canary step and waits to be promoted;
		// like a paused Deployment that is a state, not a fault.
		Paused: r.Spec.Paused || r.Status.Phase == "Paused",
	}
	if r.Status.Phase == "Degraded" {
		// The controller's own verdict that the rollout will not finish:
		// an aborted canary, a failed analysis run, a deadline exceeded.
		w.Stalled, w.StalledReason, w.StalledMessage = true, "Degraded", r.Status.Message
	}
	return w
}
