package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// RestartedAtAnnotation is where a rolling restart is recorded. It is
// kubectl's annotation on purpose: a workload rolled from Correlux and one
// rolled from `kubectl rollout restart` must be indistinguishable afterwards.
const RestartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// Restart rolls a workload by stamping its pod template with the time it was
// asked for.
//
// Kubernetes has no restart verb. Changing the template is what a rollout is:
// the controller notices its pods no longer match and replaces them at the
// pace its own strategy allows, which is why this is a rollout rather than an
// outage.
//
// The patch is a merge patch rather than a strategic one, so an operator's
// custom resource that carries a pod template is rolled by exactly this code
// (ADR 13); strategic merge needs a Go type the API machinery knows.
func Restart(
	ctx context.Context,
	client rest.Interface,
	target Target,
	namespace, name string,
	at time.Time,
) error {
	if name == "" {
		return errors.New("no object name given")
	}

	patch, err := json.Marshal(restartPatch(at))
	if err != nil {
		return fmt.Errorf("encode the restart patch: %w", err)
	}
	_, err = client.Patch(types.MergePatchType).
		AbsPath(objectPath(target, namespace, name)).
		SetHeader("Accept", "application/json").
		Body(patch).
		DoRaw(ctx)
	return err
}

// restartPatch is the annotation, at the one place in the document that makes
// the controller replace pods rather than merely record a fact.
func restartPatch(at time.Time) map[string]any {
	return map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						RestartedAtAnnotation: at.UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
}
