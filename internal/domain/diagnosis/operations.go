package diagnosis

import (
	"strconv"
	"strings"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// stuckAfter is how long past its grace period a pod may take to disappear
// before it is called stuck rather than slow.
const stuckAfter = 5 * time.Minute

// autoscaling reports a HorizontalPodAutoscaler that cannot do its job: it is
// blind (cannot read the metric it scales on) or capped (wants more replicas
// than it is allowed). Both look like "the application is slow under load".
func autoscaling(in *Input) []Diagnosis {
	var out []Diagnosis
	for i := range in.Context.Autoscalers {
		h := &in.Context.Autoscalers[i]
		w, ok := targetOf(&in.App, h)
		if !ok {
			continue
		}
		ref := application.ObjectRef{Kind: "HorizontalPodAutoscaler", Name: h.Name, UID: h.UID}
		describe := Suggestion{Text: "Read the autoscaler's conditions and the metrics it sees",
			Command: describeCommand("hpa", h.Namespace, h.Name)}
		evidence := []Evidence{{Kind: "HorizontalPodAutoscaler", Name: h.Name, Detail: hpaDetail(h)}}

		if c, ok := h.Conditions["ScalingActive"]; ok && c.Status == "False" {
			cause := orUnknown(c.Message)
			suggestions := []Suggestion{describe}
			if strings.Contains(strings.ToLower(c.Message), "missing request") {
				suggestions = append([]Suggestion{{Text: "Set a CPU or memory request on every container: utilisation is a percentage of the request"}}, suggestions...)
			} else {
				suggestions = append(suggestions, Suggestion{Text: "Check that the metrics API answers", Command: "kubectl get apiservices v1beta1.metrics.k8s.io"})
			}
			out = append(out, Diagnosis{
				Rule: "hpa.blind", Severity: Warning, Subject: ref, Confidence: High,
				Problem:     "HPA/" + h.Name + " cannot read its metrics, so " + w.Kind + "/" + w.Name + " is not being scaled",
				Cause:       cause,
				Chain:       chain(&in.App, "HPA/"+h.Name, orUnknown(c.Reason)),
				Suggestions: suggestions,
				Evidence:    evidence,
			})
			continue
		}
		if c, ok := h.Conditions["ScalingLimited"]; ok && c.Status == "True" && c.Reason == "TooManyReplicas" && h.Current >= h.Max {
			out = append(out, Diagnosis{
				Rule: "hpa.maxed", Severity: Warning, Subject: ref, Confidence: High,
				Problem: "HPA/" + h.Name + " is at its maximum of " + strconv.Itoa(int(h.Max)) + " replicas and wants more",
				Cause:   "the load is above what " + strconv.Itoa(int(h.Max)) + " replicas can carry at the target utilisation; " + orUnknown(c.Message),
				Unknown: "Whether the load is legitimate or a retry storm is not visible from the autoscaler.",
				Chain:   chain(&in.App, "HPA/"+h.Name, "maxReplicas "+strconv.Itoa(int(h.Max))),
				Suggestions: []Suggestion{describe,
					{Text: "Compare current usage with the requests the utilisation is measured against", Command: "kubectl top pods -n " + h.Namespace}},
				Evidence: evidence,
			})
		}
	}
	return out
}

func targetOf(app *application.Application, h *application.Autoscaler) (*application.Workload, bool) {
	for i := range app.Workloads {
		w := &app.Workloads[i]
		if w.Kind == h.TargetKind && w.Name == h.TargetName && w.Namespace == h.Namespace {
			return w, true
		}
	}
	return nil, false
}

func hpaDetail(h *application.Autoscaler) string {
	parts := []string{strconv.Itoa(int(h.Current)) + " current, " + strconv.Itoa(int(h.Desired)) + " desired, " +
		strconv.Itoa(int(h.Min)) + "–" + strconv.Itoa(int(h.Max)) + " allowed"}
	for _, t := range []string{"AbleToScale", "ScalingActive", "ScalingLimited"} {
		if c, ok := h.Conditions[t]; ok {
			parts = append(parts, t+"="+c.Status+" ("+orUnknown(c.Reason)+")")
		}
	}
	return strings.Join(parts, "; ")
}

// disruptionBudget reports a PodDisruptionBudget that allows no voluntary
// eviction of the application's pods. Nothing is broken until somebody drains
// a node — a cluster upgrade, a scale-down — and then the drain waits forever.
func disruptionBudget(in *Input) []Diagnosis {
	pods := sortedNames(livePods(&in.App))
	if len(pods) == 0 {
		return nil
	}
	var out []Diagnosis
	for i := range in.Context.Budgets {
		b := &in.Context.Budgets[i]
		if b.Namespace != in.App.Namespace || b.DisruptionsAllowed > 0 || b.ExpectedPods == 0 {
			continue
		}
		covered := 0
		for _, p := range pods {
			if selects, known := b.Selects(p.Labels); known && selects {
				covered++
			}
		}
		if covered == 0 {
			continue
		}
		severity, cause := Info, "it requires "+strconv.Itoa(int(b.DesiredHealthy))+" healthy pods and there are "+strconv.Itoa(int(b.ExpectedPods))+
			", so not one may be evicted; a node drain or cluster upgrade that touches them waits until it times out"
		if b.CurrentHealthy < b.DesiredHealthy {
			severity = Warning
			cause = "only " + strconv.Itoa(int(b.CurrentHealthy)) + " of the " + strconv.Itoa(int(b.DesiredHealthy)) +
				" pods it requires are healthy, so nothing may be evicted until the application recovers"
		}
		out = append(out, Diagnosis{
			Rule: "pdb.blocking", Severity: severity, Confidence: High,
			Subject: application.ObjectRef{Kind: "PodDisruptionBudget", Name: b.Name, UID: b.UID},
			Problem: "PodDisruptionBudget/" + b.Name + " allows no disruptions, so node drains block on " + podsPhrase(covered),
			Cause:   cause,
			Chain:   chain(&in.App, "PodDisruptionBudget/"+b.Name, "0 disruptions allowed"),
			Suggestions: []Suggestion{
				{Text: "Read the budget and what it requires", Command: describeCommand("pdb", b.Namespace, b.Name)},
				{Text: "Allow at least one disruption: run more replicas than minAvailable, or use maxUnavailable: 1"},
			},
			Evidence: []Evidence{{Kind: "PodDisruptionBudget", Name: b.Name, Detail: strconv.Itoa(int(b.CurrentHealthy)) + " healthy, " +
				strconv.Itoa(int(b.DesiredHealthy)) + " required, " + strconv.Itoa(int(b.ExpectedPods)) + " expected, 0 disruptions allowed"}},
		})
	}
	return out
}

// jobFailed reports a Job its controller gave up on.
func jobFailed(in *Input) []Diagnosis {
	var out []Diagnosis
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		if w.Kind != "Job" || !w.Failed {
			continue
		}
		cause := orUnknown(w.FailedMessage)
		switch w.FailedReason {
		case "BackoffLimitExceeded":
			cause = "its pods failed more often than backoffLimit allows; the reason is in the last pod's logs"
		case "DeadlineExceeded":
			cause = "it ran longer than activeDeadlineSeconds and was stopped"
		}
		out = append(out, Diagnosis{
			Rule: "job.failed", Severity: Critical, Confidence: High,
			Subject: application.ObjectRef{Kind: "Job", Name: w.Name, UID: w.UID},
			Problem: "Job/" + w.Name + " failed and will not be retried",
			Cause:   cause,
			Chain:   chain(&in.App, "Job/"+w.Name, orUnknown(w.FailedReason)),
			Suggestions: []Suggestion{
				{Text: "Read the logs of the job's pods", Command: "kubectl logs -n " + w.Namespace + " job/" + w.Name},
				{Text: "Read the job's conditions and events", Command: describeCommand("job", w.Namespace, w.Name)},
			},
			Evidence: []Evidence{{Kind: "Job", Name: w.Name, Detail: "Failed: " + orUnknown(w.FailedReason) + " — " + orUnknown(w.FailedMessage)}},
		})
	}
	return out
}

