package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/initrepo"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

const initUsage = `Usage: nerv-release init [--import] [--none]

Writes this repository's own .ai-workflow/release.yaml, seeded from the
global release.yaml's defaults: block.

--import  Copy nerv's own .ai-workflow/config.yaml release:/deploy:
          blocks into the new repository file, repo-scope keys only. A
          repository's own file is untrusted input: personal
          (scope: user) keys are never written anywhere, including the
          global release.yaml -- they are reported as skipped, for you
          to set yourself with nerv-release configure.
--none    Write nothing: release configuration stays off here.
`

// runInit is `nerv-release init` (R-S2, R-S4, U6).
func runInit(args []string, stdout, stderr io.Writer, opts options) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	importFlag := fs.Bool("import", false, "")
	noneFlag := fs.Bool("none", false, "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, initUsage)
		return 2
	}

	releaseRunner := opts.Runner
	if releaseRunner == nil {
		releaseRunner = runner.ExecRunner{}
	}
	top, err := gitTopLevel(context.Background(), releaseRunner, ".")
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release init: %v\n", err)
		return 2
	}

	home, err := resolveHome(opts.Home)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release init: %v\n", err)
		return 2
	}

	deps := initrepo.Deps{
		RepoRoot:     top,
		GlobalPath:   aiworkflow.UserConfigPath(home, runtime.GOOS, workflowconfig.Tool),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          time.Now,
	}

	result, err := initrepo.Init(deps, initrepo.Options{
		Import: *importFlag,
		None:   *noneFlag,
	})
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release init: %v\n", err)
		return 1
	}

	if result.Skipped {
		fmt.Fprintln(stdout, "Release configuration stays off in this repository (--none): nothing written.")
		return 0
	}
	if result.ImportedFrom != "" {
		fmt.Fprintf(stdout, "Imported %s's release:/deploy: blocks.\n", result.ImportedFrom)
		for _, msg := range result.SkippedPersonalKeys {
			fmt.Fprintf(stdout, "  skipped (personal): %s\n", msg)
		}
	}
	if result.SeededFromDefaults {
		fmt.Fprintln(stdout, "Seeded from the global release.yaml's defaults.")
	}
	fmt.Fprintf(stdout, "Wrote %s\n", result.Path)
	return 0
}
