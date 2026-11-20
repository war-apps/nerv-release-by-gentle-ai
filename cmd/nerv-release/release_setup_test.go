package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
)

// setupRepo is a git repository holding files (name -> content).
func setupRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func runSetup(t *testing.T, repo string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	opts := testOptions(t.TempDir())
	opts.Runner = runner.ExecRunner{}
	code := run(append([]string{"setup", "--cwd", repo}, args...), &stdout, &stderr, opts)
	return code, stdout.String(), stderr.String()
}

const dimPackageJSON = `{
  "name": "acme.api",
  "release": {
    "branches": ["main", {"name": "develop", "prerelease": "alpha", "channel": "alpha"}, {"name": "hotfix/*", "prerelease": true}],
    "plugins": [
      ["@semantic-release/commit-analyzer", {"preset": "conventionalcommits", "releaseRules": [{"type": "docs", "release": "patch"}]}],
      "@semantic-release/release-notes-generator",
      ["@semantic-release/exec", {"prepareCmd": "./bump-csproj.sh"}],
      "@semantic-release/git"
    ]
  }
}
`

// R-S2/R-S4/R-S7: setup --import has the version engine translate the
// semantic-release configuration into its keys,
// release.providers.conventional-commits; it keeps the rest of the file,
// and prints what it could not translate.
func TestReleaseSetup_ImportWritesTheReleaseSectionAndPrintsTheWarnings(t *testing.T) {
	repo := setupRepo(t, map[string]string{
		"package.json":              dimPackageJSON,
		".ai-workflow/release.yaml": "# team settings\nenabled: true\nrelease:\n  projects:\n    - name: api\n      path: .\n      build: bash\ndeploy:\n  host: github\n",
	})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".ai-workflow/release.yaml"))
	text := string(data)
	for _, want := range []string{"# team settings\nenabled: true\n", "deploy:\n  host: github\n", "- name: api"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rest of the file was not kept (%q):\n%s", want, text)
		}
	}
	rel, err := releaseconfig.Load(repo)
	if err != nil {
		t.Fatalf("the written section does not load: %v\n%s", err, text)
	}
	engine := rel.Providers["conventional-commits"]
	branches, _ := engine["branches"].([]any)
	rules, _ := engine["release_rules"].([]any)
	if len(branches) != 3 || branches[1].(map[string]any)["channel"] != "alpha" || len(rules) != 1 || rules[0].(map[string]any)["type"] != "docs" || rel.Projects[0].Name != "api" {
		t.Errorf("imported section = %+v", rel)
	}
	// Neither npm nor git assets: no version file is imported.
	if strings.Contains(text, "version_files") {
		t.Errorf("version_files written without npm or git assets:\n%s", text)
	}
	if !strings.Contains(text, "release:\n  providers:\n    conventional-commits:\n      branches:\n") {
		t.Errorf("the keys are not under release.providers.conventional-commits:\n%s", text)
	}
	for _, want := range []string{"@semantic-release/exec", "package.json"} {
		if !strings.Contains(stderr+stdout, want) {
			t.Errorf("output lacks %q:\nstdout %s\nstderr %s", want, stdout, stderr)
		}
	}
}

// --dry-run prints the new section and writes nothing.
func TestReleaseSetup_DryRunWritesNothing(t *testing.T) {
	repo := setupRepo(t, map[string]string{"package.json": dimPackageJSON})
	code, stdout, stderr := runSetup(t, repo, "--import", "--dry-run")
	if code != 0 || !strings.Contains(stdout, "release:\n") || !strings.Contains(stdout, "name: develop") {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, ".ai-workflow/release.yaml")); !os.IsNotExist(err) {
		t.Errorf("a dry run wrote the file: %v", err)
	}
}

// R-S2: a JavaScript configuration is reported, never imported; no
// configuration is a refusal too.
func TestReleaseSetup_RefusesAJavaScriptConfigAndNoConfig(t *testing.T) {
	repo := setupRepo(t, map[string]string{"release.config.js": "module.exports = {}\n"})
	code, _, stderr := runSetup(t, repo, "--import")
	if code != 1 || !strings.Contains(stderr, "release.config.js") || !strings.Contains(stderr, "by hand") {
		t.Errorf("js config: exit %d, stderr %s", code, stderr)
	}
	code, _, stderr = runSetup(t, setupRepo(t, nil), "--import")
	if code != 1 || !strings.Contains(stderr, "no semantic-release configuration") {
		t.Errorf("no config: exit %d, stderr %s", code, stderr)
	}
}

