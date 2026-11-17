package workflowconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

// goos is fixed to "linux" for every test (TestMain clears
// XDG_CONFIG_HOME), so the user path is always built from each test's
// fake home, whatever OS actually runs the suite.
const goos = "linux"

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func userPath(home string) string {
	return aiworkflow.UserConfigPath(home, goos, workflowconfig.Tool)
}

func projectPath(repo string) string {
	return aiworkflow.ProjectConfigPath(repo, workflowconfig.Tool)
}

func TestLoad_ReadsTheTwoFiles(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, userPath(home), "defaults:\n  release:\n    version: conventional-commits\n")
	write(t, projectPath(repo), "release:\n  version: conventional-commits\n")

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserPath != userPath(home) || cfg.ProjectPath != projectPath(repo) {
		t.Errorf("paths = %q, %q", cfg.UserPath, cfg.ProjectPath)
	}
	if !cfg.UserExists || !cfg.ProjectExists {
		t.Errorf("exists = %v, %v; want both true", cfg.UserExists, cfg.ProjectExists)
	}
	if v, found := cfg.Scalar("release.version"); !found || v != "conventional-commits" {
		t.Errorf("release.version = %q, %v; want conventional-commits, true (from the project file)", v, found)
	}
}

func TestLoad_MissingFilesAreNotAnError(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserExists || cfg.ProjectExists {
		t.Errorf("exists = %v, %v; want both false", cfg.UserExists, cfg.ProjectExists)
	}
	if v, found := cfg.Scalar("release.version"); found {
		t.Errorf("release.version = %q, want not found", v)
	}
}

func TestLoad_NoRepoReadsOnlyTheUserFile(t *testing.T) {
	home := t.TempDir()
	write(t, userPath(home), "defaults:\n  release:\n    version: conventional-commits\n")

	cfg, err := workflowconfig.Load(home, goos, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectPath != "" || cfg.ProjectExists {
		t.Errorf("cfg = %+v, want no project", cfg)
	}
	if !cfg.UserExists {
		t.Error("UserExists = false, want true")
	}
}

func TestLoad_NoHomeReadsOnlyTheProjectFile(t *testing.T) {
	repo := t.TempDir()
	write(t, projectPath(repo), "release:\n  version: conventional-commits\n")

	cfg, err := workflowconfig.Load("", goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserPath != "" || cfg.UserExists {
		t.Errorf("cfg = %+v, want no user", cfg)
	}
	if v, _ := cfg.Scalar("release.version"); v != "conventional-commits" {
		t.Errorf("release.version = %q", v)
	}
}

// release and deploy are both project-only, except their personal (scope:
// user) provider keys: without Personal set (the zero value), neither
// section is ever read from the user file.
func TestScalar_ExtensionSectionsAreReadFromTheProjectOnly(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, userPath(home), "release:\n  version: conventional-commits\ndeploy:\n  host: github\n  providers:\n    ssh-compose:\n      ssh_auth: key\n")
	write(t, projectPath(repo), "deploy:\n  host: gitea\n")

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"release.version", "deploy.providers.ssh-compose.ssh_auth"} {
		if v, found := cfg.Scalar(key); found {
			t.Errorf("%s = %q from the user file without Personal; want unset", key, v)
		}
	}
	if v, found := cfg.Scalar("deploy.host"); !found || v != "gitea" {
		t.Errorf("deploy.host = %q, %v; want gitea, true (the project wins)", v, found)
	}
}
