// Package cli wires kubeui's command line: flag parsing, kubeconfig discovery
// and process lifecycle. It owns everything that must happen before the TUI
// takes over the terminal, and nothing that happens afterwards.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/akiesel/kubeui/internal/buildinfo"
	"github.com/akiesel/kubeui/internal/config"
	kubeclient "github.com/akiesel/kubeui/internal/kube/client"
	"github.com/akiesel/kubeui/internal/kube/kubeconfig"
	"github.com/akiesel/kubeui/internal/ui/app"
)

type globalFlags struct {
	kubeconfig    string
	contextName   string
	namespace     string
	allNamespaces bool
	configPath    string
}

// Execute runs kubeui and returns the process exit code.
func Execute() int {
	silenceClientLogs()

	var flags globalFlags
	root := &cobra.Command{
		Use:   "kubeui",
		Short: "A terminal Kubernetes operations UI that shows what matters",
		Long: "kubeui is a terminal-native Kubernetes operations UI.\n\n" +
			"It runs against your existing kubeconfig; there is no server, agent or CRD to install.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), flags)
		},
	}

	root.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"path to the kubeconfig file (defaults to $KUBECONFIG, then ~/.kube/config)")
	root.PersistentFlags().StringVar(&flags.contextName, "context", "",
		"kubeconfig context to start in")
	root.PersistentFlags().StringVarP(&flags.namespace, "namespace", "n", "",
		"namespace to start in")
	root.PersistentFlags().BoolVarP(&flags.allNamespaces, "all-namespaces", "A", false,
		"start scoped to all namespaces")
	root.PersistentFlags().StringVar(&flags.configPath, "config", "",
		"path to the kubeui config file")

	root.AddCommand(newVersionCommand())
	root.AddCommand(newDoctorCommand(&flags))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintln(os.Stderr, "kubeui: "+err.Error())
		return 1
	}
	return 0
}

// startup is the resolved state kubeui needs before drawing anything.
type startup struct {
	cfg        config.Config
	kubeconfig *kubeconfig.Config
	factory    *kubeclient.Factory
	classifier *kubeconfig.Classifier
	context    string
	warnings   []string
}

// prepare loads configuration and the kubeconfig. It performs no network I/O,
// so kubeui starts instantly even when the cluster is unreachable — the
// connection state is then shown inside the UI rather than as a startup crash.
func prepare(flags globalFlags) (*startup, error) {
	warnings := make([]string, 0, 2)

	cfg, err := loadConfig(flags.configPath)
	if err != nil {
		warnings = append(warnings, err.Error())
	}

	classifier, patternErrs := kubeconfig.NewClassifier(
		cfg.Safety.ProductionPatterns, cfg.Safety.ProductionContexts)
	for _, e := range patternErrs {
		warnings = append(warnings, "invalid production pattern: "+e.Error())
	}

	kc, err := kubeconfig.Load(kubeconfig.LoadOptions{
		ExplicitPath: flags.kubeconfig,
		Classifier:   classifier,
	})
	if err != nil {
		return nil, err
	}

	contextName, err := kc.ResolveStartContext(flags.contextName, cfg.Startup.Context)
	if err != nil {
		return nil, err
	}

	return &startup{
		cfg:        cfg,
		kubeconfig: kc,
		factory:    kubeclient.New(kc.Raw(), kc.LoadingRules(), kubeclient.Options{}),
		classifier: classifier,
		context:    contextName,
		warnings:   warnings,
	}, nil
}

func loadConfig(explicit string) (config.Config, error) {
	if explicit != "" {
		return config.Load(explicit)
	}
	return config.LoadDefault()
}

func run(ctx context.Context, flags globalFlags) error {
	s, err := prepare(flags)
	if err != nil {
		return err
	}

	namespace := flags.namespace
	if namespace == "" {
		namespace = s.cfg.Startup.Namespace
	}

	model := app.New(app.Options{
		Config:         s.cfg,
		Kubeconfig:     s.kubeconfig,
		Factory:        s.factory,
		Classifier:     s.classifier,
		ContextName:    s.context,
		Namespace:      namespace,
		AllNamespaces:  flags.allNamespaces,
		ConfigWarnings: s.warnings,
	})

	program := tea.NewProgram(model, tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

func newVersionCommand() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Get()
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), info.Version)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), info.String())
			fmt.Fprintln(cmd.OutOrStdout(), "  go:     "+info.GoVersion)
			if info.Date != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "  built:  "+info.Date)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the version number")
	return cmd
}
