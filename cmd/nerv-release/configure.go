package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/configure"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

const configureUsage = `Usage: nerv-release configure [--print] [--json] [--set key=value] [--answers <file>]

With no mode flag: on a terminal, runs the interactive wizard (for every
provider with a scope: user key, asks whether to configure it, then
prompts each key with its manifest prompt and current value or
default, with a diff and a confirmation before writing); without a
terminal, prints the current configuration and how to set a key (no
prompts).

--print            Print the global release.yaml: its config path,
                    every scope: user key's resolved value, and every
                    known provider's config schema.
--json             Render --print as JSON instead of text.
--set key=value    Set one provider's scope: user key
                    (<provider>.<key>). May be given more than once.
--answers <file>   Drive the interactive wizard from a file instead of
                    a real terminal, one answer per line. A missing
                    file is treated as empty.
`

// stringList is a repeatable flag.Value, for --set.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runConfigure is `nerv-release configure` (R-S2, U6).
func runConfigure(args []string, stdout, stderr io.Writer, opts options) int {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	printFlag := fs.Bool("print", false, "")
	jsonFlag := fs.Bool("json", false, "")
	answersFlag := fs.String("answers", "", "")
	var sets stringList
	fs.Var(&sets, "set", "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, configureUsage)
		return 2
	}

	home, err := resolveHome(opts.Home)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
		return 2
	}
	deps := configureDeps(home)

	for _, kv := range sets {
		result, err := configure.Set(deps, kv)
		if err != nil {
			fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s: %s -> %s\n", result.Key, orDash(result.Old), result.New)
	}

	flagMode := len(sets) > 0 || *printFlag || *jsonFlag
	if flagMode {
		if len(sets) > 0 && !*printFlag && !*jsonFlag {
			return 0
		}
		return printConfigure(deps, stdout, stderr, *jsonFlag)
	}

	// No mode flag: the interactive wizard, driven by --answers when
	// given, otherwise the real terminal; without either, print the
	// current configuration and how to set a key (no prompts).
	switch {
	case *answersFlag != "":
		in, err := openAnswersFile(*answersFlag)
		if err != nil {
			fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
			return 2
		}
		return runConfigureWizard(deps, in, stdout, stderr)
	case opts.IsTerminal != nil && opts.IsTerminal() && opts.Stdin != nil:
		return runConfigureWizard(deps, opts.Stdin, stdout, stderr)
	default:
		if code := printConfigure(deps, stdout, stderr, false); code != 0 {
			return code
		}
		fmt.Fprintln(stdout, "\nstdin is not a terminal: set a key with --set key=value, or run on a terminal (or with --answers <file>) to configure interactively.")
		return 0
	}
}

// runConfigureWizard runs the interactive wizard against in, reporting
// configure.ErrInputClosed (a genuinely exhausted reader, such as an
// --answers file that ran out) as a refusal that wrote nothing.
func runConfigureWizard(deps configure.Deps, in io.Reader, stdout, stderr io.Writer) int {
	if _, err := configure.RunWizard(deps, in, stdout); err != nil {
		fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
		return 1
	}
	return 0
}

// openAnswersFile reads path's whole content up front (small text
// files, one answer per line). A missing file is treated as an empty
// answers file -- never an error.
func openAnswersFile(path string) (io.Reader, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return strings.NewReader(""), nil
		}
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func printConfigure(deps configure.Deps, stdout, stderr io.Writer, asJSON bool) int {
	out, err := configure.Print(deps)
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "nerv-release configure: %v\n", err)
			return 1
		}
		return 0
	}
	printConfigureText(stdout, out)
	return 0
}

func configureDeps(home string) configure.Deps {
	return configure.Deps{
		GlobalPath:   aiworkflow.UserConfigPath(home, runtime.GOOS, workflowconfig.Tool),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          time.Now,
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printConfigureText(stdout io.Writer, out configure.PrintOutput) {
	fmt.Fprintf(stdout, "config_path: %s\n", out.ConfigPath)
	fmt.Fprintln(stdout, "values:")
	for _, kv := range out.Values {
		state := "default"
		if kv.Set {
			state = "set"
		}
		fmt.Fprintf(stdout, "  %s: %s (%s)\n", kv.Path, orDash(kv.Value), state)
	}
	fmt.Fprintln(stdout, "providers:")
	for _, p := range out.Providers {
		fmt.Fprintf(stdout, "  - %s (slot %s)\n", p.Name, p.Slot)
		for _, c := range p.Config {
			fmt.Fprintf(stdout, "      %s.%s  scope=%s type=%s\n", p.Name, c.Key, c.Scope, c.Type)
		}
	}
}
