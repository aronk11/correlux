package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/aronk11/correlux/internal/kube/debug"
)

// CreateDebug creates a reviewed troubleshooting Job without retrying side effects.
func (f *Factory) CreateDebug(ctx context.Context, cluster string, job *batchv1.Job) (*batchv1.Job, error) {
	cs, err := f.Clientset(cluster)
	if err != nil {
		return nil, err
	}
	return cs.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{FieldManager: "correlux"})
}

// DebugSessions lists a bounded page with server-side namespace/label filtering.
func (f *Factory) DebugSessions(ctx context.Context, cluster, namespace, continuation string) ([]byte, error) {
	cs, err := f.Clientset(cluster)
	if err != nil {
		return nil, err
	}
	jobs, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: debug.Label + "=true", Limit: 200, Continue: continuation})
	if err != nil {
		return nil, err
	}
	return json.Marshal(jobs)
}

// AddDebugContainer adds one reviewed container while retaining the pod's
// concurrency token. Ephemeral entries cannot be removed afterward.
func (f *Factory) AddDebugContainer(ctx context.Context, cluster, namespace, podName, uid, version, image, target string) (string, error) {
	cs, err := f.Clientset(cluster)
	if err != nil {
		return "", err
	}
	pod, err := cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if string(pod.UID) != uid || pod.ResourceVersion != version {
		return "", errors.New("pod changed since confirmation; refresh it before adding a debug container")
	}
	if pod.Status.Phase != corev1.PodRunning {
		return "", errors.New("debug containers require a running pod in this workflow")
	}
	if pod.Spec.OS != nil && pod.Spec.OS.Name == corev1.Windows {
		return "", errors.New("this toolbox requires a Linux pod")
	}
	found := false
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if container.Name == target {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("container %q does not exist in this pod", target)
	}
	name := "correlux-debug-" + rand.String(8)
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: name, Image: image, Command: []string{"/bin/sh", "-c", "sleep 900"}, SecurityContext: debug.SecurityContext()}, TargetContainerName: target})
	_, err = cs.CoreV1().Pods(namespace).UpdateEphemeralContainers(ctx, podName, pod, metav1.UpdateOptions{FieldManager: "correlux"})
	return name, err
}
