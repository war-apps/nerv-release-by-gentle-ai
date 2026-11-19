package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/artifact"
)

// releaseFiles maps the artifact's release files to their stored content.
func releaseFiles(t *testing.T, work gitDir, project, version string) map[string]string {
	t.Helper()
	m, dir := readArtifact(t, work, project, version)
	files := map[string]string{}
	for _, f := range m.ReleaseFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Stored)))
		if err != nil {
			t.Fatal(err)
		}
		files[f.Path] = string(data)
	}
	return files
}

const (
	packageJSON = "{\r\n  \"name\": \"widgets\",\r\n  \"version\": \"1.0.0\",\r\n  \"dependencies\": { \"left-pad\": \"1.0.0\" }\r\n}\r\n"
	packageLock = "{\n  \"name\": \"widgets\",\n  \"version\": \"1.0.0\",\n  \"lockfileVersion\": 3,\n  \"packages\": {\n    \"\": { \"name\": \"widgets\", \"version\": \"1.0.0\" },\n    \"node_modules/left-pad\": { \"version\": \"1.0.0\" }\n  }\n}\n"
)

// R-S7: version_files of any format are written in the release clone and
// captured into the artifact with the new version, every other byte kept;
// a {command, files} entry runs in the clone and its files are captured.
func TestIntegration_ReleaseWritesGenericVersionFiles(t *testing.T) {
	requireReleaseToolchain(t)
	releaseEnv(t)
	_, work := newReleaseRepos(t)
	work.write(".ai-workflow/release.yaml", `release:
  projects:
    - name: widgets
      path: .
      build: bash
      stages:
        build: { run: 'true' }
      version_files:
        - package.json
        - package-lock.json
        - VERSION
        - { command: 'echo "{version}" > VERSION.txt', files: [VERSION.txt] }
`)
	work.write("package.json", packageJSON)
	work.write("package-lock.json", packageLock)
	work.write("VERSION", "1.0.0\n")
	work.commit("chore: init", "a.txt")
	work.git("tag", "v1.0.0")
	work.commit("feat: add gadgets", "b.txt")
	work.git("push", "-q", "origin", "main", "--tags")

	code, stdout, stderr := runReleaseCommand(t, "--cwd", work.dir, "--yes")
	if code != 0 {
		t.Fatalf("release: exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	files := releaseFiles(t, work, "widgets", "1.1.0")
	want := map[string]string{
		"package.json":      strings.Replace(packageJSON, `"version": "1.0.0"`, `"version": "1.1.0"`, 1),
		"package-lock.json": strings.Replace(packageLock, `"version": "1.0.0"`, `"version": "1.1.0"`, 2),
		"VERSION":           "1.1.0\n",
		"VERSION.txt":       "1.1.0\n",
	}
	for path, content := range want {
		if files[path] != content {
			t.Errorf("%s =\n%q\nwant\n%q", path, files[path], content)
		}
	}
	if _, ok := files["CHANGELOG.md"]; !ok || len(files) != len(want)+1 {
		t.Errorf("release files %v, want the version files and CHANGELOG.md", keys(files))
	}
	if status := work.git("status", "--porcelain"); status != "" {
		t.Errorf("the user's checkout changed: %q", status)
	}
}

// A failing version_files command fails the build, naming the project and
// the entry; nothing is packaged.
func TestIntegration_ReleaseFailsOnAFailingVersionCommand(t *testing.T) {
	requireReleaseToolchain(t)
	releaseEnv(t)
	_, work := newReleaseRepos(t)
	work.write(".ai-workflow/release.yaml", `release:
  projects:
    - name: widgets
      path: .
      build: bash
      stages:
        build: { run: 'true' }
      version_files:
        - { command: 'exit 3', files: [VERSION.txt] }
`)
	work.commit("chore: init", "a.txt")
	work.git("tag", "v1.0.0")
	work.commit("feat: add gadgets", "b.txt")
	work.git("push", "-q", "origin", "main", "--tags")

	code, stdout, stderr := runReleaseCommand(t, "--cwd", work.dir, "--yes")
	if code == 0 || !strings.Contains(stderr, `project "widgets", step "version_files[0]"`) || !strings.Contains(stderr, "exit code 3") {
		t.Fatalf("release: exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if _, err := os.Stat(artifact.Dir(work.dir, "widgets", "1.1.0")); !os.IsNotExist(err) {
		t.Errorf("an artifact was written: %v", err)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