// R-S2: a 4.2.0 release: section is not converted by the import: it is
// refused (releaseconfig.parse's own legacy-key check, already ported in
// U2), and the file is left as it was.
func TestReleaseSetup_RefusesALegacyReleaseSection(t *testing.T) {
	legacy := "release:\n  semantic_release: { version: 24.2.3 }\n  projects:\n    - name: api\n      path: .\n      presets: [dotnet]\n"
	repo := setupRepo(t, map[string]string{"package.json": dimPackageJSON, ".ai-workflow/release.yaml": legacy})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 1 || !strings.Contains(stderr, "4.2.0 release: section") {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, ".ai-workflow/release.yaml")); got != legacy {
		t.Errorf(".ai-workflow/release.yaml changed:\n%s", got)
	}
}

// --import is required (the only mode for now).
func TestReleaseSetup_UsageErrors(t *testing.T) {
	if code, _, stderr := runSetup(t, setupRepo(t, nil)); code != 2 || !strings.Contains(stderr, "--import") {
		t.Errorf("no --import: exit %d, stderr %s", code, stderr)
	}
}

const conflictingReleaserc = `{"branches":["main",{"name":"develop","prerelease":"alpha","channel":"alpha"}],"tagFormat":"v${version}","plugins":[["@semantic-release/commit-analyzer",{"preset":"conventionalcommits"}],["@semantic-release/release-notes-generator",{"preset":"conventionalcommits"}]]}`

const handTunedRelease = "release:\n  providers:\n    conventional-commits:\n      # hand-tuned\n      tag_format: custom-${version}\n      branches:\n        - main\n        - {name: next, prerelease: beta, channel: next}\n  projects:\n    - name: api\n      path: .\n      build: bash\n"

// R-S4 B1: without --force the import refuses (exit 1), writes nothing
// and lists every conflicting key with its current and imported values.
func TestReleaseSetup_ConflictingEngineKeysRefuseAndListBothValues(t *testing.T) {
	repo := setupRepo(t, map[string]string{".releaserc.json": conflictingReleaserc, ".ai-workflow/release.yaml": handTunedRelease})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".ai-workflow/release.yaml"))
	if string(data) != handTunedRelease {
		t.Errorf("a refused import wrote the file:\n%s", data)
	}
	for _, want := range []string{"tag_format", "custom-${version}", "v${version}", "branches", "name: next", "name: develop", "--force"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

// R-S4 B1: --force replaces them, prints each replaced value, and keeps
// the comments; an equal value is no conflict.
func TestReleaseSetup_ForceReplacesAndPrintsTheReplacedValues(t *testing.T) {
	repo := setupRepo(t, map[string]string{".releaserc.json": conflictingReleaserc, ".ai-workflow/release.yaml": handTunedRelease})
	code, stdout, stderr := runSetup(t, repo, "--import", "--force")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".ai-workflow/release.yaml"))
	text := string(data)
	if !strings.Contains(text, "# hand-tuned") || !strings.Contains(text, "tag_format: v${version}") || strings.Contains(text, "name: next") {
		t.Errorf("forced import result:\n%s", text)
	}
	for _, want := range []string{"replaced", "tag_format", "custom-${version}", "name: next"} {
		if !strings.Contains(stdout+stderr, want) {
			t.Errorf("output lacks %q:\nstdout %s\nstderr %s", want, stdout, stderr)
		}
	}
	// A second import finds the same values: no conflict, no --force.
	if code, stdout, stderr := runSetup(t, repo, "--import"); code != 0 {
		t.Errorf("re-import of equal values: exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
}

// R-S2: with no semantic-release configuration there is nothing to
// import, whatever the release: section holds, and nothing is written.
func TestReleaseSetup_ReleaseSectionWithoutSemanticReleaseConfigIsRefused(t *testing.T) {
	section := "enabled: true\nrelease:\n  projects:\n    - name: api\n      path: .\n      build: bash\n"
	repo := setupRepo(t, map[string]string{".ai-workflow/release.yaml": section})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 1 || !strings.Contains(stderr, "nothing to import") {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, ".ai-workflow/release.yaml")); got != section {
		t.Errorf(".ai-workflow/release.yaml changed:\n%s", got)
	}
}

