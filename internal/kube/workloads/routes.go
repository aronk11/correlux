package workloads

import (
	"context"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aronk11/correlux/internal/domain/application"
)

// The Gateway API's routes are what Ingress is being replaced by, and to an
// application they are the same thing: where its traffic comes in. They are
// read into the same slot, with the kind kept, so every screen and rule that
// knows about an Ingress knows about them.

// routeKinds are the route kinds read, with the API versions tried in order.
var routeKinds = []struct {
	kind, resource string
	versions       []string
}{
	{"HTTPRoute", "httproutes", []string{"/apis/gateway.networking.k8s.io/v1", "/apis/gateway.networking.k8s.io/v1beta1"}},
	{"GRPCRoute", "grpcroutes", []string{"/apis/gateway.networking.k8s.io/v1"}},
}

// routesAbsent remembers the clusters that answered "no such API". The
// dashboard is reloaded on a timer, and asking a cluster without the Gateway
// API for its routes twice a second, for a 404 each time, is the kind of
// noise in an API server's audit log that gets a tool uninstalled. The cost
// is that a Gateway API installed mid-session is seen after a restart.
var routesAbsent sync.Map // kubernetes.Interface + kind → struct{}

type absentKey struct {
	cs   kubernetes.Interface
	kind string
}

func collectRoutes(ctx context.Context, g *group, cs kubernetes.Interface, opts Options, snap *application.Snapshot) {
	rc := restOf(cs)
	if rc == nil {
		return
	}
	for _, rk := range routeKinds {
		key := absentKey{cs, rk.kind}
		if _, absent := routesAbsent.Load(key); absent {
			continue
		}
		g.run(rk.kind, func() (bool, error) {
			for _, version := range rk.versions {
				path := collectionPath(version, opts.Namespace, rk.resource)
				served := true
				truncated, err := page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
					var list itemList[routeItem]
					found, err := getJSON(ctx, rc, path, o, &list)
					if err != nil {
						return "", err
					}
					if !found {
						served = false
						return "", nil
					}
					g.collect(func() {
						for i := range list.Items {
							snap.Ingresses = append(snap.Ingresses, list.Items[i].toIngress(rk.kind))
						}
					})
					return list.Metadata.Continue, nil
				})
				if err != nil || served {
					return truncated, err
				}
			}
			routesAbsent.Store(key, struct{}{})
			return false, nil
		})
	}
}

// routeItem is a Gateway API route, reduced to where it sends traffic and
// what its Gateways said about it.
type routeItem struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		Hostnames  []string `json:"hostnames"`
		ParentRefs []struct {
			Name string `json:"name"`
		} `json:"parentRefs"`
		Rules []struct {
			BackendRefs []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		Parents []struct {
			ParentRef struct {
				Name string `json:"name"`
			} `json:"parentRef"`
			Conditions []condition `json:"conditions"`
		} `json:"parents"`
	} `json:"status"`
}

func (r *routeItem) toIngress(kind string) application.Ingress {
	out := application.Ingress{Meta: r.Metadata.toMeta(kind), Hosts: r.Spec.Hostnames}
	for _, p := range r.Spec.ParentRefs {
		if p.Name != "" {
			out.Gateways = append(out.Gateways, p.Name)
		}
	}
	seen := map[string]bool{}
	for _, rule := range r.Spec.Rules {
		for _, b := range rule.BackendRefs {
			// Only Services next door: a backend in another namespace or of
			// another kind is real, and not something grouping can follow.
			if (b.Kind != "" && b.Kind != "Service") || (b.Namespace != "" && b.Namespace != r.Metadata.Namespace) {
				continue
			}
			if b.Name != "" && !seen[b.Name] {
				seen[b.Name] = true
				out.Backends = append(out.Backends, b.Name)
			}
		}
	}
	for _, parent := range r.Status.Parents {
		for _, c := range parent.Conditions {
			if (c.Type == "Accepted" || c.Type == "ResolvedRefs") && c.Status == "False" {
				line := "Gateway/" + parent.ParentRef.Name + ": " + c.Type + "=False"
				if c.Reason != "" {
					line += " (" + c.Reason + ")"
				}
				if c.Message != "" {
					line += " — " + c.Message
				}
				out.Rejected = append(out.Rejected, line)
			}
		}
	}
	return out
}
