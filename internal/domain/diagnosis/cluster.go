package diagnosis

import (
	"strconv"
	"strings"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// nodeUnhealthy reports pods sitting on a node that is not in a position to run
// them. It is the explanation people reach for last and should reach for first:
// nothing inside the application is wrong.
func nodeUnhealthy(in *Input) []Diagnosis {
	if len(in.Context.Nodes) == 0 {
		return nil
	}

	byNode := map[string][]*application.Pod{}
	for _, p := range sortedNames(livePods(in.App)) {
		if p.Node != "" {
			byNode[p.Node] = append(byNode[p.Node], p)
		}
	}

	out := make([]Diagnosis, 0, len(byNode))
	for _, name := range sortedKeys(byNode) {
		node, ok := in.Context.Node(name)
		if !ok || (node.Ready && len(node.Pressure) == 0 && !node.Unschedulable) {
			continue
		}
		pods := byNode[name]

		var (
			severity   = Warning
			problem    string
			cause      string
			confidence = High
		)
		switch {
		case !node.Ready:
			severity = Critical
			problem = podsPhrase(len(pods)) + " run on node " + name + ", which is not ready"
			cause = "the node stopped reporting as ready"
			if node.Message != "" {
				cause = node.Message
			}
		case len(node.Pressure) > 0:
			problem = podsPhrase(len(pods)) + " run on node " + name + ", which is under " +
				strings.Join(node.Pressure, " and ")
			cause = "the node is short of a resource and may evict pods to recover it"
		default:
			// Cordoned: nothing is wrong yet, but no replacement pod will land
			// here, which is worth knowing during a rollout.
			severity = Info
			problem = podsPhrase(len(pods)) + " run on node " + name + ", which is cordoned"
			cause = "the node is marked unschedulable, so replacements will be placed elsewhere"
		}

		d := Diagnosis{
			Rule:       "node.unhealthy",
			Severity:   severity,
			Subject:    application.ObjectRef{Kind: "Node", Name: name, UID: node.UID},
			Problem:    problem,
			Cause:      cause,
			Confidence: confidence,
			Chain:      chain(in.App, "Pods", "Node/"+name),
			Suggestions: []Suggestion{
				{Text: "Check the node's conditions and what the kubelet reports",
					Command: describeCommand("node", "", name)},
			},
			Evidence: []Evidence{{
				Kind:   "Node",
				Name:   name,
				Detail: nodeDetail(node),
			}},
		}
		for _, p := range limitPods(pods) {
			d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: "scheduled on " + name})
		}
		out = append(out, d)
	}
	return out
}

func nodeDetail(n application.Node) string {
	parts := []string{"Ready=" + boolWord(n.Ready)}
	if len(n.Pressure) > 0 {
		parts = append(parts, strings.Join(n.Pressure, ", "))
	}
	if n.Unschedulable {
		parts = append(parts, "cordoned")
	}
	if n.Reason != "" {
		parts = append(parts, n.Reason)
	}
	return strings.Join(parts, "; ")
}

// storageNotBound reports a pod waiting for a volume that does not exist yet.
// It is a frequent cause of a Pending pod, and the pod itself never says so.
func storageNotBound(in *Input) []Diagnosis {
	if len(in.Context.Claims) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var out []Diagnosis
	for _, p := range sortedNames(livePods(in.App)) {
		for _, name := range p.Claims {
			claim, ok := in.Context.ClaimByName(p.Namespace, name)
			if !ok || claim.Phase == "Bound" || seen[name] {
				continue
			}
			seen[name] = true

			cause, confidence := "no volume has been provisioned for the claim yet", Medium
			events := warningsAbout(in, application.ObjectRef{
				Kind: "PersistentVolumeClaim", Name: claim.Name, UID: claim.UID,
			})
			if len(events) > 0 {
				cause, confidence = events[0].Message, High
			}
			if claim.Phase == "Lost" {
				cause = "the volume the claim was bound to is gone"
				confidence = High
			}

			d := Diagnosis{
				Rule:     "storage.unbound",
				Severity: Critical,
				Subject: application.ObjectRef{
					Kind: "PersistentVolumeClaim", Name: claim.Name, UID: claim.UID,
				},
				Problem:    "PersistentVolumeClaim " + claim.Name + " is " + strings.ToLower(orUnknown(claim.Phase)) + ", so the pods that mount it cannot start",
				Cause:      cause,
				Confidence: confidence,
				Chain:      chain(in.App, "Pods", "PersistentVolumeClaim/"+claim.Name, claim.Phase),
				Suggestions: []Suggestion{
					{Text: "Check the storage class and whether a provisioner is running",
						Command: describeCommand("pvc", claim.Namespace, claim.Name)},
				},
				Evidence: []Evidence{{
					Kind: "PersistentVolumeClaim", Name: claim.Name,
					Detail: "phase " + orUnknown(claim.Phase) + ", storage class " + orUnknown(claim.StorageClass),
				}},
			}
			for _, e := range limitEvents(events) {
				d.Evidence = append(d.Evidence, eventEvidence(e))
			}
			out = append(out, d)
		}
	}
	return out
}

