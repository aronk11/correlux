package workloads

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aronk11/correlux/internal/domain/application"
)

// The ecosystem every cluster is likely to have: NetworkPolicies are core
// Kubernetes, and cert-manager, Flux and Helm are on most of the rest. None
// of them is required. An API the cluster does not serve answers 404, which
// means "not installed" and is silently skipped; only a refusal is a gap.

// collectEcosystem schedules the ecosystem reads on the evidence pass.
func collectEcosystem(ctx context.Context, g *group, cs kubernetes.Interface, opts Options, out *application.Context) {
	g.run("NetworkPolicy", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.NetworkingV1().NetworkPolicies(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Policies = append(out.Policies, fromPolicy(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
	})

	g.run("HorizontalPodAutoscaler", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.AutoscalingV2().HorizontalPodAutoscalers(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Autoscalers = append(out.Autoscalers, fromAutoscaler(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
	})

	g.run("PodDisruptionBudget", func() (bool, error) {
		return page(ctx, opts, func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := cs.PolicyV1().PodDisruptionBudgets(opts.Namespace).List(ctx, o)
			if err != nil {
				return "", err
			}
			g.collect(func() {
				for i := range l.Items {
					out.Budgets = append(out.Budgets, fromBudget(&l.Items[i]))
				}
			})
			return l.Continue, nil
		})
	})

	rc := restOf(cs)
	if rc == nil {
		return
	}

	g.run("Certificate", func() (bool, error) {
		var list itemList[certificateItem]
		found, err := getJSON(ctx, rc, collectionPath("/apis/cert-manager.io/v1", opts.Namespace, "certificates"), nil, &list)
		if err != nil || !found {
			return false, err
		}
		g.collect(func() {
			for i := range list.Items {
				out.Certificates = append(out.Certificates, list.Items[i].toCertificate())
			}
		})
		return false, nil
	})

	for _, ns := range gitOpsScopes(opts) {
		g.run("Kustomization", func() (bool, error) {
			return false, readFlux(ctx, g, rc, out, ns, "Kustomization",
				[]string{"/apis/kustomize.toolkit.fluxcd.io/v1"}, "kustomizations")
		})
		g.run("HelmRelease", func() (bool, error) {
			return false, readFlux(ctx, g, rc, out, ns, "HelmRelease",
				[]string{"/apis/helm.toolkit.fluxcd.io/v2", "/apis/helm.toolkit.fluxcd.io/v2beta2"}, "helmreleases")
		})
	}

	g.run("Helm release", func() (bool, error) {
		var list itemList[metadataItem]
		query := map[string]string{"labelSelector": "owner=helm"}
		found, err := getMetadata(ctx, rc, collectionPath("/api/v1", opts.Namespace, "secrets"), query, &list)
		if err != nil || !found {
			return false, err
		}
		releases := latestReleases(list.Items)
		g.collect(func() { out.Releases = append(out.Releases, releases...) })
		return false, nil
	})
}

// gitOpsScopes is where Flux objects are looked for: the scope itself, plus
// every namespace an application's Flux labels name — flux-system, usually,
// which is exactly where nobody thinks to look.
func gitOpsScopes(opts Options) []string {
	if opts.Namespace == "" {
		return []string{""}
	}
	seen := map[string]bool{opts.Namespace: true}
	out := []string{opts.Namespace}
	for _, ns := range opts.GitOpsNamespaces {
		if ns != "" && !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	return out
}

func readFlux(ctx context.Context, g *group, rc rest.Interface, out *application.Context, ns, kind string, versions []string, resource string) error {
	for _, version := range versions {
		var list itemList[fluxItem]
		found, err := getJSON(ctx, rc, collectionPath(version, ns, resource), nil, &list)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		g.collect(func() {
			for i := range list.Items {
				out.GitOps = append(out.GitOps, list.Items[i].toGitOps(kind))
			}
		})
		return nil
	}
	return nil
}

// restOf returns the clientset's REST client, or nil when there is none —
// the fake clientset tests use has a typed nil there.
func restOf(cs kubernetes.Interface) rest.Interface {
	rc := cs.CoreV1().RESTClient()
	if rc == nil {
		return nil
	}
	if v := reflect.ValueOf(rc); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	return rc
}

func collectionPath(prefix, namespace, resource string) string {
	if namespace == "" {
		return prefix + "/" + resource
	}
	return prefix + "/namespaces/" + namespace + "/" + resource
}

const readLimit = "500"

// getJSON reads one page of a collection. found is false when the API is not
// served at all.
func getJSON(ctx context.Context, rc rest.Interface, path string, query map[string]string, into any) (bool, error) {
	return get(ctx, rc, path, query, "", into)
}

// getMetadata reads only the metadata of a collection, which for Secrets is
// the difference between reading labels and reading credentials.
func getMetadata(ctx context.Context, rc rest.Interface, path string, query map[string]string, into any) (bool, error) {
	return get(ctx, rc, path, query, "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1", into)
}

func get(ctx context.Context, rc rest.Interface, path string, query map[string]string, accept string, into any) (bool, error) {
	req := rc.Get().AbsPath(path).Param("limit", readLimit)
	for k, v := range query {
		req = req.Param(k, v)
	}
	if accept != "" {
		req = req.SetHeader("Accept", accept)
	}
	raw, err := req.Do(ctx).Raw()
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, into)
}

type itemList[T any] struct {
	Items []T `json:"items"`
}

type objectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
}

func (m objectMeta) toMeta(kind string) application.Meta {
	return application.Meta{Kind: kind, Name: m.Name, Namespace: m.Namespace, UID: m.UID, Labels: m.Labels, CreatedAt: m.CreationTimestamp}
}

type condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func ready(conditions []condition) (c condition, known bool) {
	for _, c := range conditions {
		if c.Type == "Ready" {
			return c, true
		}
	}
	return condition{}, false
}

type certificateItem struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		SecretName string   `json:"secretName"`
		DNSNames   []string `json:"dnsNames"`
		IssuerRef  struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"issuerRef"`
	} `json:"spec"`
	Status struct {
		Conditions []condition `json:"conditions"`
		NotAfter   *time.Time  `json:"notAfter"`
	} `json:"status"`
}

func (c *certificateItem) toCertificate() application.Certificate {
	out := application.Certificate{
		Meta:       c.Metadata.toMeta("Certificate"),
		SecretName: c.Spec.SecretName,
		DNSNames:   c.Spec.DNSNames,
		Issuer:     strings.TrimPrefix(c.Spec.IssuerRef.Kind+"/"+c.Spec.IssuerRef.Name, "/"),
	}
	if r, ok := ready(c.Status.Conditions); ok {
		out.ReadyKnown, out.Ready, out.Reason, out.Message = true, r.Status == "True", r.Reason, r.Message
	}
	if c.Status.NotAfter != nil {
		out.NotAfter = *c.Status.NotAfter
	}
	return out
}

type fluxItem struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		Suspend bool `json:"suspend"`
	} `json:"spec"`
	Status struct {
		Conditions            []condition `json:"conditions"`
		LastAppliedRevision   string      `json:"lastAppliedRevision"`
		LastAttemptedRevision string      `json:"lastAttemptedRevision"`
	} `json:"status"`
}

func (f *fluxItem) toGitOps(kind string) application.GitOpsObject {
	out := application.GitOpsObject{
		Meta:      f.Metadata.toMeta(kind),
		Suspended: f.Spec.Suspend,
		Revision:  f.Status.LastAppliedRevision,
	}
	if r, ok := ready(f.Status.Conditions); ok {
		out.ReadyKnown, out.Ready, out.Reason, out.Message = true, r.Status == "True", r.Reason, r.Message
	}
	return out
}

type metadataItem struct {
	Metadata objectMeta `json:"metadata"`
}

// latestReleases keeps the newest revision of each release. Helm names its
// storage Secrets sh.helm.release.v1.<release>.v<revision> and labels them
// with name, version and status, which is everything needed here.
func latestReleases(items []metadataItem) []application.HelmRelease {
	newest := map[string]application.HelmRelease{}
	for i := range items {
		m := items[i].Metadata
		name, status := m.Labels["name"], m.Labels["status"]
		revision, err := strconv.Atoi(m.Labels["version"])
		if name == "" || status == "" || err != nil {
			continue
		}
		key := m.Namespace + "/" + name
		if current, ok := newest[key]; ok && current.Revision >= revision {
			continue
		}
		newest[key] = application.HelmRelease{
			Name: name, Namespace: m.Namespace, Revision: revision, Status: status, UpdatedAt: m.CreationTimestamp,
		}
	}
	out := make([]application.HelmRelease, 0, len(newest))
	for _, r := range newest {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func fromAutoscaler(h *autoscalingv2.HorizontalPodAutoscaler) application.Autoscaler {
	out := application.Autoscaler{
		Meta:       meta("HorizontalPodAutoscaler", h.ObjectMeta),
		TargetKind: h.Spec.ScaleTargetRef.Kind,
		TargetName: h.Spec.ScaleTargetRef.Name,
		Max:        h.Spec.MaxReplicas,
		Current:    h.Status.CurrentReplicas,
		Desired:    h.Status.DesiredReplicas,
		Min:        1,
		Conditions: map[string]application.Condition{},
	}
	if h.Spec.MinReplicas != nil {
		out.Min = *h.Spec.MinReplicas
	}
	for _, c := range h.Status.Conditions {
		out.Conditions[string(c.Type)] = application.Condition{Status: string(c.Status), Reason: c.Reason, Message: c.Message}
	}
	return out
}

func fromBudget(b *policyv1.PodDisruptionBudget) application.DisruptionBudget {
	out := application.DisruptionBudget{
		Meta:               meta("PodDisruptionBudget", b.ObjectMeta),
		DisruptionsAllowed: b.Status.DisruptionsAllowed,
		CurrentHealthy:     b.Status.CurrentHealthy,
		DesiredHealthy:     b.Status.DesiredHealthy,
		ExpectedPods:       b.Status.ExpectedPods,
	}
	if b.Spec.Selector == nil {
		out.NoSelector = true
	} else {
		out.MatchLabels = b.Spec.Selector.MatchLabels
		out.HasExpressions = len(b.Spec.Selector.MatchExpressions) > 0
	}
	return out
}

func fromPolicy(p *networkingv1.NetworkPolicy) application.NetworkPolicy {
	out := application.NetworkPolicy{
		Meta:           meta("NetworkPolicy", p.ObjectMeta),
		MatchLabels:    p.Spec.PodSelector.MatchLabels,
		HasExpressions: len(p.Spec.PodSelector.MatchExpressions) > 0,
		IngressRules:   len(p.Spec.Ingress),
		EgressRules:    len(p.Spec.Egress),
	}
	if len(p.Spec.PolicyTypes) == 0 {
		// The API's own default: Ingress always, Egress only when rules exist.
		out.Ingress = true
		out.Egress = len(p.Spec.Egress) > 0
	}
	for _, t := range p.Spec.PolicyTypes {
		switch t {
		case networkingv1.PolicyTypeIngress:
			out.Ingress = true
		case networkingv1.PolicyTypeEgress:
			out.Egress = true
		}
	}
	return out
}
