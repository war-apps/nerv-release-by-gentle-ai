package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"gopkg.in/yaml.v3"
)

const (
	migrateGitHubOrigin = "https://github.com/dirm-s-a/repo.git"
	releaseYAMLRel      = ".ai-workflow/release.yaml"
)

// migrateWorld is a git repository and a home for `nerv-release migrate`.
type migrateWorld struct {
	t          *testing.T
	root, home string
}

// newMigrateWorld is a repository holding files (name -> content), with
// origin when origin is not empty, and an empty home whose user config
// directory is isolated.
func newMigrateWorld(t *testing.T, origin string, files map[string]string) migrateWorld {
	t.Helper()
	w := migrateWorld{t: t, root: setupRepo(t, files), home: t.TempDir()}
	// On Windows a file just written can stay pending delete for a moment
	// (a scanner holds it), failing t.TempDir's own cleanup: empty the
	// repository and the home first, retrying (cleanups run
	// last-registered first).
	t.Cleanup(func() {
		for _, dir := range []string{w.root, w.home} {
			for i := 0; i < 20 && os.RemoveAll(dir) != nil; i++ {
				time.Sleep(50 * time.Millisecond)
			}
		}
	})
	t.Setenv("AppData", filepath.Join(w.home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(w.home, ".config"))
	if origin != "" {
		if out, err := exec.Command("git", "-C", w.root, "remote", "add", "origin", origin).CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v %s", err, out)
		}
	}
	return w
}

// fixtureWorld copies testdata/migrate/<fixture> into a new repository,
// every file at its own path.
func fixtureWorld(t *testing.T, fixture, origin string) migrateWorld {
	t.Helper()
	src := filepath.Join("testdata", "migrate", fixture)
	files := map[string]string{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return newMigrateWorld(t, origin, files)
}

// globalPath is the global release.yaml of the world's home.
func (w migrateWorld) globalPath() string {
	return aiworkflow.UserConfigPath(w.home, runtime.GOOS, "release")
}

func (w migrateWorld) path(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(w.root, filepath.FromSlash(rel))
}

func (w migrateWorld) write(rel, content string) {
	w.t.Helper()
	path := w.path(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w migrateWorld) read(rel string) string {
	w.t.Helper()
	return mustReadFile(w.t, w.path(rel))
}

func (w migrateWorld) exists(rel string) bool {
	_, err := os.Lstat(w.path(rel))
	return err == nil
}

// migrate runs `nerv-release migrate --cwd <root> args...`.
func (w migrateWorld) migrate(args ...string) (int, string, string) {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	opts := testOptions(w.home)
	opts.Runner = runner.ExecRunner{}
	opts.Home = w.home
	code := run(append([]string{"migrate", "--cwd", w.root}, args...), &stdout, &stderr, opts)
	return code, stdout.String(), stderr.String()
}

// mustMigrate runs migrate and fails unless it exits 0.
func (w migrateWorld) mustMigrate(args ...string) string {
	w.t.Helper()
	code, stdout, stderr := w.migrate(args...)
	if code != 0 {
		w.t.Fatalf("migrate %v: exit %d\nstdout %s\nstderr %s", args, code, stdout, stderr)
	}
	return stdout
}

// hash hashes every file under the repository and the home (path and
// content), .git excluded.
func (w migrateWorld) hash() string {
	w.t.Helper()
	h := sha256.New()
	for _, dir := range []string{w.root, w.home} {
		var files []string
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if err == nil && !d.IsDir() {
				files = append(files, path)
			}
			return nil
		})
		sort.Strings(files)
		for _, f := range files {
			data, _ := os.ReadFile(f)
			h.Write([]byte(f + "\x00"))
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// backups lists the bak-migrate- backups of path.
func backups(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".bak-migrate-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// repoBackups lists the backups of the repository file rel: they live
// under .nerv/backups/release-migrate-<timestamp>/<rel>, never next to the file
// in a committed folder; it fails when one sits next to the file.
func (w migrateWorld) repoBackups(rel string) []string {
	w.t.Helper()
	if next := backups(w.t, w.path(rel)); len(next) != 0 {
		w.t.Errorf("backups next to %s, in a committed folder: %v", rel, next)
	}
	matches, err := filepath.Glob(filepath.Join(w.root, ".nerv", "backups", "release-migrate-*", filepath.FromSlash(rel)))
	if err != nil {
		w.t.Fatal(err)
	}
	return matches
}

// yamlMap decodes text into a generic map.
func yamlMap(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%v:\n%s", err, text)
	}
	return m
}

func containsAll(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain %q:\n%s", what, want, text)
		}
	}
}

func containsNone(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(text, u) {
			t.Errorf("%s still contains %q:\n%s", what, u, text)
		}
	}
}

