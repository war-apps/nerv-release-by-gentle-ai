package initrepo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/initrepo"
)

func newDeps(t *testing.T) initrepo.Deps {
	t.Helper()
	repo := t.TempDir()
	return initrepo.Deps{
		RepoRoot:     repo,
		GlobalPath:   filepath.Join(t.TempDir(), "release.yaml"),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInit_None_WritesNothing(t *testing.T) {
	deps := newDeps(t)

	result, err := initrepo.Init(deps, initrepo.Options{None: true})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !result.Skipped {
		t.Error("Skipped = false, want true for --none")
	}
	if _, err := os.Stat(filepath.Join(deps.RepoRoot, ".ai-workflow", "release.yaml")); err == nil {
		t.Error("release.yaml was written despite --none")
	}
}

func TestInit_NoSeedNoImport_WritesAnEmptyFile(t *testing.T) {
	deps := newDeps(t)

	result, err := initrepo.Init(deps, initrepo.Options{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.SeededFromDefaults {
		t.Error("SeededFromDefaults = true, want false with no global defaults:")
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("release.yaml was not written: %v", err)
	}
}

// TestInit_SeedsFromGlobalDefaults_WithoutPersonalKeys is R-S4/U6: init
// seeds the repository's release.yaml from the global file's defaults:
// block, and the seed itself never carries a personal (scope: user) key
// (workflowconfig.Seed already strips them).
func TestInit_SeedsFromGlobalDefaults_WithoutPersonalKeys(t *testing.T) {
	deps := newDeps(t)
	writeFile(t, deps.GlobalPath, "defaults:\n"+
		"  deploy:\n"+
		"    providers:\n"+
		"      ssh-compose:\n"+
		"        environment: prod\n"+
		"        ssh_port: 2222\n"+
		"        ssh_key_file: /home/user/.ssh/id_rsa\n")

	result, err := initrepo.Init(deps, initrepo.Options{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !result.SeededFromDefaults {
		t.Error("SeededFromDefaults = false, want true")
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("reading %s: %v", result.Path, err)
	}
	text := string(data)
	if !strings.Contains(text, "environment: prod") {
		t.Errorf("release.yaml = %q, want the seeded repo-scope key", text)
	}
	if !strings.Contains(text, "ssh_port: 2222") {
		t.Errorf("release.yaml = %q, want the seeded repo-scope key", text)
	}
	if strings.Contains(text, "ssh_key_file") {
		t.Errorf("release.yaml = %q, must never carry a personal (scope: user) key", text)
	}
}

// TestInit_Import_CopiesRepoScopeKeysOnly_SkipsPersonalOnes is the
// security rule shared with nerv-task init: a repository's own
// .ai-workflow/config.yaml is untrusted input (it travels with a
// clone), so only its scope: repo keys are imported, and a scope: user
// key is never written anywhere -- only reported, by name, as skipped.
func TestInit_Import_CopiesRepoScopeKeysOnly_SkipsPersonalOnes(t *testing.T) {
	deps := newDeps(t)
	nervConfigPath := filepath.Join(deps.RepoRoot, ".ai-workflow", "config.yaml")
	writeFile(t, nervConfigPath, "release:\n"+
		"  projects:\n"+
		"    - name: api\n"+
		"deploy:\n"+
		"  providers:\n"+
		"    ssh-compose:\n"+
		"      environment: staging\n"+
		"      ssh_auth: key\n"+
		"      ssh_known_hosts: /tmp/attacker-known-hosts\n")

	result, err := initrepo.Init(deps, initrepo.Options{Import: true})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.ImportedFrom != nervConfigPath {
		t.Errorf("ImportedFrom = %q, want %q", result.ImportedFrom, nervConfigPath)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("reading %s: %v", result.Path, err)
	}
	text := string(data)
	if !strings.Contains(text, "name: api") {
		t.Errorf("release.yaml = %q, want the imported release: block", text)
	}
	if !strings.Contains(text, "environment: staging") {
		t.Errorf("release.yaml = %q, want the imported repo-scope deploy key", text)
	}
	if strings.Contains(text, "ssh_auth") || strings.Contains(text, "attacker-known-hosts") {
		t.Errorf("release.yaml = %q, must never carry a personal (scope: user) key", text)
	}

	if len(result.SkippedPersonalKeys) != 2 {
		t.Errorf("SkippedPersonalKeys = %v, want 2 entries (ssh_auth, ssh_known_hosts)", result.SkippedPersonalKeys)
	}
	joined := strings.Join(result.SkippedPersonalKeys, "\n")
	for _, key := range []string{"ssh_auth", "ssh_known_hosts"} {
		if !strings.Contains(joined, key) {
			t.Errorf("SkippedPersonalKeys = %v, want it to name %s", result.SkippedPersonalKeys, key)
		}
	}

	// The security rule in full: the personal values were never written
	// anywhere, including the global file.
	if _, err := os.Stat(deps.GlobalPath); err == nil {
		global, _ := os.ReadFile(deps.GlobalPath)
		if strings.Contains(string(global), "attacker-known-hosts") {
			t.Error("a personal key from the repository's config.yaml leaked into the global release.yaml")
		}
	}
}

func TestInit_Import_NoNervConfig_IsNotAnError(t *testing.T) {
	deps := newDeps(t)

	result, err := initrepo.Init(deps, initrepo.Options{Import: true})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.ImportedFrom != "" {
		t.Errorf("ImportedFrom = %q, want empty with no .ai-workflow/config.yaml", result.ImportedFrom)
	}
}

func TestInit_WritesUnderDotAiWorkflow(t *testing.T) {
	deps := newDeps(t)

	result, err := initrepo.Init(deps, initrepo.Options{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	want := filepath.Join(deps.RepoRoot, ".ai-workflow", "release.yaml")
	if result.Path != want {
		t.Errorf("Path = %q, want %q", result.Path, want)
	}
}
