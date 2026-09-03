// Package podexec runs an interactive command inside a container and streams
// a terminal to it.
//
// Unlike logs, which read something that already exists, exec opens a
// connection that stays open for as long as a human is typing into it — there
// is no tail, no timeout that makes sense, and no batching. The package stays
// small on purpose: everything about *when* Correlux allows a shell (SPEC 16)
// lives above it.
package podexec

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Target names one container to run a command in.
type Target struct {
	Namespace string
	Pod       string
	// Container may be empty, in which case the server picks the pod's only
	// container, and refuses when there is more than one — the same contract
	// logs.Source uses, for the same reason: guessing which container someone
	// meant is a worse answer than asking.
	Container string
}

// Label renders the target the way a session is attributed.
func (t Target) Label() string {
	if t.Container == "" {
		return t.Pod
	}
	return t.Pod + "/" + t.Container
}

// DefaultShellCommand picks bash when a container has it, and falls back to
// sh, which is the one shell every image that has a shell at all is
// guaranteed to carry.
var DefaultShellCommand = []string{
	"/bin/sh", "-c",
	"if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi",
}

// request builds the exec subresource request. It is separate from Stream so
// the URL and query it produces can be checked without an API server.
func request(rc restclient.Interface, target Target, command []string) *restclient.Request {
	req := rc.Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(target.Pod).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: target.Container,
		Command:   command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)
	return req
}

// Stream runs command inside target and connects it to the given terminal. It
// blocks until the command ends, the connection breaks, or ctx is cancelled.
//
// restCfg must not carry a request timeout: an exec session is meant to stay
// open for as long as the user is typing into it, and a factory's default
// per-request timeout would cut it off mid-session (see
// client.Factory.RESTConfigForExec).
func Stream(
	ctx context.Context,
	restCfg *restclient.Config,
	target Target,
	command []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	sizeQueue remotecommand.TerminalSizeQueue,
) error {
	core, err := corev1client.NewForConfig(restCfg)
	if err != nil {
		return err
	}
	req := request(core.RESTClient(), target, command)

	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})
}
