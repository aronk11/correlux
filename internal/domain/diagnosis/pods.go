package diagnosis

import (
	"strconv"
	"strings"

	"github.com/aronk11/correlux/internal/domain/application"
)

// affected pairs a pod with the container that is failing inside it.
type affected struct {
	pod       *application.Pod
	container *application.Container
}

// findContainers collects every container matching a predicate, across the
// application's live pods.
func findContainers(app *application.Application, match func(*application.Container) bool) []affected {
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

// restarting includes the short terminated window between two backoff waits.
// A crash loop alternates between terminated and CrashLoopBackOff; diagnosing
// only the latter makes the answer disappear depending on refresh timing.
func restarting(c *application.Container) bool {
	return c.State == "waiting" && c.Reason == "CrashLoopBackOff" ||
		c.State == "terminated" && c.Restarts > 0 && c.ExitCode != 0
}

// crashLoop explains containers that keep restarting.
//
// The pod's own status only says "CrashLoopBackOff", which is a symptom: the
// container is waiting to be restarted. What explains it is how the *previous*
// run ended, which is why the last termination state is collected at all.
func crashLoop(in *Input) []Diagnosis {
	hits := findContainers(&in.App, restarting)
	if len(hits) == 0 {
		return nil
	}

	cause, confidence, unknown := crashCause(hits)
	probe := probeKills(in, hits)
	if probe != nil && !anyOOM(hits) {
		cause = "the " + probeKind(probe.Message) + " probe fails, so the kubelet kills the container and restarts it: " +
			strings.TrimSpace(probe.Message[strings.Index(probe.Message, ":")+1:])
		confidence, unknown = High, ""
	}
	first := hits[0]
	d := Diagnosis{
		Rule:       "pod.crashloop",
		Severity:   Critical,
		Subject:    podRef(first.pod),
		Problem:    podsPhrase(countPods(hits)) + agree(countPods(hits), " restarts", " restart") + " in a loop",
		Cause:      cause,
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Pods", "CrashLoopBackOff", crashChainTail(hits, probe)),
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
	if probe != nil && !anyOOM(hits) {
		d.Suggestions = append(d.Suggestions, Suggestion{
			Text: "Compare the probe's path, port and timeouts with how long the container takes to answer",
			Command: "kubectl get events -n " + first.pod.Namespace +
				" --field-selector involvedObject.name=" + probe.About.Name + ",reason=Unhealthy",
		})
	}
	quoted := limitEvents(warningsAbout(in, podRef(first.pod)))
	for i := range quoted {
		d.Evidence = append(d.Evidence, eventEvidence(&quoted[i]))
	}
	return []Diagnosis{d}
}

// probeKills returns the newest event saying a liveness or startup probe
// failed for one of the restarting pods. Those restarts are the kubelet's
// doing, not the process's, and the exit code alone (137, 143) hides that.
func probeKills(in *Input, hits []affected) *application.Event {
	var newest *application.Event
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.pod.Name] {
			continue
		}
		seen[h.pod.Name] = true
		events := warningsAbout(in, podRef(h.pod), "Unhealthy")
		for i := range events {
			if probeKind(events[i].Message) == "" {
				continue
			}
			if newest == nil || events[i].LastSeen.After(newest.LastSeen) {
				e := events[i]
				newest = &e
			}
			break
		}
	}
	return newest
}

// probeKind names the probe an Unhealthy event is about, when it is one that
// restarts the container. A failing readiness probe never does.
func probeKind(message string) string {
	switch {
	case strings.HasPrefix(message, "Liveness probe failed"):
		return "liveness"
	case strings.HasPrefix(message, "Startup probe failed"):
		return "startup"
	}
	return ""
}

func anyOOM(hits []affected) bool {
	for _, h := range hits {
		if h.container.OOMKilled {
			return true
		}
	}
	return false
}

