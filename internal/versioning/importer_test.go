package versioning

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeFiles writes files (name -> content) into a new temporary directory.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "import", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func warningsAt(ws []Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Where)
	}
	return out
}

func TestImportRealPackageJSON(t *testing.T) {
	for _, c := range []struct{ fixture, changelog, repo, csproj string }{
		{"acme-api-package.json", "src/Api/CHANGELOG.md", "https://github.com/acme-corp/acme-api.git", "src/Api/Acme.Api.csproj"},
		{"acme-billing-package.json", "src/Host/CHANGELOG.md", "https://github.com/acme-corp/acme-billing-service.git", "src/Host/AcmeBilling.Host.csproj"},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"package.json": fixture(t, c.fixture)})
			res, err := Import(dir)
			if err != nil {
				t.Fatal(err)
			}
			if res.Source != "package.json" {
				t.Fatalf("source = %q", res.Source)
			}
			cfg := res.Config
			want := []Branch{
				{Name: "main"},
				{Name: "release/rc", Prerelease: PrereleaseID("rc"), Channel: "beta"},
				{Name: "develop", Prerelease: PrereleaseID("alpha"), Channel: "alpha"},
				{Name: "hotfix/*", Prerelease: PrereleaseFromName(), Channel: "beta"},
			}
			if !slices.Equal(cfg.Branches, want) {
				t.Fatalf("branches = %+v, want %+v", cfg.Branches, want)
			}
			if !slices.Equal(cfg.NoteKeywords, DefaultNoteKeywords()) || cfg.Changelog.File != c.changelog || cfg.RepositoryURL != c.repo || cfg.TagFormat != "v${version}" || len(cfg.ReleaseRules) != 0 {
				t.Fatalf("config = %+v", cfg)
			}
			at := warningsAt(res.Warnings)
			for _, w := range []string{"@semantic-release/exec", "@semantic-release/github", "branches[3]"} {
				if !slices.Contains(at, w) {
					t.Errorf("no warning at %s; warnings: %v", w, res.Warnings)
				}
			}
			// npm (npmPublish: false) and git (every asset mapped or the
			// changelog) lose nothing: their files are version files.
			for _, w := range []string{"@semantic-release/npm", "@semantic-release/git", "@semantic-release/git.assets"} {
				if slices.Contains(at, w) {
					t.Errorf("a warning at %s; warnings: %v", w, res.Warnings)
				}
			}
			if want := []string{"package.json", c.csproj}; !slices.Equal(res.VersionFiles, want) {
				t.Errorf("version files = %v, want %v", res.VersionFiles, want)
			}
			for _, w := range res.Warnings {
				if w.Where == "@semantic-release/exec" && !strings.Contains(w.Message, "bump-csproj.sh") {
					t.Errorf("exec warning does not name its commands: %q", w.Message)
				}
			}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportReleasercYAML(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"package.json": `{"name": "x"}`,
		".releaserc.yaml": `
extends: "@acme/release-config"
branches: [main, {name: next, channel: next}, {name: "1.x", range: "1.x"}]
tagFormat: app-v${version}
plugins:
  - - "@semantic-release/commit-analyzer"
    - preset: angular
      presetConfig: {types: []}
      releaseRules:
        - {type: docs, scope: README, release: patch}
        - {type: refactor, release: false}
        - {subject: "*skip*", release: false}
      parserOpts: {noteKeywords: [BREAKING], headerPattern: "x"}
  - "@semantic-release/release-notes-generator"
  - ["@semantic-release/changelog", {changelogFile: HISTORY.md, changelogTitle: "# History"}]
  - "semantic-release-slack-bot"
`,
	})
	res, err := Import(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := res.Config
	if res.Source != ".releaserc.yaml" || cfg.TagFormat != "app-v${version}" || cfg.Changelog.File != "HISTORY.md" || cfg.Changelog.Title != "# History" {
		t.Fatalf("config = %+v (source %q)", cfg, res.Source)
	}
	if len(cfg.Branches) != 2 || cfg.Branches[1].Channel != "next" {
		t.Fatalf("branches = %+v", cfg.Branches)
	}
	wantRules := []ReleaseRule{{Type: "docs", Scope: "README", Release: ReleasePatch}, {Type: "refactor", Release: ReleaseNone}}
	if !slices.Equal(cfg.ReleaseRules, wantRules) || !slices.Equal(cfg.NoteKeywords, []string{"BREAKING"}) {
		t.Fatalf("rules = %+v keywords = %v", cfg.ReleaseRules, cfg.NoteKeywords)
	}
	at := warningsAt(res.Warnings)
	for _, w := range []string{"extends", "branches[2]", "commit-analyzer.preset", "commit-analyzer.presetConfig", "commit-analyzer.releaseRules[2]", "commit-analyzer.parserOpts.headerPattern", "release-notes-generator.preset", "semantic-release-slack-bot"} {
		if !slices.Contains(at, w) {
			t.Errorf("no warning at %s; warnings: %v", w, res.Warnings)
		}
	}
}

