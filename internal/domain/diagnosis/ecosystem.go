package diagnosis

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// certWarning is how close to expiry a certificate that still reports Ready
// has to be before it is worth saying. cert-manager renews at two thirds of a
// certificate's life by default, so a Ready certificate this close to the end
// is one whose renewal is not happening.
const certWarning = 14 * 24 * time.Hour

// helmStuck is how long a Helm release may sit in a pending state before it
// is called stuck rather than in progress.
const helmStuck = 10 * time.Minute

// servicePorts reports a service whose port sends traffic somewhere the pods
// behind it do not listen. The pods are Ready, the endpoints are published,
// and every connection is refused: the failure that looks like nothing.
func servicePorts(in *Input) []Diagnosis {
	var out []Diagnosis
	for i := range in.App.Services {
		svc := &in.App.Services[i]
		if len(svc.Selector) == 0 {
			continue
		}
		pods, _ := selectedPods(&in.App, svc.Selector)
		if len(pods) == 0 {
			continue
		}
		declared := declaredPorts(pods)
		for _, t := range svc.Targets {
			if t.TargetPort == "" {
				continue
			}
			number, err := strconv.ParseInt(t.TargetPort, 10, 32)
			named := err != nil
			switch {
			case named && !declared.hasName(t.TargetPort):
				out = append(out, portFinding(in, svc, t, pods, declared, Critical, High,
					"the port is named "+quote(t.TargetPort)+", and no container in the selected pods declares a port by that name, so Kubernetes has nowhere to send it",
					""))
			case !named && len(declared.numbers) > 0 && !declared.hasNumber(int32(number)):
				out = append(out, portFinding(in, svc, t, pods, declared, Warning, Medium,
					"traffic goes to port "+t.TargetPort+", but the containers declare "+declared.list()+"; if nothing listens on "+t.TargetPort+", connections are refused",
					"Kubernetes does not know which ports a process actually listens on; a container may listen on a port it never declared."))
			}
		}
	}
	return out
}

func portFinding(in *Input, svc *application.Service, t application.ServicePort, pods []*application.Pod, declared ports, severity Severity, confidence Confidence, cause, unknown string) Diagnosis {
	label := strconv.Itoa(int(t.Port))
	if t.Name != "" {
		label += " (" + t.Name + ")"
	}
	return Diagnosis{
		Rule:       "traffic.targetport",
		Severity:   severity,
		Subject:    application.ObjectRef{Kind: "Service", Name: svc.Name, UID: svc.UID},
		Problem:    "Service/" + svc.Name + " port " + label + " points at a port its pods do not offer",
		Cause:      cause,
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Service/"+svc.Name, "port "+label, "targetPort "+t.TargetPort),
		Suggestions: []Suggestion{
			{Text: "Compare the service's targetPort with the containerPort the process listens on",
				Command: "kubectl get service -n " + svc.Namespace + " " + svc.Name + " -o jsonpath='{.spec.ports}'"},
			{Text: "Test the port from inside the cluster, bypassing the service",
				Command: "kubectl port-forward -n " + pods[0].Namespace + " pod/" + pods[0].Name + " 8080:" + t.TargetPort},
		},
		Evidence: []Evidence{
			{Kind: "Service", Name: svc.Name, Detail: "port " + label + " → targetPort " + t.TargetPort + "/" + orUnknown(t.Protocol)},
			{Kind: "Pod", Name: pods[0].Name, Detail: "declares " + orNone(declared.list())},
		},
	}
}

type ports struct {
	names   map[string]bool
	numbers map[int32]bool
}

func declaredPorts(pods []*application.Pod) ports {
	p := ports{names: map[string]bool{}, numbers: map[int32]bool{}}
	for _, pod := range pods {
		for _, c := range pod.Containers {
			if c.Init && !c.Sidecar {
				continue
			}
			for _, port := range c.Ports {
				if port.Name != "" {
					p.names[port.Name] = true
				}
				p.numbers[port.Port] = true
			}
		}
	}
	return p
}

func (p ports) hasName(name string) bool    { return p.names[name] }
func (p ports) hasNumber(number int32) bool { return p.numbers[number] }

