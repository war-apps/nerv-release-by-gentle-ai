package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployrun"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/refusal"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

const deployUsage = `Usage: nerv-release deploy --artifact <dir> [flags]

Releases and deploys an artifact nerv-release build left (.nerv/artifacts/<project>/
<version>/), as the deploy: section of .ai-workflow/release.yaml says. It verifies
the artifact's digests, clones the remote (deploy.git.remote, default
origin) in a temporary directory at the artifact's base commit and:

  1. commits the artifact's release files (CHANGELOG, version files) with
     deploy.git.commit_message (default @semantic-release/git's
     "chore(release): ${nextRelease.version} [skip ci]" and the notes);
  2. creates the lightweight tag and semantic-release's channel note
     refs/notes/semantic-release-<tag>;
  3. pushes the branch, the tag and the note atomically (never forced);
  4. publishes the artifact's images: docker load of its image tars, then
     docker push of every image of its manifest (skipped without images,
     or with deploy.publish: false);
  5. creates the git host's release (deploy.host), a prerelease for a
     release on a channel, unless it exists;
  6. runs every target's publish, then each target's deploy and verify, in
     deploy.targets order. Targets are optional: without any, the deploy
     is the release alone and connects to no server.

Before anything is cloned or pushed, docker and each image registry's
credentials in the docker config are checked (when there are images to
publish), and each target's preflight checks the target's config and that
the tools its provider needs are installed (release-ssh-compose: sshpass for
password authentication, infisical for source infisical).

Your repository is never modified. When origin's branch moved past the
artifact's base commit the deploy is refused, unless --rebase re-applies the
release files on its new head (a conflict is refused; the tag then points at
code nerv-release build never built, and a warning says so). A re-run is
idempotent: what origin already has is skipped, so after a failure, fix the
cause and run the same command again. Without --yes it asks before pushing.

Flags:
  --artifact <dir>   The artifact directory (required)
  --target <name>    Only this target of deploy.targets (repeatable)
  --dry-run          Prepare the release in the clone and show the steps;
                     push nothing and run no provider operation but the
                     targets' preflight
  --rebase           Re-apply the release on origin's moved branch (the
                     tagged code was never built: prefer releasing again)
  --yes              Deploy without asking for confirmation
  --cwd <dir>        A directory inside the repository (default ".")
  --json             Print the final report as JSON

Progress goes to stderr, the report to stdout. Exit codes: 0 deployed (and
a dry run), 1 refused (invalid configuration or artifact, origin moved, a
missing tool, no confirmation; nothing was written), 2 a step failed (the
report says which steps were done), 128 + the signal when interrupted.
`

type deployCmd struct {
	opts                options
	cwd, dir            string
	targets             []string
	dryRun, rebase, yes bool
	stdout, stderr      io.Writer
	asJSON              bool
}

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

