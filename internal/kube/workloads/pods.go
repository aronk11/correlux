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
		Meta:  meta("Pod", p.ObjectMeta),
		Phase: string(p.Status.Phase),
		Ready: podReady(p),
		Node:  p.Spec.NodeName,
	}
	for i := range p.Status.ContainerStatuses {
		out.Restarts += p.Status.ContainerStatuses[i].RestartCount
	}
	out.Reason = podReason(p)
	return out
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
