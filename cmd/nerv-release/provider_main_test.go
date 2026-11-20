package main

import (
	"os"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/testenv"
)

// TestMain lets the release flow under test run its native providers
// through this test binary, as the real nerv-release runs them:
// `nerv-release version next` calls {nerv-release} (os.Executable, the
// test binary here) as `<nerv-release> provider release-<name>
// <operation>`, which runs the hidden command instead of the tests. The
// test flags always come first otherwise, so a normal test run never
// matches.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "provider" {
		opts := defaultOptions()
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, opts))
	}
	os.Exit(testenv.Run(m))
}
