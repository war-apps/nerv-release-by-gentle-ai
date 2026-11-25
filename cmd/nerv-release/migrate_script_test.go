package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
)

// S3 (6.6 TestScriptTarget_ProjectTargetsAreRenamed): the deploy script
// target is script in the release extension: migrate rewrites provider:
// bash in the repository's deploy targets, block and flow style, comments
// kept, and leaves the bash build provider alone.
func TestMigrate_ScriptTargetsAreRenamed(t *testing.T) {
	project := "release:\n  build: bash\n  projects:\n    - { name: api, path: ., stages: { build: { run: make } } }\ndeploy:\n  host: github\n  targets:\n    - name: lab\n      provider: bash # our script\n      config: { script: deploy/lab.sh }\n    - { name: prod, provider: \"bash\", config: { script: deploy/prod.sh } }\n    - name: box\n      provider: ssh-compose\n      config: { environment: box }\n"
	w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: project})
	stdout := w.mustMigrate()
	want := strings.NewReplacer("provider: bash # our", "provider: script # our", `provider: "bash"`, `provider: "script"`).Replace(project)
	if got := w.read(releaseYAMLRel); got != want {
		t.Errorf("release.yaml =\n%s\nwant\n%s", got, want)
	}
	containsAll(t, "stdout", stdout, ".ai-workflow/release.yaml: deploy target provider bash renamed script (the deploy script target of the release extension, release-script)")
	dep, err := deployconfig.Load(w.root)
	if err != nil {
		t.Fatalf("the migrated deploy: section does not load: %v", err)
	}
	if dep.Targets[0].Provider != "script" || dep.Targets[1].Provider != "script" || dep.Targets[2].Provider != "ssh-compose" {
		t.Errorf("targets %+v", dep.Targets)
	}
	w.assertNoop()
}

// S3 (6.6 TestScriptTarget_UserDefaultsAreRenamed and
// TestScriptTarget_UserDeployBlockIsRenamedBeforeItMoves): with --user,
// the global release.yaml's deploy: targets and defaults.deploy seed are
// renamed too; without --user it is left alone.
func TestMigrate_GlobalScriptTargetsAreRenamedWithUser(t *testing.T) {
	w := newMigrateWorld(t, "", map[string]string{"README.md": "x\n"})
	global := "# my settings\ndeploy:\n  targets:\n    - { name: s, provider: bash }\n  providers:\n    ssh-compose:\n      ssh_auth: agent\ndefaults:\n  release:\n    build: bash\n  deploy:\n    targets:\n      - name: lab\n        provider: bash\n"
	w.write(w.globalPath(), global)
	w.assertNoop()
	if got := w.read(w.globalPath()); got != global {
		t.Fatalf("the global release.yaml changed without --user:\n%s", got)
	}

	stdout := w.mustMigrate("--user")
	want := "# my settings\ndeploy:\n  targets:\n    - { name: s, provider: script }\n  providers:\n    ssh-compose:\n      ssh_auth: agent\ndefaults:\n  release:\n    build: bash\n  deploy:\n    targets:\n      - name: lab\n        provider: script\n"
	if got := w.read(w.globalPath()); got != want {
		t.Errorf("global release.yaml =\n%s\nwant\n%s", got, want)
	}
	containsAll(t, "stdout", stdout,
		"Update "+w.globalPath()+": nerv-release's global configuration, converted",
		"the global release.yaml: deploy: deploy target provider bash renamed script",
		"the global release.yaml: defaults.deploy: deploy target provider bash renamed script")
	if bak := backups(t, w.globalPath()); len(bak) != 1 || mustReadFile(t, bak[0]) != global {
		t.Errorf("global backups = %v, want one holding the old file", bak)
	}
	w.assertNoop("--user")
}

// S5: --user outside a repository converts the global release.yaml only.
func TestMigrate_UserOutsideARepository(t *testing.T) {
	w := newMigrateWorld(t, "", nil)
	w.write(w.globalPath(), "deploy:\n  targets:\n    - { name: s, provider: bash }\n")
	outside := t.TempDir()
	w.root = outside
	stdout := w.mustMigrate("--user")
	if got := w.read(w.globalPath()); got != "deploy:\n  targets:\n    - { name: s, provider: script }\n" {
		t.Errorf("global release.yaml =\n%s", got)
	}
	containsAll(t, "stdout", stdout, "deploy target provider bash renamed script")
}

