package diagnosis

import (
	"strconv"
	"strings"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// affected pairs a pod with the container that is failing inside it.
type affected struct {
	pod       *application.Pod
	container *application.Container
}

// findContainers collects every container matching a predicate, across the
// application's live pods.
func findContainers(app application.Application, match func(*application.Container) bool) []affected {
	var out []affected
	for _, p := range sortedNames(livePods(app)) {
		for i := range p.Containers {
			if match(&p.Containers[i]) {
				out = append(out, affected{pod: p, container: &p.Containers[i]})
			}
		}
	}
	return out
}

func waitingFor(reasons ...string) func(*application.Container) bool {
	return func(c *application.Container) bool {
		return c.State == "waiting" && contains(reasons, c.Reason)
	}
}

// crashLoop explains containers that keep restarting.
//
// The pod's own status only says "CrashLoopBackOff", which is a symptom: the
// container is waiting to be restarted. What explains it is how the *previous*
// run ended, which is why the last termination state is collected at all.
func crashLoop(in *Input) []Diagnosis {
	hits := findContainers(in.App, waitingFor("CrashLoopBackOff"))
	if len(hits) == 0 {
		return nil
	}

	cause, confidence := crashCause(hits)
	first := hits[0]
	d := Diagnosis{
		Rule:       "pod.crashloop",
		Severity:   Critical,
		Subject:    podRef(first.pod),
		Problem:    podsPhrase(countPods(hits)) + agree(countPods(hits), " restarts", " restart") + " in a loop",
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", "CrashLoopBackOff", crashChainTail(hits)),
		Suggestions: []Suggestion{
			{
				Text:    "Read the logs of the run that failed, not of the one waiting to start",
				Command: logsCommand(first.pod, first.container, true),
			},
			{
				Text:    "Check the container's events and its last state",
				Command: describeCommand("pod", first.pod.Namespace, first.pod.Name),
			},
		},
	}
	for _, h := range limit(hits) {
		d.Evidence = append(d.Evidence, Evidence{
			Kind:   "Pod",
			Name:   h.pod.Name,
			Detail: containerOf(h.container) + " " + lastRun(h.container) + ", restarted " + strconv.Itoa(int(h.container.Restarts)) + " times",
		})
	}
	for _, e := range limitEvents(warningsAbout(in, podRef(first.pod))) {
		d.Evidence = append(d.Evidence, eventEvidence(e))
	}
	return []Diagnosis{d}
}

// crashCause reads the previous run. Only the cluster's own numbers are used:
// an exit code and a reason, never a guess about what the program does.
func crashCause(hits []affected) (string, Confidence) {
	for _, h := range hits {
		if h.container.OOMKilled {
			return "the container is killed for exceeding its memory limit (exit 137)", High
		}
	}
	for _, h := range hits {
		if h.container.LastExitCode > 0 {
			cause := "the container exits with code " + strconv.Itoa(int(h.container.LastExitCode)) +
				" shortly after starting"
			if h.container.LastReason != "" && h.container.LastReason != "Error" {
				cause += " (" + h.container.LastReason + ")"
			}
			return cause, High
		}
	}
	for _, h := range hits {
		if h.container.LastReason == "Completed" || h.container.LastExitCode == 0 && h.container.LastReason != "" {
			return "the container exits successfully and is restarted; a long-running workload's process must not return", High
		}
	}
	return "", Medium
}

func crashChainTail(hits []affected) string {
	for _, h := range hits {
		if h.container.OOMKilled {
			return "OOMKilled"
		}
	}
	for _, h := range hits {
		if h.container.LastExitCode != 0 {
			return "exit " + strconv.Itoa(int(h.container.LastExitCode))
		}
	}
	return "restarting"
}

func lastRun(c *application.Container) string {
	switch {
	case c.OOMKilled:
		return "was killed for exceeding its memory limit"
	case c.LastExitCode != 0:
		return "last exited with code " + strconv.Itoa(int(c.LastExitCode))
	case c.LastReason != "":
		return "last ended: " + c.LastReason
	default:
		return "is waiting to restart"
	}
}

// imagePull explains a container whose image never arrives.
func imagePull(in *Input) []Diagnosis {
	hits := findContainers(in.App,
		waitingFor("ImagePullBackOff", "ErrImagePull", "ErrImageNeverPull", "InvalidImageName"))
	if len(hits) == 0 {
		return nil
	}

	first := hits[0]
	cause, confidence := imageCause(hits)
	d := Diagnosis{
		Rule:       "pod.imagepull",
		Severity:   Critical,
		Subject:    podRef(first.pod),
		Problem:    podsPhrase(countPods(hits)) + " cannot pull " + agree(countPods(hits), "its", "their") + " image",
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", first.container.Reason),
		Suggestions: []Suggestion{
			{Text: "Check the image name and tag in the pod template"},
			{
				Text:    "Check that the namespace has a pull secret for this registry",
				Command: "kubectl get serviceaccount -n " + first.pod.Namespace + " default -o yaml",
			},
		},
	}
	for _, h := range limit(hits) {
		detail := containerOf(h.container) + " wants " + h.container.Image
		if h.container.Message != "" {
			detail += " — " + h.container.Message
		}
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: h.pod.Name, Detail: detail})
	}
	for _, e := range limitEvents(warningsAbout(in, podRef(first.pod), "Failed", "BackOff")) {
		d.Evidence = append(d.Evidence, eventEvidence(e))
	}
	return []Diagnosis{d}
}

