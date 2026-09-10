package client

import (
	"context"

	"github.com/aronk11/correlux/internal/kube/helm"
)

// Helm runs a bounded Helm request against the same merged config as the TUI.
func (f *Factory) Helm(ctx context.Context, cluster, namespace string, args []string) ([]byte, error) {
	return helm.Run(ctx, f.raw, cluster, namespace, args)
}

// HelmWithValues applies explicitly reviewed values without putting them in arguments.
func (f *Factory) HelmWithValues(ctx context.Context, cluster, namespace string, args []string, values []byte) ([]byte, error) {
	return helm.RunWithValues(ctx, f.raw, cluster, namespace, args, values)
}