// S3 (6.6 TestProjectDeployProviders_MoveToTheReleaseExtension): a project
// deploy provider, .ai-workflow/providers/deploy-<name>, moves to
// release-<name> with every file, its extension: deploy becomes release
// (the old manifest backed up), other providers stay, and a dry run
// writes nothing.
func TestMigrate_ProjectDeployProvidersMove(t *testing.T) {
	manifest := "name: rsync\nextension: deploy # moved\nslot: target\ncontract_version: 1\nruntime: exec\noperations:\n  deploy:\n    - argv: [bash, \"{provider_dir}/deploy.sh\"]\n"
	w := newMigrateWorld(t, "", map[string]string{
		".ai-workflow/providers/deploy-rsync/provider.yaml": manifest,
		".ai-workflow/providers/deploy-rsync/deploy.sh":     "#!/bin/bash\necho deployed\n",
		".ai-workflow/providers/task-board/provider.yaml":   "name: board\n",
	})
	before := w.hash()
	code, stdout, stderr := w.migrate("--dry-run")
	if code != 0 || w.hash() != before {
		t.Fatalf("dry run: exit %d, changed %v\n%s\n%s", code, w.hash() != before, stdout, stderr)
	}
	containsAll(t, "dry run", stdout,
		"Move .ai-workflow/providers/deploy-rsync to .ai-workflow/providers/release-rsync",
		"Update .ai-workflow/providers/release-rsync/provider.yaml: the deploy provider, now of the release extension",
		".ai-workflow/providers/deploy-rsync moved to release-rsync: the deploy extension merged into release")

	w.mustMigrate()
	if got := w.read(".ai-workflow/providers/release-rsync/provider.yaml"); got != strings.Replace(manifest, "extension: deploy", "extension: release", 1) {
		t.Errorf("release-rsync/provider.yaml =\n%s", got)
	}
	if got := w.read(".ai-workflow/providers/release-rsync/deploy.sh"); got != "#!/bin/bash\necho deployed\n" {
		t.Errorf("release-rsync/deploy.sh = %q", got)
	}
	if w.exists(".ai-workflow/providers/deploy-rsync") {
		t.Error(".ai-workflow/providers/deploy-rsync is still there")
	}
	if !w.exists(".ai-workflow/providers/task-board/provider.yaml") {
		t.Error("a task provider moved")
	}
	if bak := w.repoBackups(".ai-workflow/providers/release-rsync/provider.yaml"); len(bak) != 1 || mustReadFile(t, bak[0]) != manifest {
		t.Errorf("provider.yaml backups = %v, want one holding the old manifest", bak)
	}
	w.assertNoop()
}

// S3 (6.6 TestProjectDeployProviders_ConflictsAreRefused): a destination
// that exists, or a provider named bash, is refused before anything is
// written.
func TestMigrate_ProjectDeployProviderConflictsAreRefused(t *testing.T) {
	for name, files := range map[string][]string{
		"release-rsync already exists": {".ai-workflow/providers/deploy-rsync/provider.yaml", ".ai-workflow/providers/release-rsync/provider.yaml"},
		"would be release-bash":        {".ai-workflow/providers/deploy-bash/provider.yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			m := map[string]string{releaseYAMLRel: "pipeline:\n  deploy:\n    environments: [staging]\n"}
			for _, f := range files {
				m[f] = "name: x\nextension: deploy\n"
			}
			w := newMigrateWorld(t, "", m)
			before := w.hash()
			code, stdout, stderr := w.migrate()
			if code != 1 || !strings.Contains(stderr, name) {
				t.Errorf("exit %d, want 1 naming %q\nstdout %s\nstderr %s", code, name, stdout, stderr)
			}
			if w.hash() != before {
				t.Error("a refused migration changed the files")
			}
		})
	}
}

// S3: a converted release.yaml that today's loaders reject is refused,
// naming the problem, and nothing is written.
func TestMigrate_RefusesAResultThatDoesNotLoad(t *testing.T) {
	w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: "release:\n  branches: [main]\n  projects:\n    - { name: api, path: ., build: bash, bogus: 1 }\n"})
	before := w.hash()
	code, stdout, stderr := w.migrate()
	if code != 1 || !strings.Contains(stderr, "the migrated .ai-workflow/release.yaml does not load") || !strings.Contains(stderr, "bogus") {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	if w.hash() != before {
		t.Error("a refused migration changed files")
	}
}

// S3: a symlinked release.yaml, .ai-workflow or .nerv folder (where the
// backups go) is refused: a cloned repository must never redirect what
// migrate writes.
func TestMigrate_RefusesSymlinks(t *testing.T) {
	for _, link := range []string{releaseYAMLRel, ".ai-workflow", ".nerv"} {
		t.Run(link, func(t *testing.T) {
			w := newMigrateWorld(t, "", map[string]string{"README.md": "x\n"})
			target := filepath.Join(t.TempDir(), "elsewhere")
			if link == ".nerv" {
				w.write(releaseYAMLRel, "deploy:\n  targets:\n    - { name: s, provider: bash }\n")
				if err := os.MkdirAll(target, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if link == releaseYAMLRel {
				w.write(target, "ship:\n  projects: []\n")
				if err := os.MkdirAll(w.path(".ai-workflow"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, w.path(link)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			code, stdout, stderr := w.migrate()
			if code != 1 || !strings.Contains(stderr, "refusing") {
				t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
		})
	}
}
