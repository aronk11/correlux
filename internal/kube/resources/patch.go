package resources

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// MergePatch updates only the fields in a reviewed patch. Its resourceVersion
// precondition prevents a stale confirmation from overwriting newer intent.
func MergePatch(ctx context.Context, client rest.Interface, target Target, namespace, name string, body []byte) (*Object, error) {
	if name == "" {
		return nil, errors.New("no object name given")
	}
	raw, err := client.Patch(types.MergePatchType).AbsPath(objectPath(target, namespace, name)).SetHeader("Accept", "application/json").Body(body).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return decodeObject(target, raw)
}
