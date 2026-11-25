package main

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseplan"
)

const (
	comprobantes = "dim.comprobantes.api"
	factura      = "dim-factura-electronica-service"
	legacyFull   = "legacy-full"
)

// S3 (6.6 TestRelease_LegacyReleaseBecomesScriptModeRelease): the 4.2.0
// release: becomes today's release:, the presets become build: bash with
// the commands 4.2.0 ran, the project's own stage commands (and their
// comments) win, and the result loads with today's strict loader.
func TestMigrate_LegacyReleaseBecomesBashBuild(t *testing.T) {
	w := fixtureWorld(t, comprobantes, migrateGitHubOrigin)
	old := w.read(releaseYAMLRel)
	stdout := w.mustMigrate()
	containsAll(t, "stdout", stdout,
		".ai-workflow/release.yaml: the release: section is the 4.2.0 one (release.semantic_release, release.projects[0].presets, release.projects[0].dotnet, release.projects[0].docker)",
		"release.semantic_release dropped: the version engine is native",
		"release.projects[0] (api): presets dotnet, docker became build: bash with the commands they ran",
		"release: converted from the 4.2.0 section")
	rel, err := releaseconfig.Load(w.root)
	if err != nil {
		t.Fatalf("releaseconfig.Load: %v", err)
	}
	if err := rel.CheckBuild(); err != nil {
		t.Fatalf("CheckBuild: %v", err)
	}
	api, _ := rel.Project("api")
	if api.Build != "bash" {
		t.Fatalf("build = %q, want bash", api.Build)
	}
	if len(api.VersionFiles) != 1 || api.VersionFiles[0].Path != "src/Api/Dim.Comprobantes.Api.csproj" {
		t.Errorf("version_files = %q, want the dotnet project_file", api.VersionFiles)
	}
	for _, stage := range []string{"restore", "lint", "build", "test", "coverage"} {
		if !strings.Contains(old, "run: "+api.Stages[stage].Run) {
			t.Errorf("stages.%s = %q, not the project's own 4.2.0 command", stage, api.Stages[stage].Run)
		}
	}
	pkg := api.Stages["package"].Run
	containsAll(t, "stages.package", pkg,
		`docker build -f docker/Dockerfile -t ghcr.io/dirm-s-a/dim.comprobantes.api:"$NERV_RELEASE_VERSION"`,
		`docker build -f docker/LoginBroker.Dockerfile -t ghcr.io/dirm-s-a/dim.comprobantes.api-login-broker:"$NERV_RELEASE_VERSION"`)
	containsNone(t, "stages.package", pkg, "docker save")
	all := []string{"version", "channel", "latest"}
	if want := []releaseconfig.BuildImage{
		{Name: "ghcr.io/dirm-s-a/dim.comprobantes.api", Tags: all},
		{Name: "ghcr.io/dirm-s-a/dim.comprobantes.api-login-broker", Tags: all},
	}; !reflect.DeepEqual(api.Images, want) {
		t.Errorf("images = %+v, want %+v", api.Images, want)
	}
	text := w.read(releaseYAMLRel)
	containsAll(t, "release.yaml", text, "# The release sections nerv moved here", "\nrelease:", `# "Internal CLR error (0x80131506)" when MSBuild/compiler servers are reused.`)
	containsNone(t, "release.yaml", text, "presets:", "semantic_release:")
	if bak := w.repoBackups(releaseYAMLRel); len(bak) != 1 || mustReadFile(t, bak[0]) != old {
		t.Errorf("backups = %v, want one holding the old file", bak)
	}
	w.assertNoop()
}

