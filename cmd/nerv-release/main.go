// Command nerv-release is the release tool of the NERV ecosystem,
// independent of nerv: version computation, builds and deploys.
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseclone"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseplan"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versioning"
)

// usage lists every verb R-S2 plans for nerv-release
// (odd/tasks/initial-release.md). Only "version next" works today (U2);
// the others print a not-implemented-yet notice.
const usage = `Usage: nerv-release <command> [flags]

Engine commands:
  version next [flags]  Compute the next release version with the native
                         engine (see "nerv-release version next --help")
  build [flags]          Build, test and package origin's code into a local
                         artifact, no git writes (not implemented yet)
  deploy --artifact <dir> [flags]
                         Release and deploy a release artifact (not
                         implemented yet)
  setup --import [--dry-run]
                         Import a semantic-release configuration into the
                         release: section (not implemented yet)

Tool commands (not implemented yet):
  install [flags]        Install nerv-release
  upgrade [--alpha | --rc]
                         Replace this binary with the newest release
  configure [flags]      Configure nerv-release
  init [flags]            Write this repository's release.yaml
  version [--json]       Print the binary and plugin versions
`

// options bundles nerv-release's external effects so run can be exercised
// end to end against fakes: main fills the real ones, tests fill scripted
// ones. It only carries what `version next` (U2) needs; later units add
// the fields their own verbs need.
type options struct {
	// ExtensionsFS holds the release extension and its providers; nil
	// means the embedded tree (nervrelease.ExtensionsFS()).
	ExtensionsFS fs.FS
	// Runner runs git for the CLI itself (gitTopLevel); nil means
	// runner.ExecRunner.
	Runner runner.Runner
	// ReleaseProc runs the release extension's provider steps (the
	// version engine's `nerv-release provider release-<name>
	// <operation>`); nil means proc.ExecRunner.
	ReleaseProc proc.Runner
	// Stdin is the hidden `provider` command's request source, when this
	// process is re-invoked as one of its own provider steps
	// (nativeRelease); nil means os.Stdin.
	Stdin io.Reader
	// ComputeVersions is the `nerv-release version next` engine
	// (releaseclone.Compute); tests inject a recorded fake. Nil means
	// releaseclone.Compute.
	ComputeVersions func(context.Context, releaseclone.ComputeOptions) ([]versioning.Result, error)
	// SignalContext derives the context a long-running command runs
	// under, cancelled on SIGINT, SIGTERM and (on Unix) SIGHUP, with a
	// stop function and the signal received (nil if none). Tests inject
	// a controllable one. Nil means signalContext.
	SignalContext func(context.Context) (context.Context, context.CancelFunc, func() os.Signal)
}

func defaultOptions() options {
	return options{
		Runner:          runner.ExecRunner{},
		Stdin:           os.Stdin,
		ComputeVersions: releaseclone.Compute,
		SignalContext:   signalContext,
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, defaultOptions()))
}

// run dispatches the given command-line arguments and returns the process
// exit code. It never touches package-level state so it can be exercised
// directly from tests, with opts carrying every external effect a
// subcommand might need.
func run(args []string, stdout, stderr io.Writer, opts options) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version":
		if len(args) > 1 && args[1] == "next" {
			return runVersionNext(args[2:], stdout, stderr, opts)
		}
		if len(args) == 1 {
			fmt.Fprintln(stderr, "nerv-release version: not implemented yet (binary/plugin version reporting is U6)")
			return 2
		}
		fmt.Fprint(stderr, usage)
		return 2
	case "provider":
		// Hidden: the operations of nerv-release's native providers, run
		// by the release extension (releaseProviders' Call) as
		// `{nerv-release} provider <plugin> <operation>`.
		return runProvider(args[1:], stdout, stderr, opts)
	case "build", "deploy", "setup", "install", "upgrade", "configure", "init":
		fmt.Fprintf(stderr, "nerv-release %s: not implemented yet\n", args[0])
		return 2
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// nativeProviders are the providers nerv-release implements itself, by
// their plugin name (<extension>-<provider>): each is an exec provider
// whose provider.yaml steps run the hidden `nerv-release provider
// <plugin> <operation>` with the common request on stdin and the response
// on stdout.
//
// Only the version engine (release-conventional-commits) is wired up
// today; the native build providers (release-docker-compose, release-bash)
// and the deploy providers are deferred to U4/U5.
var nativeProviders = map[string]func(op string, stdout, stderr io.Writer, opts options) int{
	releaseplan.ConventionalCommits: nativeRelease(releaseplan.ConventionalCommits),
}

// nativeRelease runs one operation of a native release provider: today,
// only the version engine (conventional-commits).
func nativeRelease(plugin string) func(op string, stdout, stderr io.Writer, opts options) int {
	return func(op string, stdout, stderr io.Writer, opts options) int {
		stdin := opts.Stdin
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		signalCtx := opts.SignalContext
		if signalCtx == nil {
			signalCtx = signalContext
		}
		ctx, stop, _ := signalCtx(context.Background())
		defer stop()
		return releaseplan.RunNative(ctx, plugin, op, stdin, stdout, stderr, releaseplan.NativeDeps{})
	}
}

// runProvider is the hidden `nerv-release provider <extension>-<name>
// <operation>`.
func runProvider(args []string, stdout, stderr io.Writer, opts options) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: nerv-release provider <extension>-<provider> <operation> (run by the extension, request on stdin)")
		return 2
	}
	run, ok := nativeProviders[args[0]]
	if !ok {
		names := make([]string, 0, len(nativeProviders))
		for name := range nativeProviders {
			names = append(names, name)
		}
		fmt.Fprintf(stderr, "nerv-release provider: %q is not a native provider yet (%s)\n", args[0], strings.Join(names, ", "))
		return 2
	}
	return run(args[1], stdout, stderr, opts)
}

// releaseProviders calls the release extension's providers from the
// extensions tree (the embedded one when opts has none: the release
// extension always ships its own), their stderr streamed to output.
//
// Unlike nerv, there is no project extension tree to merge in
// (internal/provider dropped it; see its package doc): nerv-release has no
// project-local providers yet.
func releaseProviders(opts options, output io.Writer) releaseplan.Providers {
	fsys := opts.ExtensionsFS
	if fsys == nil {
		fsys = nervrelease.ExtensionsFS()
	}
	return releaseplan.Providers{FS: fsys, Runner: opts.ReleaseProc, Output: output}
}
