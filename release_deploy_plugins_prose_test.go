package nervrelease

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// assertContainsAll fails t for every want not found in text.
func assertContainsAll(t *testing.T, path, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain %q", path, want)
		}
	}
}

// agentNamePattern reads an agent file's frontmatter name.
var agentNamePattern = regexp.MustCompile(`(?m)^name: (\S+)$`)

// The release plugin ships its own agents, commands and contract skills
// from CoreFS (U5, R-S5): there is no separate deploy plugin, and no flat
// plugins/ layout.
func TestReleasePlugin_HasAgentsCommandsAndSkills(t *testing.T) {
	agents := []string{"versioner", "builder", "deployer"}
	commands := []string{"version", "setup", "build", "deploy", "deploy-setup", "ship"}
	skills := []string{"release", "deploy"}

	for _, agent := range agents {
		path := "agents/" + agent + ".md"
		data, err := fs.ReadFile(CoreFS(), path)
		if err != nil {
			t.Errorf("CoreFS()/%s is missing: %v", path, err)
			continue
		}
		m := agentNamePattern.FindSubmatch(data)
		if m == nil || string(m[1]) != agent {
			t.Errorf("CoreFS()/%s frontmatter name = %q, want %q", path, m, agent)
		}
	}
	for _, cmd := range commands {
		path := "commands/" + cmd + ".md"
		data, err := fs.ReadFile(CoreFS(), path)
		if err != nil {
			t.Errorf("CoreFS()/%s is missing: %v", path, err)
			continue
		}
		if !strings.HasPrefix(string(data), "---\ndescription: ") {
			t.Errorf("CoreFS()/%s has no description frontmatter", path)
		}
	}
	for _, s := range skills {
		skill := "skills/" + s + "/SKILL.md"
		if _, err := fs.Stat(CoreFS(), skill); err != nil {
			t.Errorf("CoreFS()/%s is missing: %v", skill, err)
		}
	}
}

// The setup commands tell the user how to import and where things are, in
// nerv-release's own vocabulary (R-S3): nerv-release build/deploy/version,
// nerv-release setup --import, and .ai-workflow/release.yaml, never nerv's.
func TestReleasePlugin_SetupNamesImportAndProviders(t *testing.T) {
	read := func(path string) string {
		data, err := fs.ReadFile(CoreFS(), path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		return string(data)
	}
	assertContainsAll(t, "commands/setup.md", read("commands/setup.md"),
		"nerv-release setup --import", "build provider", "docker-compose", "bash",
		"nerv-release version next", ".ai-workflow/release.yaml")
	assertContainsAll(t, "commands/deploy-setup.md", read("commands/deploy-setup.md"),
		"release-github", "release-ssh-compose", "release-script", ".ai-workflow/release.yaml")
	assertContainsAll(t, "commands/deploy.md", read("commands/deploy.md"),
		"nerv-release deploy --artifact", "--dry-run", "--yes")
	assertContainsAll(t, "commands/build.md", read("commands/build.md"),
		"nerv-release build --dry-run", "--expect", ".nerv/artifacts/")
	assertContainsAll(t, "commands/ship.md", read("commands/ship.md"),
		"nerv-release build --dry-run", "--expect", "nerv-release deploy --artifact", "--dry-run", "--yes")
	// L1 of merge-deploy-into-release (ported from nerv): version, build
	// (lint and tests from the configured build provider) and deploy, the
	// deploy skipped when no deploy provider is configured.
	assertContainsAll(t, "commands/ship.md", read("commands/ship.md"),
		"`deploy.host`", "`deploy.targets`", "deploy is skipped", "/release:deploy-setup",
		"`lint`", "`test`", "nerv-release adds no stage of its own", "the artifacts stay local")
}

// semanticReleaseRun matches a command line that runs semantic-release.
var semanticReleaseRun = regexp.MustCompile(`(?im)(^\s*(\$\s*)?|\b(npx|npm\s+exec|pnpm\s+(dlx|exec)|yarn(\s+dlx)?|bunx)\s+)semantic-release(\s|$)`)

// Neither the release plugin nor its 6 providers ever run semantic-release:
// the version engine is native, and an existing semantic-release config is
// only ever imported.
func TestReleasePluginAndProviders_NeverRunSemanticRelease(t *testing.T) {
	check := func(label string, fsys fs.FS) {
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			if loc := semanticReleaseRun.FindIndex(data); loc != nil {
				t.Errorf("%s/%s runs semantic-release: %q", label, path, strings.TrimSpace(string(data[loc[0]:loc[1]])))
			}
			return nil
		})
		if err != nil {
			t.Errorf("walking %s: %v", label, err)
		}
	}
	check("release (core)", CoreFS())
	for _, root := range []string{
		"release-conventional-commits", "release-docker-compose", "release-bash",
		"release-github", "release-ssh-compose", "release-script",
	} {
		sub, err := fs.Sub(ExtensionsFS(), root)
		if err != nil {
			t.Fatalf("fs.Sub(%s): %v", root, err)
		}
		check(root, sub)
	}
}
