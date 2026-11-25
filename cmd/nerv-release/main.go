// Command nerv-release is the release tool of the NERV ecosystem,
// independent of nerv: version computation, builds and deploys.
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deploy"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseclone"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseplan"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaserun"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versioning"
)

// usage lists every verb R-S2 plans for nerv-release
// (odd/tasks/initial-release.md). "version next", "build", "deploy" and
// "setup" work since U2-U4; install/upgrade/configure/init/version are
// U6a.
const usage = `Usage: nerv-release <command> [flags]

Engine commands:
  version next [flags]  Compute the next release version with the native
                         engine (see "nerv-release version next --help")
  build [flags]          Build, test and package origin's code into a local
                         artifact, no git writes (see "nerv-release build --help")
  deploy --artifact <dir> [flags]
                         Release and deploy a release artifact (see
                         "nerv-release deploy --help")
  setup --import [--force] [--dry-run]
                         Import a semantic-release configuration into the
                         release: section (see "nerv-release setup --help")
  migrate [--cwd <dir>] [--user] [--dry-run]
                         Convert the legacy release and deploy sections nerv
                         moved into release.yaml (see "nerv-release migrate --help")

Tool commands:
  install [--no-configure]
                         Install nerv-release (see "nerv-release install --help")
  upgrade [--alpha | --rc]
                         Replace this binary with the newest release
  configure [--print] [--json] [--set k=v] [--answers f]
                         Configure nerv-release (see "nerv-release configure --help")
  init [--import] [--none]
                         Write this repository's release.yaml (see
                         "nerv-release init --help")
  version [--json]       Print the binary and plugin versions
`

// options bundles nerv-release's external effects so run can be exercised
// end to end against fakes: main fills the real ones, tests fill scripted
// ones.
type options struct {
	// ExtensionsFS holds the release extension and its providers; nil
	// means the embedded tree (nervrelease.ExtensionsFS()).
	ExtensionsFS fs.FS
	// Runner runs git for the CLI itself (gitTopLevel); nil means
	// runner.ExecRunner.
	Runner runner.Runner
	// ReleaseProc runs the release extension's provider steps (the
	// version and build providers' `nerv-release provider release-<name>
	// <operation>`); nil means proc.ExecRunner.
	ReleaseProc proc.Runner
	// DeployProc runs the deploy providers' commands (nerv-release
	// deploy's host and target providers, and the native ssh-compose and
	// script operations); nil means proc.ExecRunner.
	DeployProc proc.Runner
	// Stdin is the hidden `provider` command's request source, when this
	// process is re-invoked as one of its own provider steps
	// (nativeRelease), and the confirmation prompts' source; nil means
	// os.Stdin.
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
	// OpenReleaseWorkspace opens the release clone `nerv-release build`
	// works in; nil means releaseclone.New. Tests inject a fake.
	OpenReleaseWorkspace func(context.Context, releaseclone.Options) (releaseWorkspace, error)
	// ReleaseSteps returns the step runner `nerv-release build` runs the
	// pipeline with, given the release clone's root and where to stream
	// output; nil means a *releaserun.Runner.
	ReleaseSteps func(root string, out io.Writer) releaseplan.StepRunner
	// IsTerminal reports whether stdin is attached to a real interactive
	// terminal (confirmation prompts); nil means stdinIsTerminal.
	IsTerminal func() bool
	// Home overrides the resolved home directory (the user's global
	// release.yaml, deploy.md). Empty means runner.HomeDir() decides. Not
	// a public flag: settable only here, only for tests.
	Home string
	// Getenv reads an environment variable (release-ssh-compose's
	// NERV_DEPLOY_* variables); nil means a function always returning "".
	Getenv func(string) string
	// DeploySleep is release-ssh-compose's poll sleep (verify's health
	// wait); nil means time.Sleep.
	DeploySleep func(context.Context, time.Duration) error
}

func defaultOptions() options {
	return options{
		Runner:          runner.ExecRunner{},
		Stdin:           os.Stdin,
		ComputeVersions: releaseclone.Compute,
		SignalContext:   signalContext,
		IsTerminal:      stdinIsTerminal,
		ReleaseSteps: func(root string, out io.Writer) releaseplan.StepRunner {
			return &releaserun.Runner{RepoRoot: root, Output: out}
		},
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
		return runVersion(args[1:], stdout, stderr)
	case "provider":
		// Hidden: the operations of nerv-release's native providers, run
		// by the release extension (releaseProviders' Call) as
		// `{nerv-release} provider <plugin> <operation>`.
		return runProvider(args[1:], stdout, stderr, opts)
	case "setup":
		return runReleaseSetup(args[1:], stdout, stderr, opts)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr, opts)
	case "build":
		return runBuild(args[1:], stdout, stderr, opts)
	case "deploy":
		return runDeploy(args[1:], stdout, stderr, opts)
	case "install":
		return runInstall(args[1:], stdout, stderr, opts)
	case "upgrade":
		return runUpgrade(args[1:], stdout, stderr, opts)
	case "configure":
		return runConfigure(args[1:], stdout, stderr, opts)
	case "init":
		return runInit(args[1:], stdout, stderr, opts)
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
var nativeProviders = map[string]func(op string, stdout, stderr io.Writer, opts options) int{
	releaseplan.ConventionalCommits: nativeRelease(releaseplan.ConventionalCommits),
	releaseplan.DockerCompose:       nativeRelease(releaseplan.DockerCompose),
	releaseplan.Bash:                nativeRelease(releaseplan.Bash),
	"release-script":                runDeployScript,
	"release-ssh-compose":           runDeploySSHCompose,
}

// nativeRelease runs one operation of a native release provider: the
// version engine (conventional-commits) or a build provider
// (docker-compose, bash).
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

// runDeployScript runs one operation of the release-script target
// provider: the project's own deploy script, taken from the release
// commit.
func runDeployScript(op string, stdout, stderr io.Writer, opts options) int {
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
	return deploy.RunBashOp(ctx, op, stdin, stdout, deploy.BashDeps{Output: stderr})
}

// runDeploySSHCompose runs one operation of the release-ssh-compose
// provider (preflight, deploy or verify): the request on stdin, the JSON
// response on stdout.
func runDeploySSHCompose(op string, stdout, stderr io.Writer, opts options) int {
	home, err := resolveHome(opts.Home)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release provider release-ssh-compose: %v\n", err)
		return 2
	}
	var runner proc.Runner = proc.ExecRunner{}
	if opts.DeployProc != nil {
		runner = opts.DeployProc
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
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
	return deploy.RunComposeOp(ctx, op, stdin, stdout, deploy.ComposeDeps{
		Proc: runner, Getenv: getenv, Home: home, Output: stderr, Sleep: opts.DeploySleep,
	})
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
		slices.Sort(names)
		fmt.Fprintf(stderr, "nerv-release provider: %q is not a native provider (%s)\n", args[0], strings.Join(names, ", "))
		return 2
	}
	return run(args[1], stdout, stderr, opts)
}

// resolveHome is the user's home directory, or override when not empty
// (options.Home: settable only there, only for tests).
func resolveHome(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return runner.HomeDir()
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
