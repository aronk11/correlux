package resources

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// Cordon marks a node schedulable or unschedulable, which is the whole of what
// `kubectl cordon` and `kubectl uncordon` do: only the scheduler's next
// decision changes, and the pods already running on the node are left alone.
//
// Nodes are cluster-scoped, so the object is addressed without a namespace.
func Cordon(
	ctx context.Context,
	client rest.Interface,
	target Target,
	name string,
	unschedulable bool,
) error {
	if name == "" {
		return errors.New("no node name given")
	}

	patch := []byte(`{"spec":{"unschedulable":` + strconv.FormatBool(unschedulable) + `}}`)
	_, err := client.Patch(types.MergePatchType).
		AbsPath(objectPath(target, "", name)).
		SetHeader("Accept", "application/json").
		Body(patch).
		DoRaw(ctx)
	return err
}

// Unschedulable reads spec.unschedulable out of a node's document.
//
// It reports known as false for anything it cannot read as an object, so a
// caller can say it does not know rather than report a node as schedulable on
// the strength of a document it failed to parse.
func Unschedulable(raw []byte) (unschedulable, known bool) {
	if len(raw) == 0 {
		return false, false
	}
	var doc struct {
		Spec struct {
			Unschedulable bool `json:"unschedulable"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, false
	}
	return doc.Spec.Unschedulable, true
}