// imageCause reads the kubelet's message. The three failures below are the ones
// worth telling apart, because the fix differs: a typo, a credential, a network.
func imageCause(hits []affected) (string, Confidence) {
	for _, h := range hits {
		message := strings.ToLower(h.container.Message)
		switch {
		case h.container.Reason == "InvalidImageName":
			return "the image reference is not a valid name", High
		case strings.Contains(message, "not found"),
			strings.Contains(message, "manifest unknown"),
			strings.Contains(message, "repository does not exist"):
			return "the registry does not have that image or tag", High
		case strings.Contains(message, "unauthorized"),
			strings.Contains(message, "authentication required"),
			strings.Contains(message, "denied"),
			strings.Contains(message, "forbidden"):
			return "the registry refused the pull: the node has no credentials for it", High
		case strings.Contains(message, "no such host"),
			strings.Contains(message, "timeout"),
			strings.Contains(message, "connection refused"),
			strings.Contains(message, "i/o timeout"):
			return "the node cannot reach the registry", High
		}
	}
	return "the image could not be pulled; the kubelet did not say why", Low
}

// containerConfig explains a container the kubelet cannot even create, which is
// almost always a ConfigMap or Secret that is not there.
func containerConfig(in *Input) []Diagnosis {
	hits := findContainers(in.App, waitingFor("CreateContainerConfigError", "CreateContainerError"))
	if len(hits) == 0 {
		return nil
	}

	first := hits[0]
	cause, confidence := "", Medium
	if first.container.Message != "" {
		cause, confidence = first.container.Message, High
	}
	d := Diagnosis{
		Rule:       "pod.configerror",
		Severity:   Critical,
		Subject:    podRef(first.pod),
		Problem:    podsPhrase(countPods(hits)) + " cannot start " + agree(countPods(hits), "its", "their") + " container",
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", first.container.Reason),
		Suggestions: []Suggestion{
			{
				Text:    "Check the ConfigMaps and Secrets the pod template references",
				Command: describeCommand("pod", first.pod.Namespace, first.pod.Name),
			},
		},
	}
	for _, h := range limit(hits) {
		d.Evidence = append(d.Evidence, Evidence{
			Kind: "Pod", Name: h.pod.Name, Detail: containerOf(h.container) + ": " + h.container.Message,
		})
	}
	return []Diagnosis{d}
}

// outOfMemory reports a container that was OOM-killed but is running again, so
// the crash-loop rule does not cover it. It is the quiet version of the same
// problem and disappears from a dashboard that only looks at readiness.
func outOfMemory(in *Input) []Diagnosis {
	hits := findContainers(in.App, func(c *application.Container) bool {
		return c.OOMKilled && (c.State != "waiting" || c.Reason != "CrashLoopBackOff")
	})
	if len(hits) == 0 {
		return nil
	}

	first := hits[0]
	d := Diagnosis{
		Rule:     "pod.oomkilled",
		Severity: Warning,
		Subject:  podRef(first.pod),
		Problem: podsPhrase(countPods(hits)) + agree(countPods(hits), " has", " have") +
			" been killed for exceeding " + agree(countPods(hits), "its", "their") + " memory limit",
		Cause:      "the container's memory use reached its limit and the kernel killed it",
		Confidence: High,
		Chain:      chain(in.App, "Pods", "OOMKilled"),
		Suggestions: []Suggestion{
			{Text: "Compare the container's memory limit with what it actually uses"},
			{
				Text:    "Check whether the limit was lowered or the workload's memory use grew",
				Command: describeCommand("pod", first.pod.Namespace, first.pod.Name),
			},
		},
	}
	for _, h := range limit(hits) {
		d.Evidence = append(d.Evidence, Evidence{
			Kind:   "Pod",
			Name:   h.pod.Name,
			Detail: containerOf(h.container) + " restarted " + strconv.Itoa(int(h.container.Restarts)) + " times, last kill was OOM",
		})
	}
	return []Diagnosis{d}
}