// Errors name the section that is really in the file.
func TestReleaseSetup_ErrorsNameTheReleaseSection(t *testing.T) {
	repo := setupRepo(t, map[string]string{
		"package.json":              dimPackageJSON,
		".ai-workflow/release.yaml": "release: [not, a, mapping]\n",
	})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 1 || !strings.Contains(stderr, "release: must be a mapping") {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
}

// R-S2, U2's L8 advisory 1: setup --import refuses an alpha.1 ship:
// section, whether or not there is a configuration to import; the file
// is left as it was.
func TestReleaseSetup_RefusesTheAlphaShipSection(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"with a semantic-release config": {"package.json": dimPackageJSON, ".ai-workflow/release.yaml": alphaShipYAML},
		"without one":                    {".ai-workflow/release.yaml": alphaShipYAML},
	} {
		repo := setupRepo(t, files)
		code, stdout, stderr := runSetup(t, repo, "--import")
		if code != 1 || stdout != "" {
			t.Errorf("%s: exit %d\nstdout %s\nstderr %s", name, code, stdout, stderr)
		}
		for _, want := range alphaShipWants {
			if !strings.Contains(stderr, want) {
				t.Errorf("%s: stderr lacks %q: %s", name, want, stderr)
			}
		}
		if got := mustReadFile(t, filepath.Join(repo, ".ai-workflow/release.yaml")); got != alphaShipYAML {
			t.Errorf("%s: .ai-workflow/release.yaml changed:\n%s", name, got)
		}
	}
}

// npmReleaserc uses @semantic-release/npm, whose package.json (and the
// repository's package-lock.json) are version files.
const npmReleaserc = `{"branches": ["main"], "plugins": [["@semantic-release/commit-analyzer", {"preset": "conventionalcommits"}], "@semantic-release/npm"]}`

// R-S7: setup --import adds the imported version files to the one
// project's version_files, after its entries, comments kept; the result
// loads.
func TestReleaseSetup_ImportAddsVersionFilesToTheOnlyProject(t *testing.T) {
	repo := setupRepo(t, map[string]string{
		".releaserc.json":           npmReleaserc,
		"package.json":              `{"name": "x", "version": "1.0.0"}`,
		"package-lock.json":         `{"version": "1.0.0", "packages": {"": {"version": "1.0.0"}}}`,
		".ai-workflow/release.yaml": "release:\n  projects:\n    - name: api\n      path: .\n      build: bash\n      version_files:\n        - VERSION # plain text\n",
	})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	text := mustReadFile(t, filepath.Join(repo, ".ai-workflow/release.yaml"))
	if !strings.Contains(text, "# plain text") {
		t.Errorf("the comment was not kept:\n%s", text)
	}
	rel, err := releaseconfig.Load(repo)
	if err != nil {
		t.Fatalf("the written section does not load: %v\n%s", err, text)
	}
	var paths []string
	for _, f := range rel.Projects[0].VersionFiles {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "VERSION,package.json,package-lock.json" {
		t.Errorf("version_files = %v\n%s", paths, text)
	}
	if !strings.Contains(stderr, "added package.json, package-lock.json to release.projects[0].version_files") {
		t.Errorf("stderr does not say what was added:\n%s", stderr)
	}
}

// R-S7: with several projects none is chosen: the version files are
// named in a warning, and no version_files is written.
func TestReleaseSetup_ImportWithSeveralProjectsWarnsAboutVersionFiles(t *testing.T) {
	repo := setupRepo(t, map[string]string{
		".releaserc.json":           npmReleaserc,
		"package.json":              `{"name": "x", "version": "1.0.0"}`,
		".ai-workflow/release.yaml": "release:\n  projects:\n    - name: api\n      path: api\n      build: bash\n    - name: web\n      path: web\n      build: bash\n",
	})
	code, stdout, stderr := runSetup(t, repo, "--import")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if text := mustReadFile(t, filepath.Join(repo, ".ai-workflow/release.yaml")); strings.Contains(text, "version_files") {
		t.Errorf("version_files written with several projects:\n%s", text)
	}
	if !strings.Contains(stderr, "warning: version files package.json were not added: release.projects has 2 projects") {
		t.Errorf("no warning names the version files:\n%s", stderr)
	}
}