// assertNoop checks a further run changes nothing and says so.
func (w migrateWorld) assertNoop(args ...string) {
	w.t.Helper()
	before := w.hash()
	code, stdout, stderr := w.migrate(args...)
	if code != 0 || !strings.Contains(stdout, "nothing to migrate") {
		w.t.Errorf("second run: exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if w.hash() != before {
		w.t.Error("the second run changed files")
	}
}

// S5: nerv-release migrate -h prints its usage, exit 0; a bad flag is a
// usage error, exit 2.
func TestMigrate_HelpAndUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"migrate", "-h"}, &stdout, &stderr, options{}); code != 0 {
		t.Fatalf("migrate -h: exit %d, stderr %s", code, stderr.String())
	}
	containsAll(t, "migrate -h", stdout.String(), "Usage: nerv-release migrate [--cwd <dir>] [--user] [--dry-run]", "--dry-run", "--user", "Exit codes:")

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"migrate", "--bogus"}, &stdout, &stderr, options{}); code != 2 {
		t.Errorf("migrate --bogus: exit %d, want 2", code)
	}
	containsAll(t, "migrate --bogus stderr", stderr.String(), "Usage: nerv-release migrate")

	stdout.Reset()
	if code := run(nil, &stdout, &stderr, options{}); code != 0 || !strings.Contains(stdout.String(), "migrate [--cwd <dir>] [--user] [--dry-run]") {
		t.Errorf("the usage does not list migrate:\n%s", stdout.String())
	}
}

// S3: a repository in today's shape (or with no release.yaml) has nothing
// to migrate: exit 0, nothing written.
func TestMigrate_NothingToMigrate(t *testing.T) {
	current := "release:\n  projects:\n    - name: api\n      path: .\n      build: bash\n      stages: { build: { run: make } }\ndeploy:\n  host: github\n"
	for name, files := range map[string]map[string]string{
		"no release.yaml":    {"README.md": "x\n"},
		"today's sections":   {releaseYAMLRel: current},
		"no legacy sections": {releaseYAMLRel: "# nothing yet\n"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newMigrateWorld(t, "", files)
			w.assertNoop()
			w.assertNoop("--user")
		})
	}
}

// S5: outside a git repository migrate is refused (exit 1).
func TestMigrate_RefusesOutsideARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	var stdout, stderr bytes.Buffer
	opts := testOptions("")
	opts.Runner = runner.ExecRunner{}
	opts.Home = t.TempDir()
	if code := run([]string{"migrate", "--cwd", t.TempDir()}, &stdout, &stderr, opts); code != 1 {
		t.Errorf("exit %d, want 1\nstderr %s", code, stderr.String())
	}
	containsAll(t, "stderr", stderr.String(), "not inside a git repository")
}

// alpha1Release is the release: section the alpha1 fixture's ship:
// section becomes.
const alpha1Release = `release: # ship plugin; project scope only
  providers:
    conventional-commits:
      # The version engine.
      branches:
        - main
        - {name: develop, prerelease: alpha, channel: alpha}
      release_rules:
        - {type: docs, scope: readme, release: patch}
      note_keywords: [BREAKING CHANGE, BREAKING]
      tag_format: "v${version}"
      repository_url: https://github.com/acme/widgets
      changelog: {file: CHANGELOG.md, title: "# Changelog"}
  projects:
    - name: api
      path: api
      build: docker-compose
      images: ["ghcr.io/acme/api:${version}"]
      providers:
        docker-compose:
          file: api/docker-compose.yml
          test_service: tests
    - name: worker
      path: worker
      tag_format: "worker-v${version}"
      changelog: worker/HISTORY.md
      depends_on:
        - {project: api, bump: patch}
      build: bash
      version_files: [worker/Worker.csproj]
      coverage: {line: 80}
      providers:
        bash:
          shared_host: true
      stages:
        restore: {run: dotnet restore worker}
        build: {run: dotnet build worker -c Release --no-restore}
        test: {run: dotnet test worker --no-build}
        package: {run: 'dotnet publish worker -o "$NERV_RELEASE_OUTPUTS_DIR"'}
      hooks:
        prepare:
          - {run: echo ready}
`