// unschedulable reports pods no node will take. The scheduler already explains
// itself precisely, so the rule quotes it rather than paraphrasing.
func unschedulable(in *Input) []Diagnosis {
	var pods []*application.Pod
	for _, p := range sortedNames(livePods(in.App)) {
		// A pod no node has taken is Pending by definition. Requiring the phase
		// as well keeps the rule from accusing a running pod when the
		// scheduling condition is simply not known.
		if !p.Scheduled && (p.Phase == "Pending" || p.Phase == "") {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}

	cause, confidence := "", Medium
	for _, p := range pods {
		if p.ScheduledMessage != "" {
			cause, confidence = p.ScheduledMessage, High
			break
		}
	}
	d := Diagnosis{
		Rule:       "pod.unschedulable",
		Severity:   Critical,
		Subject:    podRef(pods[0]),
		Problem:    podsPhrase(len(pods)) + " cannot be scheduled onto any node",
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", "Pending", "Unschedulable"),
		Suggestions: []Suggestion{
			{Text: "Compare the pod's resource requests with what the nodes have free",
				Command: "kubectl describe nodes"},
			{Text: "Check node selectors, affinity and taints on the pod template",
				Command: describeCommand("pod", pods[0].Namespace, pods[0].Name)},
		},
	}
	for _, p := range limitPods(pods) {
		detail := p.ScheduledReason
		if detail == "" {
			detail = "not scheduled"
		}
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: detail})
	}
	for _, e := range limitEvents(warningsAbout(in, podRef(pods[0]), "FailedScheduling")) {
		d.Evidence = append(d.Evidence, eventEvidence(e))
	}
	return []Diagnosis{d}
}

// podFailed reports pods that ran and stopped for good: evicted, killed by the
// node, or terminated with an error and not restarted.
func podFailed(in *Input) []Diagnosis {
	var pods []*application.Pod
	for _, p := range sortedNames(livePods(in.App)) {
		if p.Phase == "Failed" {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}

	cause, confidence := "", Low
	if reason := pods[0].Reason; reason != "" {
		confidence = High
		switch reason {
		case "Evicted":
			cause = "the node evicted the pod, which happens when it runs short of memory or disk"
		case "Shutdown", "NodeShutdown":
			cause = "the node was shut down while the pod was running"
		case "NodeAffinity":
			cause = "the pod no longer matches the node it was bound to"
		default:
			cause = "the pod stopped with reason " + reason
		}
	}
	d := Diagnosis{
		Rule:     "pod.failed",
		Severity: Critical,
		Subject:  podRef(pods[0]),
		Problem: podsPhrase(len(pods)) + agree(len(pods), " has", " have") + " failed and " +
			agree(len(pods), "is", "are") + " not running",
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", "Failed"),
		Suggestions: []Suggestion{
			{Text: "Check the node the pods were on and why it removed them",
				Command: describeCommand("pod", pods[0].Namespace, pods[0].Name)},
		},
	}
	for _, p := range limitPods(pods) {
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: "phase Failed, reason " + orUnknown(p.Reason)})
	}
	return []Diagnosis{d}
}

// notReady reports pods that are running and not ready, with nothing else wrong
// with them. That is what a failing readiness probe looks like from outside,
// and the event stream is where the probe says why.
func notReady(in *Input) []Diagnosis {
	var pods []*application.Pod
	for _, p := range sortedNames(livePods(in.App)) {
		if p.Phase == "Running" && !p.Ready && p.Reason == "" {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}

	cause, confidence := "the readiness probe has not succeeded", Low
	var quoted []application.Event
	for _, p := range pods {
		if events := warningsAbout(in, podRef(p), "Unhealthy", "ProbeWarning"); len(events) > 0 {
			cause, confidence = events[0].Message, High
			quoted = events
			break
		}
	}
	d := Diagnosis{
		Rule:     "pod.notready",
		Severity: Warning,
		Subject:  podRef(pods[0]),
		Problem: podsPhrase(len(pods)) + agree(len(pods), " is", " are") +
			" running but not ready, so nothing routes to " + agree(len(pods), "it", "them"),
		Cause:      cause,
		Confidence: confidence,
		Chain:      chain(in.App, "Pods", "not ready"),
		Suggestions: []Suggestion{
			{Text: "Check what the readiness probe asks for and what the container answers",
				Command: describeCommand("pod", pods[0].Namespace, pods[0].Name)},
			{Text: "Read the container's logs from the moment it started",
				Command: logsCommand(pods[0], nil, false)},
		},
	}
	for _, p := range limitPods(pods) {
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: "Running, not ready"})
	}
	for _, e := range limitEvents(quoted) {
		d.Evidence = append(d.Evidence, eventEvidence(e))
	}
	return []Diagnosis{d}
}

// countPods counts distinct pods behind a set of failing containers.
func countPods(hits []affected) int {
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.pod.Name] = true
	}
	return len(seen)
}

func limit(hits []affected) []affected {
	if len(hits) > maxEvidence {
		return hits[:maxEvidence]
	}
	return hits
}

func limitPods(pods []*application.Pod) []*application.Pod {
	if len(pods) > maxEvidence {
		return pods[:maxEvidence]
	}
	return pods
}

func limitEvents(events []application.Event) []application.Event {
	if len(events) > maxEvidence {
		return events[:maxEvidence]
	}
	return events
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
