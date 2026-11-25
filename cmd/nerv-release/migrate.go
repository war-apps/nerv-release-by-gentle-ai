package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/legacymigrate"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/refusal"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

const migrateUsage = `Usage: nerv-release migrate [--cwd <dir>] [--user] [--dry-run]

Converts the legacy release and deploy sections of .ai-workflow/release.yaml
(nerv moves them there as they were) into today's shape:

  ship: (nerv 5.0.0-alpha.1)  becomes release:, its keys moved
  release: with engine keys at its top or a build: mapping
                              its keys move under
                              release.providers.conventional-commits and
                              the project (build: docker-compose or bash)
  a 4.2.0 release: (semantic_release:, project presets)
                              each project becomes build: bash with the
                              commands its presets ran, and the
                              semantic-release configuration is imported
                              into release.providers.conventional-commits
  a 4.2.0 pipeline:           becomes deploy: one release-ssh-compose
                              target per environment (host: github when
                              origin is GitHub)
  provider: bash targets      become provider: script
  .ai-workflow/providers/deploy-<name>
                              moves to release-<name> (extension: release)

What has no equivalent is listed under "Check by hand". A key already set
in today's shape is kept and reported, never overwritten. The personal
release-ssh-compose settings of a pipeline: (ssh_auth, ssh_key_file, ...)
are dropped from the repository file and never written to the global
release.yaml, with or without --user: each one is reported with the
command that sets it (nerv-release configure --set ssh-compose.<key>=<value>),
to run if you trust the repository. Every repository file replaced is
backed up under .nerv/backups/release-migrate-<timestamp>/ (git-ignored), the
global release.yaml next to it (<file>.bak-migrate-<timestamp>). A second
run changes nothing.

Flags:
  --user         Also convert the global release.yaml (provider: bash
                 targets under deploy: and defaults.deploy); outside a
                 repository, only it
  --dry-run      Print what would change; write nothing
  --cwd <dir>    A directory inside the repository (default ".")

Exit codes: 0 migrated, nothing to migrate, or a dry run; 1 refused (a
ship: and a release: section together, a key with no place, a result that
does not load, a symlinked file, not a repository); 2 usage or environment
error.
`

// runMigrate is `nerv-release migrate`.
func runMigrate(args []string, stdout, stderr io.Writer, opts options) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cwd := fs.String("cwd", ".", "")
	user := fs.Bool("user", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, migrateUsage)
			return 0
		}
		return migrateUsageError(stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return migrateUsageError(stderr, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	err := migrate(context.Background(), *cwd, *user, *dryRun, stdout, opts)
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "nerv-release migrate: %v\n", err)
	return exitCodeFor(err)
}

func migrateUsageError(stderr io.Writer, problem string) int {
	fmt.Fprintf(stderr, "nerv-release migrate: %s\n\n%s", problem, migrateUsage)
	return 2
}

func migrate(ctx context.Context, cwd string, user, dryRun bool, stdout io.Writer, opts options) error {
	o := legacymigrate.Options{ExtensionsFS: opts.ExtensionsFS}
	if o.ExtensionsFS == nil {
		o.ExtensionsFS = nervrelease.ExtensionsFS()
	}
	top, err := gitTopLevel(ctx, opts.Runner, cwd)
	switch {
	case err == nil:
		o.RepoRoot = top
		o.OriginURL = originURL(ctx, opts, top)
	case user && refusal.Is(err):
		// --user outside a repository converts the global file only.
	default:
		return err
	}
	if user {
		home, err := resolveHome(opts.Home)
		if err != nil {
			return err
		}
		o.UserPath = aiworkflow.UserConfigPath(home, runtime.GOOS, workflowconfig.Tool)
	}
	p, err := legacymigrate.Compute(o)
	if err != nil {
		return err
	}
	if p.Empty() {
		fmt.Fprintln(stdout, "nerv-release migrate: nothing to migrate")
		if text := legacymigrate.Render(p); text != "" {
			fmt.Fprint(stdout, "\n"+text)
		}
		return nil
	}
	if dryRun {
		fmt.Fprintf(stdout, "nerv-release migrate: dry run: nothing was written; it would:\n%s", legacymigrate.Render(p))
		return nil
	}
	fmt.Fprintf(stdout, "nerv-release migrate:\n%s", legacymigrate.Render(p))
	backups, err := legacymigrate.Apply(p, time.Now())
	if err != nil {
		return err
	}
	msg := "migrated"
	if len(backups) > 0 {
		msg += "; backups: " + strings.Join(backups, ", ")
	}
	fmt.Fprintf(stdout, "\nnerv-release migrate: %s\n", msg)
	return nil
}

// originURL is the repository's origin URL, "" when it has none.
func originURL(ctx context.Context, opts options, top string) string {
	out, _, code, err := opts.Runner.Run(ctx, "git", "-C", top, "remote", "get-url", "origin")
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}
