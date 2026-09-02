package diagnosis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/aronk11/correlux/internal/domain/application"
)

// serviceWithoutEndpoints reports a service that routes to nothing.
//
// This is the failure the SPEC's example is built around, and it is worth
// separating into three different sentences, because the fix differs:
//
//   - the selector matches no pods at all: a label typo, or the workload was
//     renamed and the service was not;
//   - it matches pods, but none of them are ready: the application's own
//     problem, already explained by a pod rule;
//   - the pods are ready and the endpoints are still empty: rarer, and worth
//     saying plainly rather than guessing about.
func serviceWithoutEndpoints(in *Input) []Diagnosis {
	if len(in.Context.Endpoints) == 0 && len(in.Context.Gaps) > 0 {
		// Endpoints could not be read at all; saying "no endpoints" would be a
		// statement about Correlux's permissions, not about the cluster.
		return nil
	}

	out := make([]Diagnosis, 0, len(in.App.Services))
	for i := range in.App.Services {
		svc := &in.App.Services[i]
		if len(svc.Selector) == 0 {
			// ExternalName services and manually managed endpoints have no
			// selector, and nothing here applies to them.
			continue
		}
		set, known := in.Context.EndpointsFor(svc.Namespace, svc.Name)
		if !known && len(in.Context.Endpoints) == 0 {
			continue
		}
		if set.Ready > 0 {
			continue
		}

		selected, ready := selectedPods(&in.App, svc.Selector)
		var (
			cause      string
			confidence Confidence
			chainTail  = "0 ready endpoints"
		)
		switch {
		case len(selected) == 0:
			cause = "the service selector " + renderSelector(svc.Selector) + " matches no pods in this namespace"
			confidence = High
			chainTail = "selector matches nothing"
		case ready == 0:
			cause = "every pod the selector matches is not ready, so none of them are published"
			confidence = High
		default:
			cause = "pods are ready but no address is published for this service"
			confidence = Low
		}

		d := Diagnosis{
			Rule:       "service.noendpoints",
			Severity:   Critical,
			Subject:    application.ObjectRef{Kind: "Service", Name: svc.Name, UID: svc.UID},
			Problem:    "Service/" + svc.Name + " has no ready endpoints, so nothing it fronts can be reached",
			Cause:      cause,
			Confidence: confidence,
			Chain:      chain(&in.App, "Service/"+svc.Name, chainTail),
			Suggestions: []Suggestion{
				{Text: "Compare the service selector with the labels on the pods",
					Command: describeCommand("service", svc.Namespace, svc.Name)},
				{Text: "Look at the endpoints the cluster publishes for it",
					Command: "kubectl get endpointslices -n " + svc.Namespace +
						" -l kubernetes.io/service-name=" + svc.Name},
			},
			Evidence: []Evidence{{
				Kind:   "Service",
				Name:   svc.Name,
				Detail: "selector " + renderSelector(svc.Selector) + "; " + strconv.Itoa(set.Ready) + " ready, " + strconv.Itoa(set.NotReady) + " not ready",
			}},
		}
		for _, p := range limitPods(selected) {
			d.Evidence = append(d.Evidence, Evidence{
				Kind: "Pod", Name: p.Name, Detail: "matched, ready=" + boolWord(p.Ready),
			})
		}
		out = append(out, d)
	}
	return out
}

// ingressWithoutBackend reports an ingress rule pointing at a service that is
// not in the scope at all. A request to that host returns 503 and nothing in
// the application looks wrong.
func ingressWithoutBackend(in *Input) []Diagnosis {
	if len(in.App.Ingresses) == 0 {
		return nil
	}
	known := map[string]bool{}
	for i := range in.Scope.Services {
		known[in.Scope.Services[i].Name] = true
	}
	if len(known) == 0 {
		// Without the scope's services this rule cannot tell "missing" from
		// "not loaded", and guessing would accuse a working ingress.
		return nil
	}

	out := make([]Diagnosis, 0, len(in.App.Ingresses))
	for i := range in.App.Ingresses {
		ing := &in.App.Ingresses[i]
		var missing []string
		for _, backend := range ing.Backends {
			if !known[backend] {
				missing = append(missing, backend)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)

		out = append(out, Diagnosis{
			Rule:       "ingress.nobackend",
			Severity:   Critical,
			Subject:    application.ObjectRef{Kind: "Ingress", Name: ing.Name, UID: ing.UID},
			Problem:    "Ingress/" + ing.Name + " routes to " + strings.Join(missing, ", ") + ", which does not exist here",
			Cause:      "the backend service named in the ingress is not in this namespace, so the router has nothing to send requests to",
			Confidence: High,
			Chain:      chain(&in.App, "Ingress/"+ing.Name, "missing backend"),
			Suggestions: []Suggestion{
				{Text: "Check the service name in the ingress rules",
					Command: describeCommand("ingress", ing.Namespace, ing.Name)},
			},
			Evidence: []Evidence{{
				Kind:   "Ingress",
				Name:   ing.Name,
				Detail: "hosts " + orUnknown(strings.Join(ing.Hosts, ", ")) + " → " + strings.Join(missing, ", "),
			}},
		})
	}
	return out
}

// selectedPods returns the application's pods a selector matches, and how many
// of them are ready.
func selectedPods(app *application.Application, selector map[string]string) ([]*application.Pod, int) {
	out := make([]*application.Pod, 0, len(app.Pods))
	ready := 0
	for _, p := range sortedNames(livePods(app)) {
		if !matches(selector, p.Labels) {
			continue
		}
		out = append(out, p)
		if p.Ready {
			ready++
		}
	}
	return out, ready
}

// renderSelector prints a selector the way kubectl does, sorted so the same
// selector always reads the same.
func renderSelector(selector map[string]string) string {
	parts := make([]string, 0, len(selector))
	for k, v := range selector {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
