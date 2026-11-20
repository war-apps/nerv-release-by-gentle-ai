package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
)

// initRepo is a bare git repository to run `nerv-release init` against
// (it needs a real top level; no secrets or network are involved, only
// local git, same as release_setup_test.go's setupRepo).
func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return repo
}

func runInitIn(t *testing.T, repo string, args []string, home string) (int, string, string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	opts := options{Home: home, Runner: runner.ExecRunner{}}
	code := runInit(args, &stdout, &stderr, opts)
	return code, stdout.String(), stderr.String()
}

func TestRunInit_None_WritesNothing(t *testing.T) {
	repo := initRepo(t)
	code, stdout, stderr := runInitIn(t, repo, []string{"--none"}, t.TempDir())
	if code != 0 {
		t.Fatalf("runInit() code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "nothing written") {
		t.Errorf("stdout = %q, want it to mention nothing written", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".ai-workflow", "release.yaml")); err == nil {
		t.Error("release.yaml was written, want nothing for --none")
	}
}

func TestRunInit_NoFlags_WritesReleaseYAML(t *testing.T) {
	repo := initRepo(t)
	code, stdout, stderr := runInitIn(t, repo, nil, t.TempDir())
	if code != 0 {
		t.Fatalf("runInit() code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Wrote") {
		t.Errorf("stdout = %q, want it to report the written path", stdout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".ai-workflow", "release.yaml")); err != nil {
		t.Errorf("release.yaml was not written: %v", err)
	}
}

func TestRunInit_Import_ReportsSkippedPersonalKeys(t *testing.T) {
	repo := initRepo(t)
	nervConfig := filepath.Join(repo, ".ai-workflow", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(nervConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "deploy:\n  providers:\n    ssh-compose:\n      environment: staging\n      ssh_auth: key\n"
	if err := os.WriteFile(nervConfig, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runInitIn(t, repo, []string{"--import"}, t.TempDir())
	if code != 0 {
		t.Fatalf("runInit() code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Imported") {
		t.Errorf("stdout = %q, want it to report the import", stdout)
	}
	if !strings.Contains(stdout, "ssh_auth") {
		t.Errorf("stdout = %q, want it to list the skipped personal key", stdout)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".ai-workflow", "release.yaml"))
	if err != nil {
		t.Fatalf("release.yaml was not written: %v", err)
	}
	if strings.Contains(string(data), "ssh_auth") {
		t.Errorf("release.yaml = %q, must never carry a personal key", data)
	}
	if !strings.Contains(string(data), "environment: staging") {
		t.Errorf("release.yaml = %q, want the imported repo-scope key", data)
	}
}
