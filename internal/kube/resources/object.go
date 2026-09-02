package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

// Object is one Kubernetes object, exactly as the server returned it.
//
// The raw document is kept because it is the truth: kubeui renders it to YAML
// for reading, but never re-serialises a parsed structure, which would quietly
// drop the fields it does not know about — and on a custom resource that is
// most of them.
type Object struct {
	Target    Target
	Kind      string
	Name      string
	Namespace string
	// Raw is the server's JSON.
	Raw []byte
	// YAML is Raw rendered for reading.
	YAML string

	UID         string
	CreatedAt   time.Time
	Labels      map[string]string
	Annotations map[string]string
	Owners      []OwnerRef
	// ResourceVersion is what an update must send back to detect a conflict.
	ResourceVersion string
}

// OwnerRef is an owner reference, reduced to what navigation needs.
type OwnerRef struct {
	Kind       string
	Name       string
	UID        string
	Controller bool
}

// Get fetches a single object.
func Get(ctx context.Context, client rest.Interface, target Target, namespace, name string) (*Object, error) {
	if name == "" {
		return nil, errors.New("no object name given")
	}
	raw, err := client.Get().
		AbsPath(objectPath(target, namespace, name)).
		SetHeader("Accept", "application/json").
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return decodeObject(target, raw)
}

// objectPath addresses one object; the collection path plus its name.
func objectPath(target Target, namespace, name string) string {
	return path(target, namespace) + "/" + name
}

func decodeObject(target Target, raw []byte) (*Object, error) {
	var meta struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name              string            `json:"name"`
			Namespace         string            `json:"namespace"`
			UID               string            `json:"uid"`
			CreationTimestamp time.Time         `json:"creationTimestamp"`
			Labels            map[string]string `json:"labels"`
			Annotations       map[string]string `json:"annotations"`
			ResourceVersion   string            `json:"resourceVersion"`
			OwnerReferences   []struct {
				Kind       string `json:"kind"`
				Name       string `json:"name"`
				UID        string `json:"uid"`
				Controller *bool  `json:"controller"`
			} `json:"ownerReferences"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}
	if meta.Kind == "Status" {
		// The API server answers an error with a Status object and a 2xx in
		// some proxy setups; treating it as the object would show the error as
		// if it were the resource.
		var status metav1.Status
		if err := json.Unmarshal(raw, &status); err == nil && status.Status == metav1.StatusFailure {
			return nil, fmt.Errorf("%s", status.Message)
		}
	}

	out := &Object{
		Target:          target,
		Kind:            meta.Kind,
		Name:            meta.Metadata.Name,
		Namespace:       meta.Metadata.Namespace,
		UID:             meta.Metadata.UID,
		Raw:             raw,
		CreatedAt:       meta.Metadata.CreationTimestamp,
		Labels:          meta.Metadata.Labels,
		Annotations:     meta.Metadata.Annotations,
		ResourceVersion: meta.Metadata.ResourceVersion,
	}
	for _, o := range meta.Metadata.OwnerReferences {
		out.Owners = append(out.Owners, OwnerRef{
			Kind: o.Kind, Name: o.Name, UID: o.UID,
			Controller: o.Controller != nil && *o.Controller,
		})
	}

	// A document kubeui cannot render as YAML is still worth showing as the
	// JSON it came as, rather than as an error about a resource that is fine.
	if converted, err := yaml.JSONToYAML(raw); err == nil {
		out.YAML = string(converted)
	} else {
		out.YAML = string(raw)
	}
	return out, nil
}

// Controller returns the owner reference that manages this object.
func (o *Object) Controller() (OwnerRef, bool) {
	for _, ref := range o.Owners {
		if ref.Controller {
			return ref, true
		}
	}
	return OwnerRef{}, false
}

// Update replaces an object with an edited document.
//
// The document is sent with the resourceVersion it was read at, which is what
// makes the server refuse an edit written against a version somebody else has
// already replaced. Losing an edit silently is worse than being told to try
// again.
func Update(
	ctx context.Context,
	client rest.Interface,
	target Target,
	namespace, name string,
	document []byte,
) (*Object, error) {
	if name == "" {
		return nil, errors.New("no object name given")
	}
	// Strict: a duplicate key in an edited document is a mistake, and the
	// lenient reading silently keeps the last one — which means half of
	// somebody's edit disappearing without a word.
	body, err := yaml.YAMLToJSONStrict(document)
	if err != nil {
		return nil, fmt.Errorf("this document cannot be applied: %w", err)
	}

	raw, err := client.Put().
		AbsPath(objectPath(target, namespace, name)).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		Body(body).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return decodeObject(target, raw)
}

// Identity reads the kind, name and namespace out of an edited document, so an
// edit that renames the object — or changes its kind — can be refused before it
// is sent somewhere it does not belong.
func Identity(document []byte) (kind, name, namespace string, err error) {
	body, err := yaml.YAMLToJSONStrict(document)
	if err != nil {
		return "", "", "", fmt.Errorf("this is not valid YAML: %w", err)
	}
	var doc struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", "", "", fmt.Errorf("this is not a Kubernetes object: %w", err)
	}
	return doc.Kind, doc.Metadata.Name, doc.Metadata.Namespace, nil
}