// exitMeaning reads an exit code by the POSIX convention every container
// runtime follows: 126 and 127 come from the shell, and 128+n means the
// process was ended by signal n. It says nothing about the program itself.
func exitMeaning(code int32) string {
	switch code {
	case 126:
		return "the entrypoint exists but cannot be executed (permissions or wrong architecture)"
	case 127:
		return "the entrypoint or a command it runs was not found in the image"
	case 134:
		return "the process aborted itself (SIGABRT)"
	case 137:
		return "the process was killed with SIGKILL, by the kernel, the kubelet or a failed probe"
	case 139:
		return "the process crashed with a segmentation fault (SIGSEGV)"
	case 143:
		return "the process was stopped with SIGTERM"
	}
	if code > 128 && code < 160 {
		return "the process was ended by signal " + strconv.Itoa(int(code-128))
	}
	return ""
}

func exitCause(code int32, reason string) string {
	cause := "the container exits with code " + strconv.Itoa(int(code)) + " shortly after starting"
	if reason != "" && reason != "Error" {
		cause += " (" + reason + ")"
	}
	if meaning := exitMeaning(code); meaning != "" {
		cause += ": " + meaning
	}
	return cause
}

// crashCause reads the previous run. Only the cluster's own numbers are used:
// an exit code and a reason, never a guess about what the program does. The
// numbers themselves are quoted in Evidence; what is returned here is the one
// reasonable reading of them, plus what that reading still cannot explain.
func crashCause(hits []affected) (cause string, confidence Confidence, unknown string) {
	for _, h := range hits {
		if h.container.OOMKilled {
			return "the container exceeded its memory limit and was killed for it", High,
				"Kubernetes does not report why memory use grew; that requires the application's own metrics or logs."
		}
	}
	for _, h := range hits {
		if h.container.LastExitCode > 0 {
			return exitCause(h.container.LastExitCode, h.container.LastReason), High,
				"Kubernetes does not know why the process exited with this code; that is determined by the application, not the cluster."
		}
	}
	for _, h := range hits {
		if h.container.ExitCode > 0 {
			return exitCause(h.container.ExitCode, h.container.Reason), High,
				"Kubernetes does not know why the process exited with this code; that is determined by the application, not the cluster."
		}
	}
	for _, h := range hits {
		if h.container.LastReason == "Completed" || h.container.LastExitCode == 0 && h.container.LastReason != "" {
			return "the container exits successfully and is restarted; a long-running workload's process must not return", High, ""
		}
	}
	return "", Medium, "Kubernetes has not recorded why the previous run ended: there is no exit code or reason to read."
}

func crashChainTail(hits []affected, probe *application.Event) string {
	if anyOOM(hits) {
		return "OOMKilled"
	}
	if probe != nil {
		return probeKind(probe.Message) + " probe failing"
	}
	for _, h := range hits {
		if h.container.LastExitCode != 0 {
			return "exit " + strconv.Itoa(int(h.container.LastExitCode))
		}
	}
	for _, h := range hits {
		if h.container.ExitCode != 0 {
			return "exit " + strconv.Itoa(int(h.container.ExitCode))
		}
	}
	return "restarting"
}

// lastRun quotes what the previous run's own state said, not what it is read
// to mean — that reading belongs in Cause, not in evidence someone might check
// against the object itself.
func lastRun(c *application.Container) string {
	switch {
	case c.OOMKilled:
		reason := c.LastReason
		if reason == "" {
			reason = "OOMKilled"
		}
		return "last terminated with reason " + reason + ", exit code " + strconv.Itoa(int(c.LastExitCode))
	case c.LastExitCode != 0:
		detail := "last exited with code " + strconv.Itoa(int(c.LastExitCode))
		if c.LastReason != "" && c.LastReason != "Error" {
			detail += ", reason " + c.LastReason
		}
		return detail
	case c.LastReason != "":
		return "last ended: " + c.LastReason
	case c.State == "terminated" && c.ExitCode != 0:
		detail := "exited with code " + strconv.Itoa(int(c.ExitCode))
		if c.Reason != "" && c.Reason != "Error" {
			detail += ", reason " + c.Reason
		}
		return detail
	default:
		return "is waiting to restart"
	}
}

