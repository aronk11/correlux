package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// MaxReplicas bounds what Correlux will ask for. It is not the API's limit; it
// is the point past which a typed number is far more likely to be a mistake
// than an intention.
const MaxReplicas = 10000

// Scale sets the replica count of a workload through its scale subresource,
// which is the same endpoint `kubectl scale` uses.
//
// The scale subresource is why this needs no per-kind code: anything that
// declares one — a Deployment, a StatefulSet, an operator's custom resource —
// is scaled the same way.
func Scale(
	ctx context.Context,
	client rest.Interface,
	target Target,
	namespace, name string,
	replicas int32,
) error {
	if name == "" {
		return errors.New("no object name given")
	}
	if replicas < 0 || replicas > MaxReplicas {
		return fmt.Errorf("%d is not a sensible replica count", replicas)
	}

	patch := []byte(`{"spec":{"replicas":` + strconv.Itoa(int(replicas)) + `}}`)
	_, err := client.Patch(types.MergePatchType).
		AbsPath(objectPath(target, namespace, name)+"/scale").
		SetHeader("Accept", "application/json").
		Body(patch).
		DoRaw(ctx)
	return err
}
