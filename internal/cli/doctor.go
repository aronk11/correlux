package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	authv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeclient "github.com/aronk11/kubeui/internal/kube/client"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// checkResult is the outcome of one diagnostic.
type checkResult struct {
	name   string
	status theme.Status
	detail string
	hint   string
}

// newDoctorCommand builds `kubeui doctor`, which answers "why does kubeui not
// work here?" without opening the TUI — the one thing that must still work when
// everything else is broken.
func newDoctorCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check kubeconfig, connectivity, permissions and terminal support",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results := runDoctor(cmd.Context(), *flags)
			printDoctor(cmd.OutOrStdout(), results)
			for _, r := range results {
				if r.status == theme.StatusCritical {
					return fmt.Errorf("%d of %d checks failed", countFailed(results), len(results))
				}
			}
			return nil
		},
	}
}

func countFailed(results []checkResult) int {
	n := 0
	for _, r := range results {
		if r.status == theme.StatusCritical {
			n++
		}
	}
	return n
}

func runDoctor(ctx context.Context, flags globalFlags) []checkResult {
	var results []checkResult

	s, err := prepare(flags)
	if err != nil {
		return append(results, checkResult{
			name:   "kubeconfig",
			status: theme.StatusCritical,
			detail: err.Error(),
			hint:   "Fix the kubeconfig, or pass --kubeconfig/--context.",
		})
	}

	results = append(results, checkResult{
		name:   "kubeconfig",
		status: theme.StatusHealthy,
		detail: fmt.Sprintf("%d context(s) from %s", len(s.kubeconfig.Contexts), strings.Join(s.kubeconfig.Sources, ", ")),
	})

	kctx, _ := s.kubeconfig.Context(s.context)
	contextDetail := s.context
	contextStatus := theme.StatusHealthy
	if kctx.Production {
		contextDetail += " (classified as production)"
		contextStatus = theme.StatusWarning
	}
	results = append(results, checkResult{name: "context", status: contextStatus, detail: contextDetail})

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info := s.factory.Probe(probeCtx, s.context)
	switch info.State {
	case kubeclient.ConnOK:
		results = append(results, checkResult{
			name:   "kubernetes api",
			status: theme.StatusHealthy,
			detail: fmt.Sprintf("%s in %dms at %s", info.ServerVersion, info.Latency.Milliseconds(), info.Server),
		})
	default:
		return append(results,
			checkResult{
				name:   "kubernetes api",
				status: theme.StatusCritical,
				detail: fmt.Sprintf("%s: %s", info.State, kubeclient.FriendlyError(info.Err)),
				hint:   info.Hint,
			},
			terminalCheck(),
		)
	}

	results = append(results, permissionChecks(ctx, s, flags)...)
	return append(results, metricsCheck(ctx, s), terminalCheck())
}

// permissionChecks asks the API server what this user may do, which is more
// reliable (and cheaper) than trying the operations and interpreting failures.
func permissionChecks(ctx context.Context, s *startup, flags globalFlags) []checkResult {
	cs, err := s.factory.Clientset(s.context)
	if err != nil {
		return []checkResult{{name: "permissions", status: theme.StatusWarning, detail: err.Error()}}
	}

	namespace := flags.namespace
	if namespace == "" {
		if kctx, ok := s.kubeconfig.Context(s.context); ok {
			namespace = kctx.Namespace
		}
	}

	type probe struct {
		label     string
		namespace string
		verb      string
		resource  string
	}
	probes := []probe{
		{"list namespaces", "", "list", "namespaces"},
		{"list pods in " + namespace, namespace, "list", "pods"},
		{"list deployments in " + namespace, namespace, "list", "deployments"},
	}

	var allowed, denied []string
	for _, p := range probes {
		ssar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: p.namespace,
					Verb:      p.verb,
					Resource:  p.resource,
				},
			},
		}
		reviewCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		res, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(reviewCtx, ssar, metav1.CreateOptions{})
		cancel()
		switch {
		case err != nil && apierrors.IsForbidden(err):
			// Even asking is denied; report unknown rather than guessing.
			return []checkResult{{
				name:   "permissions",
				status: theme.StatusWarning,
				detail: "cannot query own permissions (SelfSubjectAccessReview denied)",
				hint:   "kubeui will still work; features you lack access to report it individually.",
			}}
		case err != nil:
			denied = append(denied, p.label+" (unknown)")
		case res.Status.Allowed:
			allowed = append(allowed, p.label)
		default:
			denied = append(denied, p.label)
		}
	}

	status := theme.StatusHealthy
	detail := fmt.Sprintf("%d of %d checked actions allowed", len(allowed), len(allowed)+len(denied))
	hint := ""
	if len(denied) > 0 {
		status = theme.StatusWarning
		hint = "not allowed: " + strings.Join(denied, ", ")
	}
	return []checkResult{{name: "permissions", status: status, detail: detail, hint: hint}}
}

// metricsCheck reports whether the Metrics API is installed. Its absence is a
// warning, never an error: kubeui works without metrics.
func metricsCheck(ctx context.Context, s *startup) checkResult {
	cs, err := s.factory.Clientset(s.context)
	if err != nil {
		return checkResult{name: "metrics", status: theme.StatusWarning, detail: err.Error()}
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body, err := cs.Discovery().RESTClient().Get().AbsPath("/apis/metrics.k8s.io/v1beta1").DoRaw(discoveryCtx)
	if err != nil || len(body) == 0 {
		return checkResult{
			name:   "metrics",
			status: theme.StatusWarning,
			detail: "metrics unavailable",
			hint:   "Metrics Server is not installed or not reachable; kubeui works without it.",
		}
	}
	return checkResult{name: "metrics", status: theme.StatusHealthy, detail: "metrics.k8s.io/v1beta1 available"}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(f.Fd())
}

func terminalCheck() checkResult {
	caps := theme.DetectCapabilities(theme.OSEnv)
	parts := []string{}
	if caps.Color {
		parts = append(parts, "colour")
	} else {
		parts = append(parts, "no colour")
	}
	if caps.Unicode {
		parts = append(parts, "unicode")
	} else {
		parts = append(parts, "ascii glyphs")
	}
	return checkResult{name: "terminal", status: theme.StatusHealthy, detail: strings.Join(parts, ", ")}
}

func printDoctor(w io.Writer, results []checkResult) {
	caps := theme.DetectCapabilities(theme.OSEnv)
	// Colour only when a human is watching: `kubeui doctor > report.txt` and
	// CI logs must stay free of escape sequences.
	tty := isTerminal(w)
	caps.Color = caps.Color && tty
	caps.Attributes = tty
	t := theme.New(caps, "auto")
	for _, r := range results {
		fmt.Fprintf(w, "%s %s\n", t.Badge(r.status, pad(r.name, 16)), r.detail)
		if r.hint != "" {
			fmt.Fprintf(w, "  %s\n", t.Muted.Render(r.hint))
		}
	}
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
