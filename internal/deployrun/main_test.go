package deployrun

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deploy"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
)

// fakeTools are the tools deploy runs that the tests replace: each is a
// copy of this test binary named after the tool, so the fakes resolve on
// PATH the same way on every OS (a shell script without an extension is
// invisible to exec.LookPath on Windows, which then finds the developer's
// real docker and gh).
var fakeTools = []string{"gh", "docker", "ssh", "sshpass", "nerv-release"}

// fakeBin is the directory holding the fakes, shared by every test of this
// process; each test points FAKE_DIR at its own directory.
var fakeBin string

// TestMain answers as a fake tool when this binary runs under a fake's
// name, and otherwise creates the fakes, runs the tests and removes them.
func TestMain(m *testing.M) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), exeSuffix())
	if slices.Contains(fakeTools, name) {
		os.Exit(runFake(name, os.Args[1:]))
	}
	dir, err := makeFakeBin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating the fake tools:", err)
		os.Exit(1)
	}
	fakeBin = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// makeFakeBin links (or, across volumes, copies) this test binary once per
// fake tool into a new directory.
func makeFakeBin() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "deployrun-fakes-")
	if err != nil {
		return "", err
	}
	for _, tool := range fakeTools {
		dst := filepath.Join(dir, tool+exeSuffix())
		if os.Link(exe, dst) == nil {
			continue
		}
		if err := copyFile(exe, dst); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// runFake records its name, argv and stdin in FAKE_DIR, and answers as the
// real tool would for what deploy runs: gh release view/create keep a
// release per tag in FAKE_DIR; docker push fails while fail-push exists; a
// nerv-release provider operation answers failed while fail-<operation>
// exists; nerv-release runs the real release-ssh-compose preflight and every
// real release-script operation, and prints an ok response for any other
// operation.
func runFake(name string, args []string) int {
	dir := os.Getenv("FAKE_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "fake", name, "run without FAKE_DIR")
		return 2
	}
	line := name
	for _, a := range args {
		line += " [" + a + "]"
	}
	log, err := os.OpenFile(filepath.Join(dir, "log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Fprintln(log, line)
	log.Close()
	stdin, _ := io.ReadAll(os.Stdin)
	if err := os.WriteFile(filepath.Join(dir, name+".stdin"), stdin, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	exists := func(file string) bool {
		_, err := os.Stat(filepath.Join(dir, file))
		return err == nil
	}
	switch name {
	case "gh":
		if arg(1) == "view" {
			if exists("release-" + arg(2)) {
				return 0
			}
			fmt.Fprintln(os.Stderr, "release not found")
			return 1
		}
		if arg(1) == "create" {
			if err := os.WriteFile(filepath.Join(dir, "release-"+arg(2)), nil, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
	case "docker":
		if arg(0) == "push" && exists("fail-push") {
			fmt.Fprintln(os.Stderr, "denied: push access")
			return 1
		}
	case "nerv-release":
		if arg(0) == "provider" && (arg(2) == "preflight" || arg(1) == "release-script") {
			return runProvider(args[len(args)-2], args[len(args)-1], bytes.NewReader(stdin))
		}
		if exists("fail-" + arg(2)) {
			fmt.Printf(`{"status":"failed","message":"%s broke"}`+"\n", arg(2))
			return 0
		}
		fmt.Printf(`{"status":"ok","message":"%s done"}`+"\n", arg(2))
	}
	return 0
}

// runProvider answers the real provider operation, as the installed
// nerv-release would.
func runProvider(provider, op string, stdin io.Reader) int {
	if provider == "release-script" {
		return deploy.RunBashOp(context.Background(), op, stdin, os.Stdout, deploy.BashDeps{Output: os.Stderr})
	}
	return deploy.RunComposeOp(context.Background(), op, stdin, os.Stdout, deploy.ComposeDeps{Proc: proc.ExecRunner{}, Output: os.Stderr})
}