// S3 (6.6 TestAlpha_ShipSectionBecomesRelease): a 5.0.0-alpha.1 ship:
// section in release.yaml becomes today's release: section, each key in
// its new place, the comments kept; the dry run writes nothing, the old
// file is backed up, the result loads, and a second run is a no-op.
func TestMigrate_ShipSectionBecomesRelease(t *testing.T) {
	w := fixtureWorld(t, "alpha1", migrateGitHubOrigin)
	old := w.read(releaseYAMLRel)
	before := w.hash()

	code, stdout, stderr := w.migrate("--dry-run")
	if code != 0 {
		t.Fatalf("dry run: exit %d\n%s\n%s", code, stdout, stderr)
	}
	containsAll(t, "the dry run", stdout,
		"dry run: nothing was written",
		"Update .ai-workflow/release.yaml: converted to today's release: and deploy: sections",
		"ship: renamed release: (the 5.0.0-alpha.1 section)",
		"release.branches: moved to release.providers.conventional-commits.branches",
		"release.projects[0].build.mode: compose became release.projects[0].build: docker-compose",
		"release.projects[0].build.file: moved to release.projects[0].providers.docker-compose.file",
		"release.projects[1].build.shared_host: moved to release.projects[1].providers.bash.shared_host",
		"release.projects[1].build.coverage: moved to release.projects[1].coverage")
	if w.hash() != before {
		t.Fatal("the dry run changed files")
	}

	w.mustMigrate()
	head := "# .ai-workflow/release.yaml -- the release sections nerv 5.0.0-alpha.1 had\n"
	tail := "deploy:                             # deploy plugin\n  host: github\n  targets:\n    - { name: staging, provider: ssh-compose, config: { environment: staging } }\n"
	if got, want := w.read(releaseYAMLRel), head+alpha1Release+tail; got != want {
		t.Errorf("release.yaml =\n%s\nwant\n%s", got, want)
	}
	bak := w.repoBackups(releaseYAMLRel)
	if len(bak) != 1 || mustReadFile(t, bak[0]) != old {
		t.Errorf("backups = %v, want one holding the old file", bak)
	}
	rel, err := releaseconfig.Load(w.root)
	if err != nil {
		t.Fatalf("the migrated section does not load: %v", err)
	}
	api, worker := rel.Projects[0], rel.Projects[1]
	if rel.Providers["conventional-commits"]["tag_format"] != "v${version}" || api.Build != "docker-compose" ||
		api.Providers["docker-compose"]["file"] != "api/docker-compose.yml" || worker.Build != "bash" ||
		worker.Providers["bash"]["shared_host"] != true || worker.Coverage == nil || worker.Coverage.Line != 80 {
		t.Errorf("engine %v, api %+v, worker %+v", rel.Providers, api, worker)
	}
	w.assertNoop()
}

// S3 (6.6 TestAlpha_RefusedShipSections): a file with both ship: and
// release:, and a ship: section with no place for one of its keys, are
// refused (exit 1) and nothing is written.
func TestMigrate_RefusedShipSections(t *testing.T) {
	for _, tt := range []struct{ name, text, want string }{
		{"both sections", "ship:\n  projects: []\nrelease:\n  projects: []\n", "both a ship: section (5.0.0-alpha.1) and a release: section"},
		{"unknown build mode", "ship:\n  projects:\n    - { name: api, path: ., build: { mode: maven } }\n", `release.projects[0].build.mode: unknown build mode "maven" (want compose or script)`},
		{"unknown build key", "ship:\n  projects:\n    - { name: api, path: ., build: { mode: script, sdk: \"8\" } }\n", "release.projects[0].build.sdk: unknown build key"},
		{"engine key set twice", "ship:\n  tag_format: \"a${version}\"\n  providers:\n    conventional-commits: { tag_format: \"b${version}\" }\n", "release.providers.conventional-commits.tag_format is set too"},
		{"inline ship", "ship: { projects: [] }\n", "ship: is written on one line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: tt.text})
			before := w.hash()
			code, stdout, stderr := w.migrate()
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("exit %d, want 1 naming %q\nstdout %s\nstderr %s", code, tt.want, stdout, stderr)
			}
			if w.hash() != before {
				t.Error("a refused migration changed files")
			}
		})
	}
}