// S3 (6.6 TestRelease_LegacySectionInTheProjectFileIsConverted): a small
// 4.2.0 section is told apart by its 4.2.0 keys and converted; the dry
// run writes nothing.
func TestMigrate_SmallLegacyReleaseSection(t *testing.T) {
	text := "release:\n  semantic_release: { version: 24.2.3 }\n  projects:\n    - name: api\n      path: .\n      presets: [dotnet]\n      dotnet: { project_file: Api.csproj }\n"
	w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: text})
	before := w.hash()
	code, stdout, _ := w.migrate("--dry-run")
	if code != 0 || !strings.Contains(stdout, "release: converted from the 4.2.0 section") || w.hash() != before {
		t.Fatalf("dry run: exit %d, changed %v\n%s", code, w.hash() != before, stdout)
	}
	stdout = w.mustMigrate()
	got := w.read(releaseYAMLRel)
	containsAll(t, "release.yaml", got, "release:\n", "build: bash", "dotnet build", "TODO(nerv-release migrate): 4.2.0 dropped --disable-build-servers")
	containsNone(t, "release.yaml", got, "presets:", "semantic_release:")
	containsAll(t, "stdout", stdout, "no semantic-release configuration: the version engine's default branches apply")
	if _, err := releaseconfig.Load(w.root); err != nil {
		t.Fatalf("releaseconfig.Load: %v", err)
	}
	w.assertNoop()
}

// S3 (6.6 TestRelease_PresetsWithoutAnEquivalentGetATODO): the presets
// without an equivalent (nuget, npm push) get a TODO comment and a report
// line; the dotnet preset's commands and coverage threshold, node's
// install and scripts, and the hooks move over.
func TestMigrate_PresetsWithoutAnEquivalentGetATODO(t *testing.T) {
	w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
	stdout := w.mustMigrate()
	rel, err := releaseconfig.Load(w.root)
	if err != nil {
		t.Fatalf("releaseconfig.Load: %v", err)
	}
	web, _ := rel.Project("web")
	if got := web.Stages["restore"]; got.Run != "pnpm install --frozen-lockfile" || got.Workdir != "web" {
		t.Errorf("web restore = %+v", got)
	}
	if web.Stages["build"].Run != "pnpm run build" || web.Stages["test"].Run != "pnpm run test" {
		t.Errorf("web build/test = %q / %q", web.Stages["build"].Run, web.Stages["test"].Run)
	}
	if _, ok := web.Stages["lint"]; ok {
		t.Error("web has a lint stage, but package.json has no lint script")
	}
	lib, _ := rel.Project("lib")
	if lib.Stages["lint"].Run != "dotnet format lib/Lib.csproj --verify-no-changes --no-restore" {
		t.Errorf("lib lint = %q, want the project's own", lib.Stages["lint"].Run)
	}
	if lib.Stages["build"].Run != `dotnet build lib/Lib.csproj -c Release --no-restore --disable-build-servers -p:Version="$NERV_RELEASE_VERSION"` {
		t.Errorf("lib build = %q", lib.Stages["build"].Run)
	}
	containsAll(t, "lib test", lib.Stages["test"].Run, `--collect "XPlat Code Coverage" --results-directory "$NERV_RELEASE_ARTIFACTS_DIR/coverage"`)
	if lib.Coverage == nil || lib.Coverage.Line != 80 {
		t.Errorf("lib coverage = %+v, want line 80", lib.Coverage)
	}
	if lib.Stages["build"].Env["MSBUILDDISABLENODEREUSE"] != "1" {
		t.Errorf("lib build env = %v, want the build-server-safe environment", lib.Stages["build"].Env)
	}
	containsAll(t, "lib package", lib.Stages["package"].Run, "dotnet pack lib/Lib.csproj", "docker build -f lib/Dockerfile", `--build-arg CHANNEL="$channel_tag"`, `--build-arg TAG=v-"$NERV_RELEASE_TAG"`)
	if len(lib.Hooks["before_build"]) != 1 {
		t.Errorf("hooks.before_build = %v, want kept", lib.Hooks["before_build"])
	}
	containsAll(t, "release.yaml", w.read(releaseYAMLRel),
		"# our own lint",
		"TODO(nerv-release migrate): 4.2.0 pushed the packages to https://api.nuget.org/v3/index.json",
		`TODO(nerv-release migrate): 4.2.0 ran "npm publish --access restricted"`,
		"TODO(nerv-release migrate): 4.2.0 dropped --disable-build-servers")
	containsAll(t, "stdout", stdout, "Check by hand:", "stages.publish dropped: publishing moved to nerv-release deploy", "hooks.success dropped", "nuget push", "npm publish has no deploy provider")
	w.assertNoop()
}

