package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeConfigureOptions(t *testing.T, terminal bool, answers string) options {
	t.Helper()
	return options{
		Home:       t.TempDir(),
		IsTerminal: func() bool { return terminal },
		Stdin:      strings.NewReader(answers),
	}
}

func TestRunConfigure_Terminal_NoFlags_RunsWizard(t *testing.T) {
	// "Configure ssh-compose?" -> n: nothing changes, nothing written.
	opts := fakeConfigureOptions(t, true, "n\n")
	var stdout, stderr bytes.Buffer
	code := runConfigure(nil, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Errorf("stdout = %q, want the wizard banner", stdout.String())
	}
}

func TestRunConfigure_NonTerminal_NoFlags_PrintsCurrentConfig_NoPrompts(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure(nil, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config_path:") {
		t.Errorf("stdout = %q, want the printed configuration", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--set") {
		t.Errorf("stdout = %q, want a hint on how to set a key", stdout.String())
	}
}

func TestRunConfigure_Answers_DrivesWizardWithoutATerminal(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	answersFile := filepath.Join(t.TempDir(), "answers.txt")
	if err := os.WriteFile(answersFile, []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--answers", answersFile}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Errorf("stdout = %q, want the wizard banner", stdout.String())
	}
}

func TestRunConfigure_Answers_MissingFile_TreatedAsEmpty(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--answers", filepath.Join(t.TempDir(), "missing.txt")}, &stdout, &stderr, opts)
	// An empty answers file hits EOF on the very first prompt: ErrInputClosed.
	if code != 1 {
		t.Fatalf("runConfigure() code = %d, want 1 (input closed); stderr = %q", code, stderr.String())
	}
}

func TestRunConfigure_PrintFlag_IgnoresTerminalAndAnswers(t *testing.T) {
	opts := fakeConfigureOptions(t, true, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--print"}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "nerv-release configuration") {
		t.Error("--print must never run the interactive wizard")
	}
	if !strings.Contains(stdout.String(), "config_path:") {
		t.Errorf("stdout = %q, want the printed configuration", stdout.String())
	}
}

func TestRunConfigure_PrintJSON(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--print", "--json"}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"config_path"`) {
		t.Errorf("stdout = %q, want JSON with config_path", stdout.String())
	}
}

func TestRunConfigure_Set_PersistsAndPrintsOldNew(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--set", "ssh-compose.ssh_key_file=/etc/nerv/key"}, &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("runConfigure() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ssh-compose.ssh_key_file") {
		t.Errorf("stdout = %q, want it to report the set key", stdout.String())
	}
}

func TestRunConfigure_Set_RepoScopeKey_Refuses(t *testing.T) {
	opts := fakeConfigureOptions(t, false, "")
	var stdout, stderr bytes.Buffer
	code := runConfigure([]string{"--set", "ssh-compose.environment=prod"}, &stdout, &stderr, opts)
	if code == 0 {
		t.Fatal("runConfigure() code = 0, want a refusal for a scope: repo key")
	}
}
