package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/install"
)

const installUsage = `Usage: nerv-release install [--no-configure]

Materializes and registers the nerv-release plugin marketplace in
Claude Code: the core release plugin, plus every provider that was
enabled as <name>@nerv (carried over) or already <name>@nerv-release
(refreshed); on a fresh machine with none enabled anywhere, every
provider release bundles today (release-bash,
release-conventional-commits, release-docker-compose, release-github,
release-script, release-ssh-compose). When nerv's own release@nerv or
one of its 6 provider plugins is enabled, this disables it (only after
a successful install, and never touching any other nerv plugin), so
/release:* is not duplicated.

On a terminal, offers to run nerv-release configure's interactive
wizard once installed. --no-configure skips that offer entirely;
without a terminal, it prints how to run it later instead of prompting.
`

func runInstall(args []string, stdout, stderr io.Writer, opts options) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noConfigure := fs.Bool("no-configure", false, "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, installUsage)
		return 2
	}

	home, err := resolveHome(opts.Home)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release install: %v\n", err)
		return 2
	}

	releaseRunner := opts.Runner
	if releaseRunner == nil {
		releaseRunner = runner.ExecRunner{}
	}
	deps := install.Deps{
		Home:           home,
		SettingsPath:   filepath.Join(home, ".claude", "settings.json"),
		MarketplaceDir: filepath.Join(home, ".nerv-release", "marketplace"),
		CoreFS:         nervrelease.CoreFS(),
		ExtensionsFS:   nervrelease.ProvidersFS(),
		Runner:         releaseRunner,
		Now:            time.Now(),
		Stdout:         stdout,
	}

	result, err := install.Install(deps)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release install: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "nerv-release installed.")
	for _, name := range result.CarriedOverProviders {
		fmt.Fprintf(stdout, "Carried over %s: now installed as %s@nerv-release.\n", name, name)
	}
	for _, name := range result.BundledProviders {
		fmt.Fprintf(stdout, "Installed %s@nerv-release (release bundles it).\n", name)
	}
	for _, id := range result.DisabledLegacyPlugins {
		fmt.Fprintf(stdout, "Disabled %s so /release:* is not duplicated with nerv-release's own plugin.\n", id)
	}

	offerConfigure(home, *noConfigure, stdout, stderr, opts)

	fmt.Fprintln(stdout, "Restart Claude Code for the change to take effect.")
	return 0
}

// offerConfigure is install's end-of-run configuration step: on a
// terminal (and unless --no-configure), asks whether to run
// nerv-release configure's interactive wizard right now; without a
// terminal, prints how to run it later instead of prompting.
func offerConfigure(home string, noConfigure bool, stdout, stderr io.Writer, opts options) {
	if noConfigure {
		return
	}
	if opts.IsTerminal == nil || !opts.IsTerminal() || opts.Stdin == nil {
		fmt.Fprintln(stdout, "Run `nerv-release configure` to set your provider and personal settings.")
		return
	}

	fmt.Fprint(stdout, "Run nerv-release configure now? [Y/n]: ")
	line, _ := readAnswer(opts.Stdin)
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "" && answer != "y" && answer != "yes" {
		return
	}

	runConfigureWizard(configureDeps(home), opts.Stdin, stdout, stderr)
}
