package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// DeleteOptions carries what makes a delete safe rather than what makes it
// configurable.
type DeleteOptions struct {
	// UID, when set, is a precondition: the server deletes only if the object
	// still carries this identity, so a delete cannot land on a replacement
	// that was recreated under the same name since the screen was read.
	UID string
	// ResourceVersion, when set, is the second precondition. It refuses the
	// delete when anything about the object has changed, status included,
	// which is stricter than most callers want.
	ResourceVersion string
	// Orphan leaves the dependents behind instead of deleting them with the
	// object.
	Orphan bool
}

// Delete removes one object of any kind — native or custom.
//
// Deletion is in the foreground by default: the API server keeps the object
// until what it owns is gone, so "its pods go with it" is what happens rather
// than what is promised. Orphaning is the other choice a user can mean, and it
// is asked for explicitly.
func Delete(
	ctx context.Context,
	client rest.Interface,
	target Target,
	namespace, name string,
	opts DeleteOptions,
) error {
	if name == "" {
		return errors.New("no object name given")
	}

	body, err := json.Marshal(deleteBody(opts))
	if err != nil {
		return fmt.Errorf("encode delete options: %w", err)
	}
	_, err = client.Delete().
		AbsPath(objectPath(target, namespace, name)).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		Body(body).
		DoRaw(ctx)
	return err
}

// deleteBody renders the options as the DeleteOptions document the API server
// decodes them from.
func deleteBody(opts DeleteOptions) metav1.DeleteOptions {
	policy := metav1.DeletePropagationForeground
	if opts.Orphan {
		policy = metav1.DeletePropagationOrphan
	}
	body := metav1.DeleteOptions{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DeleteOptions",
			APIVersion: metav1.SchemeGroupVersion.String(),
		},
		PropagationPolicy: &policy,
	}
	if opts.UID != "" || opts.ResourceVersion != "" {
		body.Preconditions = &metav1.Preconditions{}
		if opts.UID != "" {
			uid := types.UID(opts.UID)
			body.Preconditions.UID = &uid
		}
		if opts.ResourceVersion != "" {
			version := opts.ResourceVersion
			body.Preconditions.ResourceVersion = &version
		}
	}
	return body
}
