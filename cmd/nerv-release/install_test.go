package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner/runnertest"
)

func fakeInstallOptions(t *testing.T, terminal bool, answers string) options {
	t.Helper()
	return options{
		Home:       t.TempDir(),
		IsTerminal: func() bool { return terminal },
		Stdin:      strings.NewReader(answers),
		Runner:     &runnertest.FakeRunner{},
	}
}

func TestRunInstall_FreshMachine_InstallsReleaseAndReportsRestart(t *testing.T) {
	opts := fakeInstallOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runInstall([]string{"--no-configure"}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runInstall() code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "nerv-release installed") {
		t.Errorf("stdout = %q, want an install confirmation", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Restart Claude Code") {
		t.Errorf("stdout = %q, want the restart hint", stdout.String())
	}
}

func TestRunInstall_Terminal_OffersConfigure_YesRunsWizard(t *testing.T) {
	// "Run nerv-release configure now?" -> y, then "Configure
	// ssh-compose?" -> n (the only provider with scope: user keys), so
	// the wizard reaches its end with no change.
	opts := fakeInstallOptions(t, true, "y\nn\n")
	var stdout, stderr bytes.Buffer
	code := runInstall(nil, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runInstall() code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Errorf("stdout = %q, want the wizard banner after accepting the offer", stdout.String())
	}
}

func TestRunInstall_Terminal_OffersConfigure_NoSkipsWizard(t *testing.T) {
	opts := fakeInstallOptions(t, true, "n\n")
	var stdout, stderr bytes.Buffer
	code := runInstall(nil, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runInstall() code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Error("declining the offer must never run the wizard")
	}
}

func TestRunInstall_NoConfigureFlag_NeverOffers(t *testing.T) {
	opts := fakeInstallOptions(t, true, "")
	var stdout, stderr bytes.Buffer
	code := runInstall([]string{"--no-configure"}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runInstall() code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Run nerv-release configure now?") {
		t.Error("--no-configure must never offer to run configure")
	}
	if strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Error("--no-configure must never run the wizard")
	}
}

func TestRunInstall_NonTerminal_PrintsHintInsteadOfOffering(t *testing.T) {
	opts := fakeInstallOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runInstall(nil, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runInstall() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nerv-release configure") {
		t.Errorf("stdout = %q, want a hint to run nerv-release configure later", stdout.String())
	}
	if strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Error("a non-terminal run must never run the wizard")
	}
}