// imagePull explains a container whose image never arrives.
func imagePull(in *Input) []Diagnosis {
	hits := findContainers(&in.App,
		waitingFor("ImagePullBackOff", "ErrImagePull", "ErrImageNeverPull", "InvalidImageName"))
	if len(hits) == 0 {
		return nil
	}

	first := hits[0]
	cause, confidence, unknown := imageCause(hits)
	d := Diagnosis{
		Rule:        "pod.imagepull",
		Severity:    Critical,
		Subject:     podRef(first.pod),
		Problem:     podsPhrase(countPods(hits)) + " cannot pull " + agree(countPods(hits), "its", "their") + " image",
		Cause:       cause,
		Unknown:     unknown,
		Confidence:  confidence,
		Chain:       chain(&in.App, "Pods", first.container.Reason),
		Suggestions: imageSuggestions(first, cause),
	}
	for _, h := range limit(hits) {
		detail := containerOf(h.container) + " wants " + h.container.Image
		if h.container.Message != "" {
			detail += " — " + h.container.Message
		}
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: h.pod.Name, Detail: detail})
	}
	quoted := limitEvents(warningsAbout(in, podRef(first.pod), "Failed", "BackOff"))
	for i := range quoted {
		d.Evidence = append(d.Evidence, eventEvidence(&quoted[i]))
	}
	return []Diagnosis{d}
}

func imageSuggestions(first affected, cause string) []Suggestion {
	out := []Suggestion{{
		Text:    "Check the image name and tag in the pod template",
		Command: describeCommand("pod", first.pod.Namespace, first.pod.Name),
	}}
	if strings.Contains(cause, "credentials") || cause == "" {
		out = append(out, Suggestion{
			Text:    "Check that the namespace has a pull secret for this registry",
			Command: "kubectl get serviceaccount -n " + first.pod.Namespace + " default -o yaml",
		})
	}
	return out
}

// imageCause reads the kubelet's message. The three failures below are the ones
// worth telling apart, because the fix differs: a typo, a credential, a network.
func imageCause(hits []affected) (cause string, confidence Confidence, unknown string) {
	for _, h := range hits {
		message := strings.ToLower(h.container.Message)
		switch {
		case h.container.Reason == "InvalidImageName":
			return "the image reference is not a valid name", High, ""
		case strings.Contains(message, "not found"),
			strings.Contains(message, "manifest unknown"),
			strings.Contains(message, "repository does not exist"):
			return "the registry does not have that image or tag", High, ""
		case strings.Contains(message, "unauthorized"),
			strings.Contains(message, "authentication required"),
			strings.Contains(message, "denied"),
			strings.Contains(message, "forbidden"):
			return "the registry refused the pull: the node has no credentials for it", High, ""
		case strings.Contains(message, "no such host"),
			strings.Contains(message, "timeout"),
			strings.Contains(message, "connection refused"),
			strings.Contains(message, "i/o timeout"):
			return "the node cannot reach the registry", High, ""
		}
	}
	return "", Low, "The kubelet did not say why the pull failed: it could be a missing image, an expired credential or a network problem."
}

