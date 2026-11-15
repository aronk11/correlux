package diagnosis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/aronk11/correlux/internal/domain/application"
)

// DependencyRule is the rule that points at another application, so a caller
// can find that finding among the rest.
const DependencyRule = "dependency.unhealthy"

// dependencies reports an application that points at another one which is
// itself unwell.
//
// It is the one rule that looks beyond the application, and it claims
// exactly what the cluster shows: this application's configuration names
// that one, and that one is down. Whether one caused the other is not
// recorded anywhere, so the finding never outranks what the pods themselves
// report, and it never changes how unhealthy the application is counted —
// an application that still serves with its cache gone is not down.
func dependencies(in *Input) []Diagnosis {
	if len(in.App.DependsOn) == 0 || len(in.Apps) == 0 {
		return nil
	}
	byKey := make(map[string]*application.Application, len(in.Apps))
	for i := range in.Apps {
		byKey[in.Apps[i].Key()] = &in.Apps[i]
	}
	unwell := in.App.Health == application.Down || in.App.Health == application.Degraded

	var out []Diagnosis
	for _, dep := range in.App.DependsOn {
		other, ok := byKey[dep.App]
		if !ok || (other.Health != application.Down && other.Health != application.Degraded) {
			continue
		}
		name := dep.Name
		if dep.Namespace != in.App.Namespace {
			name = dep.Namespace + "/" + dep.Name
		}
		d := Diagnosis{
			Rule:     DependencyRule,
			Severity: Info,
			Subject:  application.ObjectRef{Kind: "Application", Name: dep.Name},
			Problem:  "points at " + name + ", which is " + other.Health.String(),
			Cause:    name + ": " + other.Summary,
			Unknown: "Whether " + in.App.Name + " fails because of it: the cluster shows the configuration, " +
				"not the traffic.",
			Confidence: Low,
			Chain:      chain(&in.App, dep.Via, "Application/"+name, other.Health.String()),
			Suggestions: []Suggestion{{
				Text: "Open " + name + " and ask why it is " + other.Health.String() + " first",
			}},
			Evidence: []Evidence{
				{Kind: "Application", Name: in.App.Name, Detail: dep.Via + viaService(dep)},
				{Kind: "Application", Name: name, Detail: other.Health.String() + ": " + other.Summary},
			},
		}
		if unwell {
			// Both are failing at once, and one names the other: that is worth
			// a warning and a better-than-low confidence, and still not proof.
			d.Severity, d.Confidence = Warning, Medium
			d.Problem = "depends on " + name + ", which is " + other.Health.String()
			d.Unknown = "Whether that is why " + in.App.Name + " is failing: the cluster shows that it points there, " +
				"not that the calls fail."
		}
		if problems := other.ProblemSummary(); problems != "" {
			d.Cause += " (" + problems + ")"
		}
		out = append(out, d)
	}
	return out
}

func viaService(dep application.Dependency) string {
	if dep.Service == "" {
		return ""
	}
	return " names Service/" + dep.Service
}

// placement reports replicas that all sit on one node or in one zone.
//
// Nothing is broken when it fires, which is the point of saying it before
// something is: three replicas on one machine are one drain away from zero,
// and "3/3 healthy" hides that completely. It is informational, and it stays
// quiet where there was no other choice — a cluster with one node, or one
// zone.
func placement(in *Input) []Diagnosis {
	if len(in.Context.Nodes) < 2 {
		return nil
	}
	zoneOf := map[string]string{}
	zones := map[string]bool{}
	schedulable := 0
	for i := range in.Context.Nodes {
		n := &in.Context.Nodes[i]
		if z := n.Zone(); z != "" {
			zoneOf[n.Name] = z
			zones[z] = true
		}
		if n.Ready && !n.Unschedulable {
			schedulable++
		}
	}

	var out []Diagnosis
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		if !w.Replicated || w.Kind == "DaemonSet" || w.Desired < 2 {
			continue
		}
		var pods []*application.Pod
		for _, p := range sortedNames(livePods(&in.App)) {
			if p.Node != "" && p.Phase == "Running" && belongsTo(&in.App, w, p) {
				pods = append(pods, p)
			}
		}
		if len(pods) < 2 {
			continue
		}
		nodes, inZones, zoned := map[string]bool{}, map[string]bool{}, true
		spread := false
		for _, p := range pods {
			nodes[p.Node] = true
			if z, ok := zoneOf[p.Node]; ok {
				inZones[z] = true
			} else {
				zoned = false
			}
			spread = spread || p.Spread
		}

		var where, loss string
		switch {
		case len(nodes) == 1 && schedulable >= 2:
			where, loss = "on node "+pods[0].Node, "a drain or failure of that node takes all of them at once"
		case zoned && len(inZones) == 1 && len(zones) >= 2:
			where = "in zone " + zoneOf[pods[0].Node]
			loss = "losing that zone takes all of them, while the cluster has " +
				strconv.Itoa(len(zones)) + " zones to spread over"
		default:
			continue
		}

		cause := "the pod template has no topology spread constraint or anti-affinity, so the scheduler is free to place them together"
		suggestion := "Add a topologySpreadConstraint over kubernetes.io/hostname and topology.kubernetes.io/zone"
		if spread {
			cause = "the pod template asks for spreading, and the scheduler placed them together anyway: " +
				"the rule is a preference (ScheduleAnyway, preferred anti-affinity) or its selector does not match these pods"
			suggestion = "Check whether the spread rule is required or only preferred, and that its labelSelector matches the pods"
		}
		d := Diagnosis{
			Rule:       "placement.concentrated",
			Severity:   Info,
			Confidence: High,
			Subject:    application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
			Problem:    "all " + strconv.Itoa(len(pods)) + " pods of " + w.Kind + "/" + w.Name + " run " + where,
			Cause:      loss + "; " + cause,
			Chain:      chain(&in.App, "Pods", strings.TrimPrefix(strings.TrimPrefix(where, "on "), "in ")),
			Suggestions: []Suggestion{
				{Text: suggestion, Command: "kubectl get pods -n " + w.Namespace + " -o wide"},
			},
		}
		for _, p := range limitPods(pods) {
			detail := "on " + p.Node
			if z := zoneOf[p.Node]; z != "" {
				detail += " (" + z + ")"
			}
			d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: detail})
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject.Name < out[j].Subject.Name })
	return out
}

// belongsTo reports whether a pod is one of a workload's.
func belongsTo(app *application.Application, w *application.Workload, p *application.Pod) bool {
	if len(app.Workloads) == 1 {
		return true
	}
	return len(w.Selector) > 0 && matches(w.Selector, p.Labels)
}
