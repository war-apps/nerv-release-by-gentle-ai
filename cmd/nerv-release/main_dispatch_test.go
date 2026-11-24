package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner/runnertest"
)

// These are smoke tests for the dispatcher itself (run's switch), not
// for each verb's own behavior (covered by install_test.go,
// configure_test.go, init_test.go, upgrade_test.go and version_test.go
// against their own run* functions directly).
func TestRun_DispatchesToEachU6aToolVerb(t *testing.T) {
	opts := options{Home: t.TempDir(), Runner: &runnertest.FakeRunner{}}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"install", "--no-configure"}, &stdout, &stderr, opts); code != 0 {
		t.Errorf("run([install]) code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"configure", "--print"}, &stdout, &stderr, opts); code != 0 {
		t.Errorf("run([configure]) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config_path:") {
		t.Errorf("run([configure, --print]) stdout = %q, want the printed configuration", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"upgrade", "--alpha", "--rc"}, &stdout, &stderr, opts); code != 2 {
		t.Errorf("run([upgrade]) code = %d, want 2 (usage error), stderr = %q", code, stderr.String())
	}
}

func TestRun_DispatchesInit(t *testing.T) {
	opts := options{Home: t.TempDir(), Runner: &runnertest.FakeRunner{}}
	var stdout, stderr bytes.Buffer
	// Run outside this repo's own worktree is not guaranteed; init only
	// needs a git top level to exist, so a usage error (--bogus) is
	// enough to prove the dispatcher reached runInit without touching
	// any repository.
	if code := run([]string{"init", "--bogus"}, &stdout, &stderr, opts); code != 2 {
		t.Errorf("run([init, --bogus]) code = %d, want 2, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: nerv-release init") {
		t.Errorf("stderr = %q, want init's own usage", stderr.String())
	}
}

// nerv-release build -h is build's own help: it ends with its exit codes
// and carries no other command's usage.
func TestBuildHelp_IsOnlyBuildsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", "-h"}, &stdout, &stderr, options{}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	out := stdout.String() + stderr.String()
	if strings.Contains(out, "setup --import") {
		t.Errorf("build -h carries setup's usage:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "signal when interrupted.") {
		t.Errorf("build -h does not end with its exit codes:\n%s", out)
	}
}