func (p ports) list() string {
	numbers := make([]int, 0, len(p.numbers))
	for n := range p.numbers {
		numbers = append(numbers, int(n))
	}
	sort.Ints(numbers)
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// networkIsolated reports pods that NetworkPolicies cut off entirely. Policies
// are additive: a deny-all policy only isolates a pod when no other policy
// selecting that pod allows anything in the same direction, so all of them
// are read before anything is said.
func networkIsolated(in *Input) []Diagnosis {
	if len(in.Context.Policies) == 0 {
		return nil
	}
	pods := sortedNames(livePods(&in.App))
	if len(pods) == 0 {
		return nil
	}
	var out []Diagnosis
	if d, ok := isolated(in, pods, true); ok {
		out = append(out, d)
	}
	if d, ok := isolated(in, pods, false); ok {
		out = append(out, d)
	}
	return out
}

func isolated(in *Input, pods []*application.Pod, ingress bool) (Diagnosis, bool) {
	var (
		cut      []*application.Pod
		blockers []string
	)
	for _, p := range pods {
		restricted, allowed, known := false, false, true
		var names []string
		for i := range in.Context.Policies {
			pol := &in.Context.Policies[i]
			if pol.Namespace != p.Namespace {
				continue
			}
			applies, rules := pol.Ingress, pol.IngressRules
			if !ingress {
				applies, rules = pol.Egress, pol.EgressRules
			}
			if !applies {
				continue
			}
			selects, ok := pol.Selects(p.Labels)
			if !ok {
				known = false
				continue
			}
			if !selects {
				continue
			}
			restricted = true
			if rules > 0 {
				allowed = true
			}
			names = append(names, pol.Name)
		}
		if known && restricted && !allowed {
			cut = append(cut, p)
			blockers = append(blockers, names...)
		}
	}
	if len(cut) == 0 {
		return Diagnosis{}, false
	}
	blockers = unique(blockers)

	direction, rule := "incoming", "traffic.ingressdenied"
	severity, what := ingressImpact(&in.App, len(cut))
	if !ingress {
		direction, rule, severity = "outgoing", "traffic.egressdenied", Warning
		if len(in.App.Services) > 0 || len(in.App.Ingresses) > 0 {
			severity = Critical
		}
		what = agree(len(cut), "it", "they") + " cannot reach anything, not even DNS, so every lookup and outbound call fails"
	}
	first := cut[0]
	d := Diagnosis{
		Rule:     rule,
		Severity: severity,
		Subject:  podRef(first),
		Problem:  "NetworkPolicy blocks all " + direction + " traffic for " + podsPhrase(len(cut)),
		Cause: "NetworkPolicy/" + strings.Join(blockers, ", NetworkPolicy/") + " " + agree(len(blockers), "selects", "select") +
			" these pods and no policy allows any " + direction + " traffic; " + what,
		Unknown:    "Whether the cluster's network plugin enforces NetworkPolicies is not visible from the API; without enforcement these policies have no effect.",
		Confidence: Medium,
		Chain:      chain(&in.App, "Pods", "NetworkPolicy/"+blockers[0], "all "+direction+" denied"),
		Suggestions: []Suggestion{
			{Text: "Read the policies that select these pods",
				Command: "kubectl get networkpolicy -n " + first.Namespace + " " + strings.Join(blockers, " ") + " -o yaml"},
			{Text: "Add a policy that allows the traffic this application needs, or remove the deny-all if it is a leftover"},
		},
	}
	for _, name := range blockers {
		d.Evidence = append(d.Evidence, Evidence{Kind: "NetworkPolicy", Name: name, Detail: "restricts " + direction + " traffic with no allow rules"})
	}
	for _, p := range limitPods(cut) {
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: "selected, nothing allowed"})
	}
	return d, true
}

// ingressImpact weighs denied incoming traffic by who is meant to arrive.
// Default-deny is ordinary hardening: a worker that pulls from a queue or an
// operator that watches the API server takes no connections, and cutting off
// what nobody sends is not a fault. It matters as far as something is set up
// to send it: a Service means in-cluster callers time out, an Ingress means
// users do.
func ingressImpact(a *application.Application, pods int) (Severity, string) {
	them := agree(pods, "it", "them")
	switch {
	case len(a.Ingresses) > 0:
		return Critical, "nothing outside the pod can reach " + them + ", including the ingress controller behind Ingress/" + a.Ingresses[0].Name
	case len(a.Services) > 0:
		return Warning, "Service/" + a.Services[0].Name + " routes to " + them + ", but every connection through it is dropped; " +
			"if nothing calls that Service, it is the Service that is the leftover"
	}
	return Info, "no Service or Ingress points at " + them + ", so this looks like deliberate isolation of a client that takes no connections"
}

// certificates reports cert-manager certificates behind the application's
// ingresses that are failing or about to run out.
func certificates(in *Input) []Diagnosis {
	if len(in.Context.Certificates) == 0 {
		return nil
	}
	secrets := map[string]string{}
	for i := range in.App.Ingresses {
		for _, s := range in.App.Ingresses[i].TLSSecrets {
			secrets[s] = in.App.Ingresses[i].Name
		}
	}
	var out []Diagnosis
	for i := range in.Context.Certificates {
		c := &in.Context.Certificates[i]
		ingress, used := secrets[c.SecretName]
		if !used || c.Namespace != in.App.Namespace {
			continue
		}
		if d, ok := certificateFinding(in, c, ingress); ok {
			out = append(out, d)
		}
	}
	return out
}