// S3 (6.6 TestAlpha_ShipSectionInTheFolderIsConverted,
// TestAlpha_ReleaseSectionWithMovedKeysIsConverted): a small ship: section,
// and a release: section with its engine keys at the top and a build:
// mapping, are moved the same way.
func TestMigrate_MovedKeys(t *testing.T) {
	for _, tt := range []struct{ name, text, want string }{
		{"ship", "ship:\n  tag_format: \"r${version}\"\n  projects:\n    - { name: api, path: ., build: { mode: script } }\n",
			"release:\n  providers:\n    conventional-commits:\n      tag_format: \"r${version}\"\n  projects:\n    - name: api\n      path: .\n      build: bash\n"},
		{"release", "release:\n  branches: [main]\n  projects:\n    - name: api\n      path: .\n      build: { mode: compose, file: c.yml }\n",
			"release:\n  providers:\n    conventional-commits:\n      branches: [main]\n  projects:\n    - name: api\n      path: .\n      build: docker-compose\n      providers:\n        docker-compose:\n          file: c.yml\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: tt.text})
			w.mustMigrate()
			if got := w.read(releaseYAMLRel); got != tt.want {
				t.Errorf("release.yaml =\n%s\nwant\n%s", got, tt.want)
			}
			w.assertNoop()
		})
	}
}

// S3: the repository's backups go under .nerv/backups/release-migrate-<timestamp>
// (the release.yaml one can hold personal keys, and .ai-workflow/ is
// committed): .nerv/ gets a .gitignore of "*" when it has none, so git
// never sees them; the printed backup path is that one.
func TestMigrate_RepositoryBackupsAreIgnoredUnderNerv(t *testing.T) {
	w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
	old := w.read(releaseYAMLRel)
	stdout := w.mustMigrate()
	bak := w.repoBackups(releaseYAMLRel)
	if len(bak) != 1 || mustReadFile(t, bak[0]) != old {
		t.Fatalf("backups = %v, want one holding the old file", bak)
	}
	containsAll(t, "stdout", stdout, "backups: "+bak[0])
	if got := w.read(".nerv/.gitignore"); got != "*\n" {
		t.Errorf(".nerv/.gitignore = %q, want \"*\n\"", got)
	}
	out, err := exec.Command("git", "-C", w.root, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v %s", err, out)
	}
	containsNone(t, "git status", string(out), ".nerv", "bak-migrate")
}

// S3: a .nerv/.gitignore of the repository's own is kept as it is.
func TestMigrate_KeepsAnExistingNervGitignore(t *testing.T) {
	w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: "deploy:\n  targets:\n    - { name: s, provider: bash }\n", ".nerv/.gitignore": "# mine\nbackups/\n"})
	w.mustMigrate()
	if got := w.read(".nerv/.gitignore"); got != "# mine\nbackups/\n" {
		t.Errorf(".nerv/.gitignore = %q, want the repository's own", got)
	}
	if bak := w.repoBackups(releaseYAMLRel); len(bak) != 1 {
		t.Errorf("backups = %v, want one", bak)
	}
}

// S5: each refusal of a legacy section names nerv-release migrate once,
// and none names the old nerv deploy command.
func TestLegacySectionRefusalsNameMigrateOnce(t *testing.T) {
	for _, yamlText := range []string{
		"release:\n  semantic_release: { version: 24.2.3 }\n  projects:\n    - name: api\n      path: .\n",
		"pipeline:\n  test: { run: make test }\n",
		"pipeline:\n  deploy: { selection: branches }\n  docker: {}\n  release: {}\n",
		alphaShipYAML,
	} {
		f := newNextFixture(t, yamlText)
		code, _, stderr := f.run(t)
		if code != 1 || strings.Count(stderr, "nerv-release migrate") != 1 || strings.Contains(stderr, "nerv deploy") {
			t.Errorf("%q: code=%d, want 1 and nerv-release migrate named once, no nerv deploy:\n%s", yamlText, code, stderr)
		}
	}
}
