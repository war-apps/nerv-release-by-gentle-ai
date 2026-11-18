package initrepo_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/initrepo"
)

// trySymlink creates a symlink, skipping the test (not failing it) when
// the current machine refuses symlink creation for lack of privilege --
// common on Windows without Developer Mode or admin rights. These cases
// still run on Linux CI.
func trySymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) {
			t.Skipf("creating symlinks needs privileges on this machine: %v", err)
		}
		t.Fatal(err)
	}
}

// TestInit_SymlinkedAiWorkflowDir_Refused is the directory-escape case:
// a cloned repository ships .ai-workflow itself as a symlink (e.g. to
// the user's global config directory), which would otherwise make init
// write repository-derived, untrusted content there, bypassing the
// personal-key protection entirely.
func TestInit_SymlinkedAiWorkflowDir_Refused(t *testing.T) {
	repo := t.TempDir()
	elsewhere := t.TempDir()
	trySymlink(t, elsewhere, filepath.Join(repo, ".ai-workflow"))

	globalPath := filepath.Join(t.TempDir(), "release.yaml")
	if err := os.WriteFile(globalPath, []byte("defaults:\n  release:\n    projects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}

	deps := initrepo.Deps{
		RepoRoot:     repo,
		GlobalPath:   globalPath,
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	if _, err := initrepo.Init(deps, initrepo.Options{}); err == nil {
		t.Fatal("Init() error = nil, want a refusal for a symlinked .ai-workflow directory")
	}

	if _, err := os.Stat(filepath.Join(elsewhere, "release.yaml")); err == nil {
		t.Error("release.yaml was written through the symlink target")
	}
	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("the global release.yaml was modified despite the refusal")
	}
}

// TestInit_SymlinkedReleaseYAML_Refused is the file-escape case: the
// .ai-workflow directory is real, but release.yaml inside it is a
// symlink to a file outside the repository (such as the user's own
// global release.yaml): writing through it would silently overwrite
// that file with untrusted, repository-derived content.
func TestInit_SymlinkedReleaseYAML_Refused(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".ai-workflow"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "victim.yaml")
	if err := os.WriteFile(target, []byte("release:\n  original: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trySymlink(t, target, filepath.Join(repo, ".ai-workflow", "release.yaml"))

	deps := initrepo.Deps{
		RepoRoot:     repo,
		GlobalPath:   filepath.Join(t.TempDir(), "release.yaml"),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	if _, err := initrepo.Init(deps, initrepo.Options{}); err == nil {
		t.Fatal("Init() error = nil, want a refusal for a symlinked release.yaml")
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "release:\n  original: true\n" {
		t.Errorf("victim.yaml = %q, want it untouched", data)
	}
}

// TestInit_Import_SymlinkedNervConfigYAML_Refused: a cloned repository
// must not be able to make --import read through a symlink pointing
// outside the repository.
func TestInit_Import_SymlinkedNervConfigYAML_Refused(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".ai-workflow"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "victim-config.yaml")
	if err := os.WriteFile(target, []byte("release:\n  projects:\n    - name: victim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trySymlink(t, target, filepath.Join(repo, ".ai-workflow", "config.yaml"))

	deps := initrepo.Deps{
		RepoRoot:     repo,
		GlobalPath:   filepath.Join(t.TempDir(), "release.yaml"),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	if _, err := initrepo.Init(deps, initrepo.Options{Import: true}); err == nil {
		t.Fatal("Init() error = nil, want a refusal for a symlinked config.yaml")
	}
}

// TestInit_NormalPaths_StillWork is the regression guard: ordinary,
// non-symlinked repositories must keep working exactly as before.
func TestInit_NormalPaths_StillWork(t *testing.T) {
	repo := t.TempDir()
	deps := initrepo.Deps{
		RepoRoot:     repo,
		GlobalPath:   filepath.Join(t.TempDir(), "release.yaml"),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	result, err := initrepo.Init(deps, initrepo.Options{})
	if err != nil {
		t.Fatalf("Init() error = %v, want normal paths to keep working", err)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("release.yaml was not written: %v", err)
	}
}