// S3 (6.6 TestRelease_ImportsTheSemanticReleaseConfiguration): the
// semantic-release configuration is imported into
// release.providers.conventional-commits, with its warnings reported.
func TestMigrate_ImportsTheSemanticReleaseConfiguration(t *testing.T) {
	w := fixtureWorld(t, comprobantes, migrateGitHubOrigin)
	stdout := w.mustMigrate()
	rel, err := releaseconfig.Load(w.root)
	if err != nil {
		t.Fatal(err)
	}
	engine := rel.Providers[releaseconfig.EngineProvider]
	branches, _ := engine["branches"].([]any)
	if len(branches) != 4 || branches[0] != "main" {
		t.Fatalf("branches = %+v, want the imported four, main first", branches)
	}
	if changelog, _ := engine["changelog"].(map[string]any); changelog["file"] != "src/Api/CHANGELOG.md" {
		t.Errorf("changelog = %v, want the imported file src/Api/CHANGELOG.md", engine["changelog"])
	}
	containsAll(t, "stdout", stdout,
		"the semantic-release configuration of package.json was imported into release.providers.conventional-commits; it is no longer used",
		"semantic-release package.json: @semantic-release/exec", "semantic-release package.json: @semantic-release/github")
}

// S3 (6.6 TestDeploy_GitHubHostFollowsOrigin): a repository whose
// semantic-release published GitHub releases gets deploy.host github when
// origin is GitHub, and a report otherwise.
func TestMigrate_GitHubHostFollowsOrigin(t *testing.T) {
	for _, tt := range []struct{ name, origin, host, warning string }{
		{"https", migrateGitHubOrigin, "github", ""},
		{"ssh", "git@github.com:dirm-s-a/repo.git", "github", ""},
		{"gitlab", "https://gitlab.com/acme/repo.git", "", "origin https://gitlab.com/acme/repo.git is not GitHub"},
		{"none", "", "", "deploy.host: the repository has no origin, so no git host is written"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := fixtureWorld(t, comprobantes, tt.origin)
			stdout := w.mustMigrate()
			d, err := deployconfig.Load(w.root)
			if err != nil || d.Host != tt.host {
				t.Fatalf("deploy = %+v, %v; want host %q", d, err, tt.host)
			}
			if tt.warning != "" {
				containsAll(t, "stdout", stdout, tt.warning)
			}
			containsAll(t, "stdout", stdout, "deploy: written (git writes, the tag and the host release are nerv-release deploy's)")
		})
	}
}

// S3 (6.6 TestRelease_MigratedDockerPresetPlansTheImageSaves): the dim
// fixtures migrate to build: bash with images, and the release plan of
// the migrated project saves every image and records its references for
// release-ssh-compose to push.
func TestMigrate_MigratedDockerPresetPlansTheImageSaves(t *testing.T) {
	for fixture, want := range map[string][]string{
		comprobantes: {"ghcr.io/dirm-s-a/dim.comprobantes.api", "ghcr.io/dirm-s-a/dim.comprobantes.api-login-broker"},
		factura:      {"ghcr.io/dirm-s-a/dim-factura-electronica-service"},
	} {
		t.Run(fixture, func(t *testing.T) {
			w := fixtureWorld(t, fixture, migrateGitHubOrigin)
			w.mustMigrate()
			rel, err := releaseconfig.Load(w.root)
			if err != nil {
				t.Fatal(err)
			}
			bp, err := releaseplan.NewBuildProvider(releaseplan.Providers{FS: nervrelease.ExtensionsFS()}, rel, 0)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := bp.Plan(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := releaseplan.Build(w.root, rel.Projects[0], releaseplan.Context{Version: "1.4.0-alpha.2", Channel: "alpha", Tag: "v1.4.0-alpha.2", ArtifactsDir: t.TempDir()}, planned)
			if err != nil {
				t.Fatal(err)
			}
			var saves, wantSaves []string
			for _, img := range plan.Images {
				saves = append(saves, strings.Join(img.Refs, " "))
			}
			for _, name := range want {
				wantSaves = append(wantSaves, name+":1.4.0-alpha.2 "+name+":alpha")
			}
			if !slices.Equal(saves, wantSaves) {
				t.Errorf("plan image references = %q, want %q", saves, wantSaves)
			}
		})
	}
}