func TestImportDefaultPluginsAndBranches(t *testing.T) {
	dir := writeFiles(t, map[string]string{".releaserc": `{"tagFormat": "${version}"}`})
	res, err := Import(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, b := range res.Config.Branches {
		names = append(names, b.Name)
	}
	if !slices.Equal(names, []string{"master", "main", "next", "next-major", "beta", "alpha"}) || !res.Config.Branches[4].Prerelease.UsesName() {
		t.Fatalf("branches = %+v", res.Config.Branches)
	}
	at := warningsAt(res.Warnings)
	for _, w := range []string{"branches", "@semantic-release/npm", "commit-analyzer.preset"} {
		if !slices.Contains(at, w) {
			t.Errorf("no warning at %s; warnings: %v", w, res.Warnings)
		}
	}
}

func TestImportRefusesJavaScript(t *testing.T) {
	for _, name := range []string{".releaserc.js", ".releaserc.cjs", ".releaserc.mjs", "release.config.js", "release.config.cjs", "release.config.mjs"} {
		dir := writeFiles(t, map[string]string{name: "module.exports = {}"})
		_, err := Import(dir)
		var manual *ManualMigrationError
		if !errors.As(err, &manual) || manual.File != name || !strings.Contains(err.Error(), "by hand") {
			t.Errorf("%s: err = %v, want a ManualMigrationError", name, err)
		}
	}
}

func TestImportWithoutConfig(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package.json": `{"name": "x"}`})
	if _, err := Import(dir); !errors.Is(err, ErrNoSemanticReleaseConfig) {
		t.Fatalf("err = %v, want ErrNoSemanticReleaseConfig", err)
	}
}

// warningsNaming returns the messages of the warnings at where.
func warningsNaming(ws []Warning, where string) []string {
	var out []string
	for _, w := range ws {
		if w.Where == where {
			out = append(out, w.Message)
		}
	}
	return out
}

// R-S7: @semantic-release/npm writes package.json's version (and
// package-lock.json's, when the repository has one): both become version
// files; publishing is not translated and stays a warning.
func TestImportNpmPluginMapsPackageJSONAndAnExistingLockFile(t *testing.T) {
	for _, c := range []struct {
		name string
		lock bool
		want []string
	}{
		{"with a lock file", true, []string{"package.json", "package-lock.json"}},
		{"without a lock file", false, []string{"package.json"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{
				"package.json": `{"name": "x", "version": "1.0.0"}`,
				".releaserc":   `{"plugins": ["@semantic-release/commit-analyzer", "@semantic-release/npm"]}`,
			}
			if c.lock {
				files["package-lock.json"] = `{"version": "1.0.0"}`
			}
			res, err := Import(writeFiles(t, files))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.VersionFiles, c.want) {
				t.Errorf("version files = %v, want %v", res.VersionFiles, c.want)
			}
			ws := warningsNaming(res.Warnings, "@semantic-release/npm")
			if len(ws) != 1 || !strings.Contains(ws[0], "publish") {
				t.Errorf("npm warnings = %v, want one about publishing", ws)
			}
		})
	}
}

