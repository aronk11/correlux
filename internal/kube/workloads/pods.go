package workloads

import (
	"context"
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// fromPod reduces a pod to the state an operator scans for.
//
// The reason is read off the pod the way kubectl reads it, and it is copied
// rather than interpreted: "CrashLoopBackOff" is what the cluster says, and
// turning that into "the database is unreachable" is the diagnosis engine's
// job (SPEC 10), not this converter's.
func fromPod(p *corev1.Pod) application.Pod {
	out := application.Pod{
		Meta:      meta("Pod", p.ObjectMeta),
		Phase:     string(p.Status.Phase),
		Ready:     podReady(p),
		Node:      p.Spec.NodeName,
		Scheduled: true,
	}
	for i := range p.Status.ContainerStatuses {
		out.Restarts += p.Status.ContainerStatuses[i].RestartCount
	}
	out.Reason = podReason(p)
	out.Containers = containers(p)
	out.Scheduled, out.ScheduledReason, out.ScheduledMessage = scheduled(p)
	for i := range p.Spec.Volumes {
		if claim := p.Spec.Volumes[i].PersistentVolumeClaim; claim != nil {
			out.Claims = append(out.Claims, claim.ClaimName)
		}
	}
	return out
}

// containers records both the current and the previous state of every
// container. A pod in CrashLoopBackOff is only *waiting* right now; how its
// last run ended is the part that explains anything.
//
// The walk starts from the spec rather than from the statuses, because a pod
// that has never been scheduled has no statuses at all — and an unschedulable
// pod is exactly the one whose requests somebody wants to look at.
func containers(p *corev1.Pod) []application.Container {
	statuses := make(map[string]*corev1.ContainerStatus,
		len(p.Status.InitContainerStatuses)+len(p.Status.ContainerStatuses))
	for i := range p.Status.InitContainerStatuses {
		statuses[p.Status.InitContainerStatuses[i].Name] = &p.Status.InitContainerStatuses[i]
	}
	for i := range p.Status.ContainerStatuses {
		statuses[p.Status.ContainerStatuses[i].Name] = &p.Status.ContainerStatuses[i]
	}

	out := make([]application.Container, 0, len(p.Spec.InitContainers)+len(p.Spec.Containers))
	claimed := make(map[string]bool, cap(out))
	for i := range p.Spec.InitContainers {
		spec := &p.Spec.InitContainers[i]
		c := container(statuses[spec.Name], true)
		c.Name, c.Sidecar = spec.Name, spec.RestartPolicy != nil &&
			*spec.RestartPolicy == corev1.ContainerRestartPolicyAlways
		fillResources(&c, spec)
		claimed[spec.Name] = true
		out = append(out, c)
	}
	for i := range p.Spec.Containers {
		spec := &p.Spec.Containers[i]
		c := container(statuses[spec.Name], false)
		c.Name = spec.Name
		fillResources(&c, spec)
		claimed[spec.Name] = true
		out = append(out, c)
	}

	// A status naming a container the spec does not is not supposed to happen,
	// and it carries the reason a container died. Keeping it costs a map lookup
	// and losing it would cost an explanation.
	for i := range p.Status.InitContainerStatuses {
		if s := &p.Status.InitContainerStatuses[i]; !claimed[s.Name] {
			out = append(out, container(s, true))
		}
	}
	for i := range p.Status.ContainerStatuses {
		if s := &p.Status.ContainerStatuses[i]; !claimed[s.Name] {
			out = append(out, container(s, false))
		}
	}
	return out
}

// fillResources copies what the spec asked for. An absent request is recorded
// as absent, never as zero: nothing is what an unsized container is, and zero
// is what a container that asked for nothing is.
func fillResources(c *application.Container, spec *corev1.Container) {
	c.Requests = amountsOf(spec.Resources.Requests)
	c.Limits = amountsOf(spec.Resources.Limits)
}

func amountsOf(list corev1.ResourceList) application.Amounts {
	out := application.Amounts{}
	if cpu, ok := list[corev1.ResourceCPU]; ok {
		out.CPUMilli, out.HasCPU = cpu.MilliValue(), true
	}
	if mem, ok := list[corev1.ResourceMemory]; ok {
		out.MemoryBytes, out.HasMemory = mem.Value(), true
	}
	return out
}

// container reduces one container status. A nil status is a container the
// kubelet has not reported on yet, which leaves its state empty rather than
// inventing one.
func container(status *corev1.ContainerStatus, init bool) application.Container {
	c := application.Container{Init: init}
	if status == nil {
		return c
	}
	s := *status
	c.Name, c.Image, c.Ready, c.Restarts = s.Name, s.Image, s.Ready, s.RestartCount
	switch {
	case s.State.Waiting != nil:
		c.State, c.Reason, c.Message = "waiting", s.State.Waiting.Reason, s.State.Waiting.Message
	case s.State.Terminated != nil:
		t := s.State.Terminated
		c.State, c.Reason, c.Message, c.ExitCode = "terminated", t.Reason, t.Message, t.ExitCode
		c.OOMKilled = t.Reason == "OOMKilled"
	case s.State.Running != nil:
		c.State = "running"
	}
	if t := s.LastTerminationState.Terminated; t != nil {
		c.LastReason, c.LastExitCode = t.Reason, t.ExitCode
		c.OOMKilled = c.OOMKilled || t.Reason == "OOMKilled"
	}
	return c
}

// scheduled reports whether a node has accepted the pod, and what the scheduler
// said when it has not.
func scheduled(p *corev1.Pod) (bool, string, string) {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status != corev1.ConditionTrue {
			return false, c.Reason, c.Message
		}
	}
	return p.Spec.NodeName != "" || p.Status.Phase != corev1.PodPending, "", ""
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// podReason names why a pod is not simply running, in the order a human reads
// a pod: is it going away, did it fail, what are its containers doing, and was
// it ever scheduled at all.
func podReason(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	if p.Status.Phase == corev1.PodSucceeded {
		return ""
	}
	if p.Status.Phase == corev1.PodFailed && p.Status.Reason != "" {
		// "Evicted", "NodeAffinity", "Shutdown" — the node-level verdict.
		return p.Status.Reason
	}

	// Init containers first: while they are stuck, nothing else has started.
	if reason := containerReason(p.Status.InitContainerStatuses); reason != "" {
		return reason
	}
	if reason := containerReason(p.Status.ContainerStatuses); reason != "" {
		return reason
	}

	if p.Status.Phase == corev1.PodPending {
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason != "" {
				// "Unschedulable": no node can take this pod.
				return c.Reason
			}
		}
	}
	if p.Status.Phase == corev1.PodFailed {
		return "Failed"
	}
	return ""
}

