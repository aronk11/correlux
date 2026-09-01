// Package discovery builds a catalog of every resource kind a cluster serves.
//
// The catalog treats CustomResourceDefinitions as first-class: a CRD is simply
// a resource whose API group is not one of Kubernetes' own, and it is listed,
// searched and rendered exactly like a Deployment. Nothing in kubeui hard-codes
// the set of resource types it understands.
//
// Discovery on a real cluster is routinely *partially* broken: an aggregated
// API server (metrics, a service mesh, a broken CRD conversion webhook) is down
// and its group fails while everything else is fine. kubeui reports that as a
// partial result rather than an error, because refusing to show 60 healthy
// resource types over one broken one is exactly the behaviour operators
// complain about.
package discovery

import (
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// Resource is one kind the API server serves.
type Resource struct {
	GVR          schema.GroupVersionResource
	GVK          schema.GroupVersionKind
	SingularName string
	ShortNames   []string
	Categories   []string
	Namespaced   bool
	Verbs        []string
	// Builtin reports whether the resource belongs to a Kubernetes API group.
	// Everything else is a custom resource.
	Builtin bool
}

// Kind returns the resource's kind, e.g. "Deployment".
func (r Resource) Kind() string { return r.GVK.Kind }

// Plural returns the resource's plural name, e.g. "deployments".
func (r Resource) Plural() string { return r.GVR.Resource }

// Group returns the API group ("" for the core group).
func (r Resource) Group() string { return r.GVR.Group }

// GroupVersion renders the group/version, e.g. "apps/v1" or "v1".
func (r Resource) GroupVersion() string {
	if r.GVR.Group == "" {
		return r.GVR.Version
	}
	return r.GVR.Group + "/" + r.GVR.Version
}

// FullName renders the fully qualified name used by kubectl, e.g.
// "deployments.apps" — unambiguous when two groups define the same kind.
func (r Resource) FullName() string {
	if r.GVR.Group == "" {
		return r.GVR.Resource
	}
	return r.GVR.Resource + "." + r.GVR.Group
}

// Listable reports whether the resource can be listed at all. Subresources and
// write-only endpoints (pods/exec, tokenreviews) cannot, and must never appear
// in a resource browser.
func (r Resource) Listable() bool {
	for _, v := range r.Verbs {
		if v == "list" {
			return true
		}
	}
	return false
}

// Catalog is the set of resources a cluster serves.
type Catalog struct {
	// Resources are listable resources, sorted by group then plural name.
	Resources []Resource
	// Failures records API groups whose discovery failed, keyed by groupVersion.
	Failures map[string]string
	// FetchedAt is when the catalog was built.
	FetchedAt time.Time
}

// Partial reports whether some API groups could not be discovered.
func (c *Catalog) Partial() bool { return len(c.Failures) > 0 }

// Len returns the number of listable resources.
func (c *Catalog) Len() int { return len(c.Resources) }

// CustomResources returns the resources that come from CRDs or aggregated APIs.
func (c *Catalog) CustomResources() []Resource {
	out := make([]Resource, 0, len(c.Resources))
	for _, r := range c.Resources {
		if !r.Builtin {
			out = append(out, r)
		}
	}
	return out
}

// Lookup resolves a kubectl-style name — "pods", "po", "deploy",
// "deployments.apps" — to a resource. The match is exact and case-insensitive;
// fuzzy matching belongs to the UI, not to name resolution.
func (c *Catalog) Lookup(name string) (Resource, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return Resource{}, false
	}
	// Fully qualified names win, then plural, singular, kind and short names.
	for _, match := range []func(Resource) bool{
		func(r Resource) bool { return strings.EqualFold(r.FullName(), needle) },
		func(r Resource) bool { return strings.EqualFold(r.Plural(), needle) },
		func(r Resource) bool { return strings.EqualFold(r.SingularName, needle) },
		func(r Resource) bool { return strings.EqualFold(r.Kind(), needle) },
		func(r Resource) bool {
			for _, s := range r.ShortNames {
				if strings.EqualFold(s, needle) {
					return true
				}
			}
			return false
		},
	} {
		for _, r := range c.Resources {
			if match(r) {
				return r, true
			}
		}
	}
	return Resource{}, false
}

