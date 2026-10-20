package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/aronk11/correlux/internal/buildinfo"
	"github.com/aronk11/correlux/internal/config"
	"github.com/aronk11/correlux/internal/ui/theme"
	"github.com/aronk11/correlux/internal/update"
)

// checkRelease and installRelease are what runUpdate calls out to, so its
// conversation can be tested without a network.
var (
	checkRelease   = update.Check
	installRelease = update.Install
)

// newUpdateCommand checks for a newer release and, after asking, installs it.
// It always asks: replacing the binary somebody runs against production is not
// something to do behind their back, so there is deliberately no --yes.
func newUpdateCommand(flags *globalFlags) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for a newer Correlux and install it after asking",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags.configPath)
			if err != nil {
				return err
			}
			if cfg.AirGapped || flags.airGapped {
				return errors.New("updates are disabled in air-gapped mode; transfer a verified release archive instead")
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			interactive := term.IsTerminal(os.Stdin.Fd())
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.InOrStdin(), interactive, checkOnly, exe)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only report whether a newer release exists")
	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, in io.Reader, interactive, checkOnly bool, exe string) error {
	current := buildinfo.Get().Version
	cache := ""
	if dir, err := config.Dir(); err == nil {
		cache = update.CachePath(dir)
	}
	release := checkRelease(ctx, cache, current, true)
	if release.Err != nil {
		return fmt.Errorf("could not reach the release feed: %w", release.Err)
	}
	if !update.Newer(current, release.Version) {
		fmt.Fprintln(out, "Correlux "+current+" is up to date.")
		return nil
	}
	fmt.Fprintln(out, release.Version+" is available (you have "+current+"). "+release.URL)
	if checkOnly {
		return nil
	}

	plan := update.PlanFor(exe)
	if plan.Method != update.SelfReplace {
		fmt.Fprintln(out, "This copy is managed by another tool. To update it, run: "+plan.Hint)
		return nil
	}
	if !interactive {
		return errors.New("run correlux update in a terminal to confirm the installation")
	}
	verification := "cosign is not installed, so only the checksum is checked"
	if update.CosignAvailable() {
		verification = "the checksum and the release signature are verified with cosign"
	}
	fmt.Fprintf(out, "Replace %s with %s (%s)?\n%s. [y/N] ", plan.Executable, release.Version,
		update.ArchiveName(release.Version, runtime.GOOS, runtime.GOARCH), verification)
	answer, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(out, "Nothing changed.")
		return nil
	}
	res, err := installRelease(ctx, plan, release.Version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	signed := "checksum verified"
	if res.SignatureVerified {
		signed = "checksum and signature verified"
	}
	owl := theme.GlyphsFor(theme.DetectUnicode(theme.OSEnv)).Owl
	fmt.Fprintln(out, owl+" Updated to "+res.Version+" ("+signed+"). Lux is ready when you are.")
	return nil
}
