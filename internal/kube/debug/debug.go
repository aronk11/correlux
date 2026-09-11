// Package debug builds explicitly scoped, bounded Kubernetes troubleshooting jobs.
package debug

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

// Label selects only resources created for Correlux troubleshooting sessions.
const Label = "correlux.dev/debug-session"

// Options is the complete, reviewable description of a troubleshooting job.
type Options struct {
	Namespace, Image, Mode, Destination string
	Seconds                             int64
	ImagePullSecrets                    []string
	ImagePullPolicy                     corev1.PullPolicy
}

// PullPolicy validates a configured policy. Empty keeps Kubernetes defaulting.
func PullPolicy(value string) (corev1.PullPolicy, error) {
	policy := corev1.PullPolicy(value)
	switch policy {
	case "", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
		return policy, nil
	default:
		return "", errors.New("debug.imagePullPolicy must be Always, IfNotPresent or Never")
	}
}

// Command constructs an argument vector; destinations never become shell code.
func Command(mode, destination string, seconds int64) ([]string, error) {
	switch mode {
	case "toolbox":
		return []string{"/bin/sh", "-c", "sleep " + strconv.FormatInt(seconds, 10)}, nil
	case "dns":
		if destination == "" || strings.HasPrefix(destination, "-") || strings.ContainsAny(destination, " \t\n\r/") {
			return nil, errors.New("enter a DNS name without spaces or options")
		}
		return []string{"nslookup", destination}, nil
	case "tcp":
		host, port, err := net.SplitHostPort(destination)
		if err != nil || host == "" || strings.HasPrefix(host, "-") {
			return nil, errors.New("enter host:port, using [address]:port for IPv6")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("TCP port must be between 1 and 65535")
		}
		return []string{"nc", "-v", "-z", "-w", "10", host, port}, nil
	case "http":
		u, err := url.Parse(destination)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.ContainsAny(destination, "\r\n") {
			return nil, errors.New("enter an http:// or https:// URL without embedded credentials")
		}
		return []string{"curl", "--silent", "--show-error", "--fail", "--connect-timeout", "10", "--max-time", "30", "--output", "/dev/null", "--write-out", "HTTP %{http_code}\nremote %{remote_ip}:%{remote_port}\nDNS %{time_namelookup}s\nconnect %{time_connect}s\nTLS %{time_appconnect}s\ntotal %{time_total}s\n", "--", destination}, nil
	}
	return nil, errors.New("unknown troubleshooting mode")
}

// Job builds a restricted Linux job with a server-enforced deadline and cleanup.
func Job(opts Options) (*batchv1.Job, error) {
	if _, err := PullPolicy(string(opts.ImagePullPolicy)); err != nil {
		return nil, err
	}
	if len(validation.IsDNS1123Label(opts.Namespace)) != 0 {
		return nil, errors.New("select one valid namespace for troubleshooting")
	}
	if opts.Image == "" || strings.ContainsAny(opts.Image, " \t\r\n") {
		return nil, errors.New("enter a container image reference")
	}
	if opts.Seconds < 1 || opts.Seconds > 3600 {
		return nil, errors.New("debug runtime must be between 1 and 3600 seconds")
	}
	command, err := Command(opts.Mode, opts.Destination, opts.Seconds)
	if err != nil {
		return nil, err
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{GenerateName: "correlux-" + opts.Mode + "-", Namespace: opts.Namespace, Labels: map[string]string{Label: "true"}, Annotations: map[string]string{"correlux.dev/debug-mode": opts.Mode, "correlux.dev/debug-destination": opts.Destination}}, Spec: batchv1.JobSpec{
		BackoffLimit: ptr.To[int32](0), ActiveDeadlineSeconds: ptr.To(opts.Seconds), TTLSecondsAfterFinished: ptr.To[int32](600),
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{Label: "true"}}, Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr.To(false), NodeSelector: map[string]string{"kubernetes.io/os": "linux"},
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers:      []corev1.Container{{Name: "toolbox", Image: opts.Image, ImagePullPolicy: opts.ImagePullPolicy, Command: command, SecurityContext: SecurityContext(), Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}}}},
		}},
	}}
	for _, name := range opts.ImagePullSecrets {
		if len(validation.IsDNS1123Subdomain(name)) != 0 {
			return nil, errors.New("invalid debug imagePullSecret name")
		}
		job.Spec.Template.Spec.ImagePullSecrets = append(job.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
	}
	return job, nil
}

// SecurityContext drops privileges for both standalone and ephemeral toolboxes.
func SecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
}