func certificateFinding(in *Input, c *application.Certificate, ingress string) (Diagnosis, bool) {
	hosts := orUnknown(strings.Join(c.DNSNames, ", "))
	expired := !c.NotAfter.IsZero() && !c.NotAfter.After(in.Now)
	left := c.NotAfter.Sub(in.Now)
	d := Diagnosis{
		Subject:    application.ObjectRef{Kind: "Certificate", Name: c.Name, UID: c.UID},
		Confidence: High,
		Chain:      chain(&in.App, "Ingress/"+ingress, "Secret/"+c.SecretName, "Certificate/"+c.Name),
		Suggestions: []Suggestion{
			{Text: "Read what cert-manager says about the certificate",
				Command: describeCommand("certificate", c.Namespace, c.Name)},
			{Text: "Follow the issuance: requests, ACME orders and challenges",
				Command: "kubectl get certificaterequests,orders,challenges -n " + c.Namespace},
		},
		Evidence: []Evidence{{Kind: "Certificate", Name: c.Name, Detail: certDetail(c)}},
	}
	switch {
	case c.ReadyKnown && !c.Ready:
		d.Rule = "cert.notready"
		d.Severity = Warning
		d.Problem = "Certificate/" + c.Name + " for " + hosts + " is not ready"
		d.Cause = "cert-manager cannot issue it: " + orUnknown(c.Message)
		if c.NotAfter.IsZero() || expired {
			d.Severity = Critical
			d.Problem += ", so HTTPS to " + agree(len(c.DNSNames), "that host", "those hosts") + " fails"
		} else {
			d.Cause += "; the current certificate is still valid for " + days(left)
		}
		if c.Issuer != "" {
			d.Suggestions = append(d.Suggestions, Suggestion{Text: "Check that the issuer itself is ready",
				Command: "kubectl get " + strings.ToLower(strings.SplitN(c.Issuer, "/", 2)[0]) + " -A"})
		}
	case expired:
		d.Rule, d.Severity = "cert.expired", Critical
		d.Problem = "Certificate/" + c.Name + " for " + hosts + " has expired, so browsers refuse HTTPS"
		d.Cause = "it expired " + days(-left) + " ago and was not renewed"
	case c.Ready && !c.NotAfter.IsZero() && left < certWarning:
		d.Rule, d.Severity = "cert.expiring", Warning
		d.Problem = "Certificate/" + c.Name + " for " + hosts + " expires in " + days(left)
		d.Cause = "it reports Ready but is this close to expiry, which means renewal is not happening"
	default:
		return Diagnosis{}, false
	}
	return d, true
}

func certDetail(c *application.Certificate) string {
	parts := []string{"secret " + c.SecretName}
	if c.ReadyKnown {
		ready := "Ready=" + boolWord(c.Ready)
		if c.Reason != "" {
			ready += " (" + c.Reason + ")"
		}
		parts = append(parts, ready)
	}
	if !c.NotAfter.IsZero() {
		parts = append(parts, "notAfter "+c.NotAfter.UTC().Format("2006-01-02 15:04 MST"))
	}
	if c.Issuer != "" {
		parts = append(parts, "issuer "+c.Issuer)
	}
	return strings.Join(parts, "; ")
}

func days(d time.Duration) string {
	n := int(d.Hours() / 24)
	switch {
	case n >= 2:
		return strconv.Itoa(n) + " days"
	case d >= time.Hour:
		return strconv.Itoa(int(d.Hours())) + " hours"
	default:
		return "less than an hour"
	}
}

// gitOps reports the Flux object that deploys the application when it is not
// applying anything: a push to Git does nothing until this is resolved.
func gitOps(in *Input) []Diagnosis {
	m := in.App.Manager
	if m.Tool != "Flux" {
		return nil
	}
	for i := range in.Context.GitOps {
		g := &in.Context.GitOps[i]
		if g.Kind != m.Kind || g.Name != m.Name || g.Namespace != m.Namespace {
			continue
		}
		ref := application.ObjectRef{Kind: g.Kind, Name: g.Name, UID: g.UID}
		suggestions := []Suggestion{
			{Text: "Read the controller's conditions and events", Command: describeCommand(g.Kind, g.Namespace, g.Name)},
			{Text: "See the state of every Flux object at once", Command: "flux get all -n " + g.Namespace},
		}
		evidence := []Evidence{{Kind: g.Kind, Name: g.Name, Detail: gitOpsDetail(g)}}
		switch {
		case g.Suspended:
			return []Diagnosis{{
				Rule: "gitops.suspended", Severity: Info, Subject: ref, Confidence: High,
				Problem:     g.Kind + "/" + g.Name + " is suspended, so changes in Git are not applied",
				Cause:       "spec.suspend is true; Flux leaves the cluster as it is until reconciliation is resumed",
				Chain:       chain(&in.App, g.Kind+"/"+g.Name, "suspended"),
				Suggestions: append(suggestions, Suggestion{Text: "Resume it when the pause is no longer wanted", Command: "flux resume " + strings.ToLower(g.Kind) + " -n " + g.Namespace + " " + g.Name}),
				Evidence:    evidence,
			}}
		case g.ReadyKnown && !g.Ready:
			return []Diagnosis{{
				Rule: "gitops.notready", Severity: Warning, Subject: ref, Confidence: High,
				Problem:     g.Kind + "/" + g.Name + ", which deploys this application, is failing to reconcile",
				Cause:       orUnknown(g.Message),
				Unknown:     "The application may still run the last revision that applied; what is in Git is not what is in the cluster.",
				Chain:       chain(&in.App, g.Kind+"/"+g.Name, orUnknown(g.Reason)),
				Suggestions: suggestions,
				Evidence:    evidence,
			}}
		}
		return nil
	}
	return nil
}