// containerReason reports the first container state worth naming. A container
// that was OOM-killed and has since restarted is still the interesting fact
// about the pod, so the last terminated state is consulted too.
func containerReason(statuses []corev1.ContainerStatus) string {
	for i := range statuses {
		s := statuses[i]
		if w := s.State.Waiting; w != nil && w.Reason != "" {
			return w.Reason
		}
		if t := s.State.Terminated; t != nil && t.Reason != "" && t.Reason != "Completed" {
			return t.Reason
		}
		if t := s.LastTerminationState.Terminated; t != nil && t.Reason == "OOMKilled" && !s.Ready {
			return t.Reason
		}
	}
	return ""
}

// gapReason turns a failed listing into the shortest true sentence about it.
// A user who may not list ingresses needs to read exactly that, not a stack of
// wrapped client-go errors.
func gapReason(err error) string {
	switch {
	case err == nil:
		return ""
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return "not permitted for this user"
	case apierrors.IsNotFound(err):
		return "not served by this cluster"
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err), errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case apierrors.IsServiceUnavailable(err), apierrors.IsInternalError(err):
		return "the API is unavailable"
	default:
		if reason := apierrors.ReasonForError(err); reason != metav1.StatusReasonUnknown && reason != "" {
			return string(reason)
		}
		return innermost(err.Error())
	}
}

// innermost keeps the last sentence of a wrapped error, which is the one that
// says what actually went wrong.
func innermost(msg string) string {
	if idx := strings.LastIndex(msg, ": "); idx > 0 && idx < len(msg)-2 {
		if tail := msg[idx+2:]; len(tail) > 12 {
			return tail
		}
	}
	return msg
}