// R-S7: pkgRoot moves the package: <pkgRoot>/package.json (and its lock
// file); npmPublish: false publishes nothing, so nothing is lost; an
// npm-shrinkwrap.json has no known format and is named in a warning.
func TestImportNpmPluginPkgRoot(t *testing.T) {
	res, err := Import(writeFiles(t, map[string]string{
		"dist/package-lock.json":   `{"version": "1.0.0"}`,
		"dist/npm-shrinkwrap.json": `{"version": "1.0.0"}`,
		".releaserc":               `{"plugins": ["@semantic-release/commit-analyzer", ["@semantic-release/npm", {"npmPublish": false, "pkgRoot": "./dist", "tarballDir": "out"}]]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dist/package.json", "dist/package-lock.json"}; !slices.Equal(res.VersionFiles, want) {
		t.Errorf("version files = %v, want %v", res.VersionFiles, want)
	}
	if ws := warningsNaming(res.Warnings, "@semantic-release/npm"); len(ws) != 1 || !strings.Contains(ws[0], "dist/npm-shrinkwrap.json") {
		t.Errorf("npm warnings = %v, want only the shrinkwrap one", ws)
	}
	if ws := warningsNaming(res.Warnings, "@semantic-release/npm.tarballDir"); len(ws) != 1 {
		t.Errorf("tarballDir warnings = %v, want one", ws)
	}
}

// R-S7: @semantic-release/git's assets that are literal paths in a known
// format become version files; the changelog is the changelog
// configuration's; globs and unknown formats stay warnings naming them.
func TestImportGitAssets(t *testing.T) {
	res, err := Import(writeFiles(t, map[string]string{
		".releaserc": `{"plugins": ["@semantic-release/commit-analyzer",
			["@semantic-release/changelog", {"changelogFile": "docs/HISTORY.md"}],
			["@semantic-release/git", {"assets": ["CHANGELOG.md", "docs/HISTORY.md", "pom.xml", "src/*.csproj", "docs/notes.txt", "VERSION", "src/App/App.csproj"], "message": "chore(release): ${nextRelease.version}"}]]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pom.xml", "VERSION", "src/App/App.csproj"}; !slices.Equal(res.VersionFiles, want) {
		t.Errorf("version files = %v, want %v", res.VersionFiles, want)
	}
	assets := strings.Join(warningsNaming(res.Warnings, "@semantic-release/git.assets"), "\n")
	for _, want := range []string{"src/*.csproj", "docs/notes.txt"} {
		if !strings.Contains(assets, want) {
			t.Errorf("no assets warning names %s: %q", want, assets)
		}
	}
	for _, skipped := range []string{"CHANGELOG.md", "HISTORY.md", "pom.xml"} {
		if strings.Contains(assets, skipped) {
			t.Errorf("an assets warning names %s: %q", skipped, assets)
		}
	}
	if ws := warningsNaming(res.Warnings, "@semantic-release/git.message"); len(ws) != 1 {
		t.Errorf("message warnings = %v, want one", ws)
	}
}

// R-S7: assets may be one string; package.json from npm and git is listed
// once.
func TestImportGitAssetStringAndDuplicates(t *testing.T) {
	res, err := Import(writeFiles(t, map[string]string{
		"package.json": `{"name": "x", "release": {"plugins": ["@semantic-release/commit-analyzer", "@semantic-release/npm", ["@semantic-release/git", {"assets": "package.json"}]]}}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"package.json"}; !slices.Equal(res.VersionFiles, want) {
		t.Errorf("version files = %v, want %v", res.VersionFiles, want)
	}
	if ws := warningsNaming(res.Warnings, "@semantic-release/git"); len(ws) != 0 {
		t.Errorf("git warnings = %v, want none (every asset mapped)", ws)
	}
}