// builtinGroups are the API groups shipped with Kubernetes itself. Anything
// outside this set is a custom resource as far as the UI is concerned.
var builtinGroups = map[string]struct{}{
	"":                             {},
	"admissionregistration.k8s.io": {},
	"apiextensions.k8s.io":         {},
	"apiregistration.k8s.io":       {},
	"apps":                         {},
	"authentication.k8s.io":        {},
	"authorization.k8s.io":         {},
	"autoscaling":                  {},
	"batch":                        {},
	"certificates.k8s.io":          {},
	"coordination.k8s.io":          {},
	"discovery.k8s.io":             {},
	"events.k8s.io":                {},
	"flowcontrol.apiserver.k8s.io": {},
	"networking.k8s.io":            {},
	"node.k8s.io":                  {},
	"policy":                       {},
	"rbac.authorization.k8s.io":    {},
	"resource.k8s.io":              {},
	"scheduling.k8s.io":            {},
	"storage.k8s.io":               {},
	"storagemigration.k8s.io":      {},
}

// IsBuiltinGroup reports whether an API group ships with Kubernetes.
func IsBuiltinGroup(group string) bool {
	_, ok := builtinGroups[group]
	return ok
}

// BuildCatalog turns server discovery into a catalog.
//
// It uses the server's *preferred* version of each group, which is what a user
// means by "deployments", and it tolerates partial failure: a group that cannot
// be reached is recorded in Failures and the rest of the catalog is returned.
func BuildCatalog(dc discovery.DiscoveryInterface) (*Catalog, error) {
	lists, err := dc.ServerPreferredResources()

	catalog := &Catalog{Failures: map[string]string{}, FetchedAt: time.Now()}
	if err != nil {
		var groupErr *discovery.ErrGroupDiscoveryFailed
		if !asGroupDiscoveryFailed(err, &groupErr) {
			return nil, err
		}
		for gv, gvErr := range groupErr.Groups {
			catalog.Failures[gv.String()] = gvErr.Error()
		}
	}

	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			// A group version we cannot parse is a server bug; skip it rather
			// than failing the whole catalog.
			catalog.Failures[list.GroupVersion] = parseErr.Error()
			continue
		}
		for i := range list.APIResources {
			r := toResource(gv, list.APIResources[i])
			// Subresources ("pods/log") are addressed through their parent.
			if strings.Contains(r.GVR.Resource, "/") || !r.Listable() {
				continue
			}
			catalog.Resources = append(catalog.Resources, r)
		}
	}

	sort.Slice(catalog.Resources, func(i, j int) bool {
		a, b := catalog.Resources[i], catalog.Resources[j]
		if a.GVR.Group != b.GVR.Group {
			return a.GVR.Group < b.GVR.Group
		}
		return a.GVR.Resource < b.GVR.Resource
	})
	return catalog, nil
}

func toResource(gv schema.GroupVersion, api metav1.APIResource) Resource {
	// Discovery may leave group/version empty on the resource itself, in which
	// case the containing list's group version applies.
	group, version := api.Group, api.Version
	if group == "" {
		group = gv.Group
	}
	if version == "" {
		version = gv.Version
	}
	return Resource{
		GVR:          schema.GroupVersionResource{Group: group, Version: version, Resource: api.Name},
		GVK:          schema.GroupVersionKind{Group: group, Version: version, Kind: api.Kind},
		SingularName: api.SingularName,
		ShortNames:   api.ShortNames,
		Categories:   api.Categories,
		Namespaced:   api.Namespaced,
		Verbs:        api.Verbs,
		Builtin:      IsBuiltinGroup(group),
	}
}