// stuckTerminating reports pods that were deleted and are still there long
// after their grace period: a finalizer nobody removes, or a node that is gone
// and cannot confirm the containers stopped.
func stuckTerminating(in *Input) []Diagnosis {
	var pods []*application.Pod
	for _, p := range sortedNames(livePods(&in.App)) {
		if !p.DeletingSince.IsZero() && in.Now.Sub(p.DeletingSince) > p.Grace+stuckAfter {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}
	first := pods[0]
	cause, confidence := "the pod's processes have not confirmed they stopped", Medium
	unknown := "Kubernetes does not say what is holding the pod beyond its finalizers and its node's state."
	switch {
	case len(first.Finalizers) > 0:
		cause, confidence, unknown = "finalizer "+strings.Join(first.Finalizers, ", ")+" has not been removed, and the pod stays until whatever owns it does so", High, ""
	case first.Node != "":
		if node, ok := in.Context.Node(first.Node); ok && !node.Ready {
			cause, confidence, unknown = "node "+first.Node+" is not ready, so nothing can confirm the containers stopped", High, ""
		}
	}
	d := Diagnosis{
		Rule: "pod.terminating", Severity: Warning, Confidence: confidence,
		Subject: podRef(first),
		Problem: podsPhrase(len(pods)) + agree(len(pods), " has", " have") + " been terminating for " + days(in.Now.Sub(first.DeletingSince)),
		Cause:   cause,
		Unknown: unknown,
		Chain:   chain(&in.App, "Pods", "Terminating"),
		Suggestions: []Suggestion{
			{Text: "See what the pod is waiting for",
				Command: "kubectl get pod -n " + first.Namespace + " " + first.Name + " -o jsonpath='{.metadata.finalizers} {.spec.nodeName}'"},
			{Text: "Force deletion is safe only once you know the node is gone or the processes are stopped; otherwise two copies can run"},
		},
	}
	for _, p := range limitPods(pods) {
		detail := "deleting since " + p.DeletingSince.UTC().Format("15:04 MST") + " on " + orUnknown(p.Node)
		if len(p.Finalizers) > 0 {
			detail += "; finalizers " + strings.Join(p.Finalizers, ", ")
		}
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: detail})
	}
	return []Diagnosis{d}
}