func gitOpsDetail(g *application.GitOpsObject) string {
	parts := []string{}
	if g.ReadyKnown {
		ready := "Ready=" + boolWord(g.Ready)
		if g.Reason != "" {
			ready += " (" + g.Reason + ")"
		}
		parts = append(parts, ready)
	}
	if g.Suspended {
		parts = append(parts, "suspended")
	}
	if g.Revision != "" {
		parts = append(parts, "last applied "+g.Revision)
	}
	return orUnknown(strings.Join(parts, "; "))
}

// helmRelease reports a Helm release whose last operation failed or never
// finished. A release stuck in pending-upgrade refuses every further upgrade
// with "another operation is in progress", which is the message people search
// for at 3 a.m.
func helmRelease(in *Input) []Diagnosis {
	m := in.App.Manager
	name, namespace := m.Name, m.Namespace
	switch {
	case m.Tool == "Helm":
	case m.Tool == "Flux" && m.Kind == "HelmRelease":
		// Flux names the Helm release after the HelmRelease unless told
		// otherwise; anything else is not this application's release.
	default:
		return nil
	}
	if name == "" {
		return nil
	}
	for i := range in.Context.Releases {
		r := &in.Context.Releases[i]
		if r.Name != name || (namespace != "" && r.Namespace != namespace && r.Namespace != in.App.Namespace) {
			continue
		}
		ref := application.ObjectRef{Kind: "HelmRelease", Name: r.Name}
		history := Suggestion{Text: "Read the release's history: which revision is deployed and which one failed",
			Command: "helm history " + r.Name + " -n " + r.Namespace}
		evidence := []Evidence{{Kind: "Helm release", Name: r.Name, Detail: "revision " + strconv.Itoa(r.Revision) + " is " + r.Status, At: r.UpdatedAt}}
		switch {
		case r.Status == "failed":
			return []Diagnosis{{
				Rule: "helm.failed", Severity: Warning, Subject: ref, Confidence: High, Evidence: evidence,
				Problem: "Helm release " + r.Name + " revision " + strconv.Itoa(r.Revision) + " failed",
				Cause:   "Helm recorded the last install or upgrade as failed; resources may be a mix of the previous revision and this one",
				Chain:   chain(&in.App, "Helm release/"+r.Name, "revision "+strconv.Itoa(r.Revision), "failed"),
				Suggestions: []Suggestion{history,
					{Text: "Read what the failed revision left behind", Command: "helm status " + r.Name + " -n " + r.Namespace}},
			}}
		case strings.HasPrefix(r.Status, "pending-") || r.Status == "uninstalling":
			if !r.UpdatedAt.IsZero() && in.Now.Sub(r.UpdatedAt) < helmStuck {
				return nil
			}
			return []Diagnosis{{
				Rule: "helm.stuck", Severity: Warning, Subject: ref, Confidence: High, Evidence: evidence,
				Problem: "Helm release " + r.Name + " is stuck in " + r.Status,
				Cause:   "the operation that set " + r.Status + " never finished, and Helm refuses every further upgrade with \"another operation is in progress\" until it is resolved",
				Unknown: "Whether the operation is still running somewhere, for example in a CI job, is not visible from the cluster.",
				Chain:   chain(&in.App, "Helm release/"+r.Name, r.Status),
				Suggestions: []Suggestion{history,
					{Text: "Once nothing is running it any more, roll back to the last deployed revision, which clears the lock"}},
			}}
		}
		return nil
	}
	return nil
}

func quote(s string) string { return "\"" + s + "\"" }

func orNone(s string) string {
	if s == "" {
		return "no ports"
	}
	return s
}

func unique(list []string) []string {
	seen := map[string]bool{}
	out := list[:0]
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