// runDeploy is `nerv-release deploy`.
func runDeploy(args []string, stdout, stderr io.Writer, opts options) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	d := deployCmd{opts: opts, stdout: stdout, stderr: stderr}
	var targets repeated
	fs.StringVar(&d.cwd, "cwd", ".", "")
	fs.StringVar(&d.dir, "artifact", "", "")
	fs.Var(&targets, "target", "")
	fs.BoolVar(&d.dryRun, "dry-run", false, "")
	fs.BoolVar(&d.rebase, "rebase", false, "")
	fs.BoolVar(&d.yes, "yes", false, "")
	fs.BoolVar(&d.asJSON, "json", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, deployUsage)
			return 0
		}
		return deployUsageError(stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return deployUsageError(stderr, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	if d.dir == "" {
		return deployUsageError(stderr, "--artifact is required")
	}
	d.targets = targets

	signalCtx := opts.SignalContext
	if signalCtx == nil {
		signalCtx = signalContext
	}
	ctx, stop, received := signalCtx(context.Background())
	defer stop()
	rep, err := d.run(ctx)
	code := 0
	switch {
	case ctx.Err() != nil:
		code = interruptedExitCode(received())
		fmt.Fprintln(stderr, "nerv-release deploy: interrupted")
	case err != nil:
		code = 2
		if refusal.Is(err) {
			code = 1
		}
		fmt.Fprintf(stderr, "nerv-release deploy: %v\n", err)
	}
	if werr := writeDeployReport(stdout, d.asJSON, rep, err); werr != nil && code == 0 {
		code = 2
	}
	return code
}

func deployUsageError(stderr io.Writer, problem string) int {
	fmt.Fprintf(stderr, "nerv-release deploy: %s\n\n%s", problem, deployUsage)
	return 2
}

func (d deployCmd) run(ctx context.Context) (deployrun.Report, error) {
	top, err := gitTopLevel(ctx, d.opts.Runner, d.cwd)
	if err != nil {
		return deployrun.Report{}, err
	}
	cfg, err := deployconfig.Load(top)
	switch {
	case errors.Is(err, deployconfig.ErrNotConfigured):
		return deployrun.Report{}, refuse("the repository has no deploy: section in .ai-workflow/release.yaml")
	case err != nil:
		return deployrun.Report{}, &refusal.Error{Err: err}
	}
	home, err := resolveHome(d.opts.Home)
	if err != nil {
		return deployrun.Report{}, err
	}
	wc, err := workflowconfig.Load(home, runtime.GOOS, "")
	if err != nil {
		return deployrun.Report{}, err
	}
	dir := d.dir
	if !filepath.IsAbs(dir) {
		cwd := d.cwd
		if !filepath.IsAbs(cwd) {
			if cwd, err = filepath.Abs(cwd); err != nil {
				return deployrun.Report{}, err
			}
		}
		dir = filepath.Join(cwd, dir)
	}
	fsys := d.opts.ExtensionsFS
	if fsys == nil {
		fsys = nervrelease.ExtensionsFS()
	}
	var runner proc.Runner = proc.ExecRunner{}
	if d.opts.DeployProc != nil {
		runner = d.opts.DeployProc
	}
	o := deployrun.Options{
		RepoRoot: top, ArtifactDir: dir, Config: cfg, Targets: d.targets, DryRun: d.dryRun, Rebase: d.rebase,
		Providers: fsys, Proc: runner, Output: d.stderr,
	}
	if wc.Personal, err = workflowconfig.LoadPersonal(fsys); err != nil {
		return deployrun.Report{}, err
	}
	o.UserValue = userProviderValue(wc)
	if !d.dryRun {
		o.Confirm = d.confirm
	}
	return deployrun.Run(ctx, o)
}

// userProviderValue reads a deploy provider's config key from the user
// file, at deploy.providers.<provider>.<key>, only when the key is
// personal (scope: user in its manifest, wc.Personal): every other deploy
// key comes from the project file.
func userProviderValue(wc workflowconfig.Config) func(provider, key string) (string, bool) {
	return func(provider, key string) (string, bool) {
		path := "deploy.providers." + provider + "." + key
		if wc.User == nil || !wc.Personal.Has(path) {
			return "", false
		}
		return wc.User.Scalar(path)
	}
}

// confirm shows the prepared release and asks before pushing on an
// interactive terminal; --yes skips it, and without a terminal --yes is
// required.
func (d deployCmd) confirm(rep deployrun.Report) error {
	if d.yes {
		return nil
	}
	if d.opts.IsTerminal == nil || !d.opts.IsTerminal() || d.opts.Stdin == nil {
		return refuse("confirmation needed but stdin is not an interactive terminal: pass --yes to deploy without asking (or --dry-run to preview)")
	}
	fmt.Fprintf(d.stderr, "nerv-release deploy: %s %s (%s) is ready:\n", rep.Project, rep.Version, rep.Tag)
	rep.Write(d.stderr)
	fmt.Fprint(d.stderr, "Push the release and deploy it? [y/N]: ")
	line, sawNewline := readAnswer(d.opts.Stdin)
	if !sawNewline {
		fmt.Fprintln(d.stderr)
	}
	if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
		return refuse("cancelled: nothing was pushed or deployed")
	}
	return nil
}

// writeDeployReport writes the report: JSON, or one line per step.
func writeDeployReport(w io.Writer, asJSON bool, rep deployrun.Report, runErr error) error {
	if asJSON {
		out := struct {
			deployrun.Report
			Error string `json:"error,omitempty"`
		}{Report: rep}
		if out.Steps == nil {
			out.Steps = []deployrun.Step{}
		}
		if runErr != nil {
			out.Error = runErr.Error()
		}
		data, err := json.Marshal(out)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	}
	if len(rep.Steps) == 0 {
		return nil
	}
	status := "deployed"
	switch {
	case runErr != nil:
		status = "failed"
	case rep.DryRun:
		status = "dry run"
	}
	if _, err := fmt.Fprintf(w, "deploy %s %s: %s\n", rep.Project, rep.Version, status); err != nil {
		return err
	}
	rep.Write(w)
	return nil
}