// replicasMissing reports a workload the controller has not filled, when no pod
// explains it. If pods exist and are broken, the pod rules have already said
// more than this one can.
func replicasMissing(in *Input) []Diagnosis {
	out := make([]Diagnosis, 0, len(in.App.Workloads))
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		if !w.Replicated || w.Desired == 0 || w.Ready >= w.Desired {
			continue
		}
		running := podsOf(in.App, w)
		if running >= int(w.Desired) {
			// The pods exist; whatever is wrong with them is a pod problem.
			continue
		}

		missing := int(w.Desired) - running
		cause, confidence := "the controller has not created every pod yet, which is normal during a rollout", Low
		events := warningsAbout(in, application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
			"FailedCreate", "FailedGet")
		if len(events) > 0 {
			cause, confidence = events[0].Message, High
		}

		severity := Warning
		if w.Ready == 0 {
			severity = Critical
		}
		d := Diagnosis{
			Rule:       "workload.replicas",
			Severity:   severity,
			Subject:    application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
			Problem:    w.Kind + "/" + w.Name + " is missing " + strconv.Itoa(missing) + " of " + strconv.Itoa(int(w.Desired)) + " pods",
			Cause:      cause,
			Confidence: confidence,
			Chain:      chain(in.App, "ReplicaSet", "pods not created"),
			Suggestions: []Suggestion{
				{Text: "Check the controller's events for quota, limit ranges or admission webhooks",
					Command: describeCommand(w.Kind, w.Namespace, w.Name)},
			},
			Evidence: []Evidence{{
				Kind: w.Kind, Name: w.Name,
				Detail: strconv.Itoa(int(w.Ready)) + " of " + strconv.Itoa(int(w.Desired)) +
					" ready, " + strconv.Itoa(running) + " pods exist",
			}},
		}
		for _, e := range limitEvents(events) {
			d.Evidence = append(d.Evidence, eventEvidence(e))
		}
		out = append(out, d)
	}
	return out
}

// rolloutPaused states a deliberate state rather than a fault: nothing is
// broken, and nothing will change either, which is exactly what surprises
// somebody waiting for a fix to roll out.
func rolloutPaused(in *Input) []Diagnosis {
	var out []Diagnosis
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		switch {
		case w.Paused:
			out = append(out, Diagnosis{
				Rule:       "workload.paused",
				Severity:   Info,
				Subject:    application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
				Problem:    w.Kind + "/" + w.Name + " has its rollout paused",
				Cause:      "somebody paused it; changes to the pod template will not be applied until it is resumed",
				Confidence: High,
				Chain:      chain(in.App, "rollout paused"),
				Suggestions: []Suggestion{
					{Text: "Resume the rollout when the pause is no longer wanted",
						Command: "kubectl rollout resume " + strings.ToLower(w.Kind) + " -n " + w.Namespace + " " + w.Name},
				},
			})
		case w.Suspended:
			out = append(out, Diagnosis{
				Rule:       "workload.suspended",
				Severity:   Info,
				Subject:    application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
				Problem:    w.Kind + "/" + w.Name + " is suspended",
				Cause:      "it will not run again until it is resumed",
				Confidence: High,
				Chain:      chain(in.App, "suspended"),
			})
		}
	}
	return out
}

// podsOf counts the application's pods that belong to one workload, by owner or
// by the workload's own selector.
func podsOf(app application.Application, w *application.Workload) int {
	count := 0
	for i := range app.Pods {
		p := &app.Pods[i]
		if p.Terminal() {
			continue
		}
		if len(w.Selector) > 0 && matches(w.Selector, p.Labels) {
			count++
			continue
		}
		if ref, ok := p.Controller(); ok && ref.Name != "" && strings.HasPrefix(ref.Name, w.Name) {
			count++
		}
	}
	return count
}

func matches(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func sortedKeys(m map[string][]*application.Pod) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