// containerConfig explains a container the kubelet cannot even create, which is
// almost always a ConfigMap or Secret that is not there.
func containerConfig(in *Input) []Diagnosis {
	hits := findContainers(&in.App, waitingFor("CreateContainerConfigError", "CreateContainerError"))
	if len(hits) == 0 {
		return nil
	}

	first := hits[0]
	cause, confidence, unknown := "", Medium, "The kubelet gave no message about why the container could not be created."
	if first.container.Message != "" {
		cause, confidence, unknown = first.container.Message, High, ""
	}
	d := Diagnosis{
		Rule:       "pod.configerror",
		Severity:   Critical,
		Subject:    podRef(first.pod),
		Problem:    podsPhrase(countPods(hits)) + " cannot start " + agree(countPods(hits), "its", "their") + " container",
		Cause:      cause,
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Pods", first.container.Reason),
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
	hits := findContainers(&in.App, func(c *application.Container) bool {
		return c.OOMKilled && !restarting(c)
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
		Unknown:    "Kubernetes does not report why memory use grew; that requires the application's own metrics or logs.",
		Confidence: High,
		Chain:      chain(&in.App, "Pods", "OOMKilled"),
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
	for _, p := range sortedNames(livePods(&in.App)) {
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
	unknown := "The scheduler has not reported why these pods remain unscheduled."
	for _, p := range pods {
		if p.ScheduledMessage != "" {
			cause, confidence, unknown = p.ScheduledMessage, High, ""
			break
		}
	}
	d := Diagnosis{
		Rule:       "pod.unschedulable",
		Severity:   Critical,
		Subject:    podRef(pods[0]),
		Problem:    podsPhrase(len(pods)) + " cannot be scheduled onto any node",
		Cause:      cause,
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Pods", "Pending", "Unschedulable"),
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
	scheduling := limitEvents(warningsAbout(in, podRef(pods[0]), "FailedScheduling"))
	for i := range scheduling {
		d.Evidence = append(d.Evidence, eventEvidence(&scheduling[i]))
	}
	return []Diagnosis{d}
}

// podFailed reports pods that ran and stopped for good: evicted, killed by the
// node, or terminated with an error and not restarted.
func podFailed(in *Input) []Diagnosis {
	var pods []*application.Pod
	for _, p := range sortedNames(livePods(&in.App)) {
		if p.Phase == "Failed" {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}

	cause, confidence, unknown := "", Low, "Kubernetes does not report why the pod failed."
	if reason := pods[0].Reason; reason != "" {
		confidence, unknown = High, ""
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
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Pods", "Failed"),
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
	for _, p := range sortedNames(livePods(&in.App)) {
		if p.Phase == "Running" && !p.Ready && p.Reason == "" {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil
	}

	cause, confidence := "the readiness probe has not succeeded", Low
	unknown := "Kubernetes does not say why the probe is failing without an event to quote."
	var quoted []application.Event
	for _, p := range pods {
		if events := warningsAbout(in, podRef(p), "Unhealthy", "ProbeWarning"); len(events) > 0 {
			cause, confidence, unknown = events[0].Message, High, ""
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
		Unknown:    unknown,
		Confidence: confidence,
		Chain:      chain(&in.App, "Pods", "not ready"),
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
	shown := limitEvents(quoted)
	for i := range shown {
		d.Evidence = append(d.Evidence, eventEvidence(&shown[i]))
	}
	return []Diagnosis{d}
}

// creatingReasons are the kubelet's complaints about a pod it was given and
// cannot set up: its volumes or its network sandbox.
var creatingReasons = []string{"FailedMount", "FailedAttachVolume", "FailedMapVolume", "FailedCreatePodSandBox"}

// stuckCreating reports pods the kubelet has accepted and cannot start, which
// the pod itself shows only as "ContainerCreating" — the same word it shows
// for the two seconds a healthy pod takes. The events are what tell them apart,
// so the rule fires only when the kubelet has complained.
func stuckCreating(in *Input) []Diagnosis {
	var (
		pods   []*application.Pod
		events []application.Event
	)
	for _, p := range sortedNames(livePods(&in.App)) {
		if p.Phase != "Pending" || !p.Scheduled || !creating(p) {
			continue
		}
		complaints := warningsAbout(in, podRef(p), creatingReasons...)
		if len(complaints) == 0 {
			continue
		}
		pods = append(pods, p)
		events = append(events, complaints[0])
	}
	if len(pods) == 0 {
		return nil
	}

	first := pods[0]
	cause, suggestions := creatingCause(&events[0], first)
	d := Diagnosis{
		Rule:     "pod.creating",
		Severity: Critical,
		Subject:  podRef(first),
		Problem: podsPhrase(len(pods)) + agree(len(pods), " is", " are") +
			" stuck creating " + agree(len(pods), "its", "their") + " containers",
		Cause:       cause,
		Confidence:  High,
		Chain:       chain(&in.App, "Pods", "ContainerCreating", events[0].Reason),
		Suggestions: suggestions,
	}
	for _, p := range limitPods(pods) {
		d.Evidence = append(d.Evidence, Evidence{Kind: "Pod", Name: p.Name, Detail: "Pending on " + orUnknown(p.Node) + ", containers not created"})
	}
	shown := limitEvents(events)
	for i := range shown {
		d.Evidence = append(d.Evidence, eventEvidence(&shown[i]))
	}
	return []Diagnosis{d}
}

func creating(p *application.Pod) bool {
	for i := range p.Containers {
		c := &p.Containers[i]
		if c.State == "waiting" && (c.Reason == "ContainerCreating" || c.Reason == "PodInitializing") {
			return true
		}
	}
	return false
}

// creatingCause reads the kubelet's message. The cases separated here are the
// ones whose fix lives in a different place: the manifest, the storage driver,
// the network plugin.
func creatingCause(e *application.Event, p *application.Pod) (string, []Suggestion) {
	message := strings.ToLower(e.Message)
	describe := Suggestion{Text: "Read the pod's events in full", Command: describeCommand("pod", p.Namespace, p.Name)}
	switch {
	case e.Reason == "FailedMount" && strings.Contains(message, "configmap") && strings.Contains(message, "not found"):
		return "a ConfigMap the pod mounts does not exist: " + e.Message, []Suggestion{
			{Text: "List the ConfigMaps in the namespace and compare the names", Command: "kubectl get configmaps -n " + p.Namespace},
			describe,
		}
	case e.Reason == "FailedMount" && strings.Contains(message, "secret") && strings.Contains(message, "not found"):
		return "a Secret the pod mounts does not exist: " + e.Message, []Suggestion{
			{Text: "List the Secrets in the namespace and compare the names", Command: "kubectl get secrets -n " + p.Namespace},
			describe,
		}
	case strings.Contains(message, "multi-attach"):
		return "the volume is still attached to another node, and a ReadWriteOnce volume can only be attached to one: " + e.Message, []Suggestion{
			{Text: "Find which node still holds the volume attachment", Command: "kubectl get volumeattachments"},
			describe,
		}
	case e.Reason == "FailedCreatePodSandBox" && (strings.Contains(message, "assign an ip") ||
		strings.Contains(message, "no available ip") || strings.Contains(message, "ipam")):
		return "the network plugin could not give the pod an IP address; the node or subnet may have run out: " + e.Message, []Suggestion{
			{Text: "Check the CNI pods on the node the pod was placed on",
				Command: "kubectl get pods -n kube-system -o wide --field-selector spec.nodeName=" + p.Node},
			describe,
		}
	case e.Reason == "FailedCreatePodSandBox":
		return "the node could not set up the pod's network sandbox: " + e.Message, []Suggestion{
			{Text: "Check the CNI pods on the node the pod was placed on",
				Command: "kubectl get pods -n kube-system -o wide --field-selector spec.nodeName=" + p.Node},
			describe,
		}
	case e.Reason == "FailedAttachVolume":
		return "the storage driver could not attach the volume to the node: " + e.Message, []Suggestion{
			{Text: "Check the volume attachments and the CSI driver's pods", Command: "kubectl get volumeattachments"},
			describe,
		}
	}
	return "the kubelet cannot mount the pod's volumes: " + e.Message, []Suggestion{describe}
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
