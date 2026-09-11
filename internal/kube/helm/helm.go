// Package helm runs the installed Helm CLI with explicit session credentials.
// Delegating release storage and lifecycle to Helm preserves driver and version
// compatibility without reimplementing Helm's release transactions.
package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const maxOutput = 16 << 20

type boundedBuffer struct{ buf bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxOutput-b.buf.Len() {
		return 0, errors.New("helm output exceeded 16 MiB; narrow the release scope")
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *boundedBuffer) String() string { return b.buf.String() }

// Run isolates the selected context in a private temporary kubeconfig. It never
// changes the user's kubeconfig and never places credentials in process arguments.
func Run(ctx context.Context, raw clientcmdapi.Config, cluster, namespace string, args []string) ([]byte, error) {
	binary, err := exec.LookPath("helm")
	if err != nil {
		return nil, errors.New("helm is not installed: install Helm 3 or 4 and retry")
	}
	if cluster == "" || raw.Contexts[cluster] == nil {
		return nil, errors.New("the selected Helm context is missing from kubeconfig")
	}
	cfg := raw.DeepCopy()
	cfg.CurrentContext = cluster
	if minifyErr := clientcmdapi.MinifyConfig(cfg); minifyErr != nil {
		return nil, minifyErr
	}
	// Resolve referenced certificates before writing outside the kubeconfig directory.
	if flattenErr := clientcmdapi.FlattenConfig(cfg); flattenErr != nil {
		return nil, flattenErr
	}
	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "correlux-helm-*.kubeconfig")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return nil, err
	}
	if closeErr := f.Close(); closeErr != nil {
		return nil, closeErr
	}
	commandArgs := append([]string{"--kubeconfig", f.Name(), "--kube-context", cluster, "--namespace", namespace}, args...)
	cmd := exec.CommandContext(ctx, binary, commandArgs...) //nolint:gosec // resolved Helm executable, fixed argument vectors, no shell
	// Kube flags override credentials, and inherited HELM_KUBE* settings must not
	// silently override the selected context's authentication or impersonation.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "HELM_KUBE") && key != "KUBECONFIG" && key != "HELM_NAMESPACE" && key != "HELM_DEBUG" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.WaitDelay = time.Second
	var stdout, stderr boundedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("helm failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// RunWithValues supplies reviewed values through a private file, never through
// process arguments or the user's Helm configuration.
func RunWithValues(ctx context.Context, raw clientcmdapi.Config, cluster, namespace string, args []string, values []byte) ([]byte, error) {
	if values == nil {
		return Run(ctx, raw, cluster, namespace, args)
	}
	f, err := os.CreateTemp("", "correlux-helm-values-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(values); err != nil {
		_ = f.Close()
		return nil, err
	}
	if closeErr := f.Close(); closeErr != nil {
		return nil, closeErr
	}
	return Run(ctx, raw, cluster, namespace, append(append([]string(nil), args...), "--values", f.Name()))
}
