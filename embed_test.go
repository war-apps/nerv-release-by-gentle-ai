package nervrelease

import (
	"bytes"
	"io/fs"
	"testing"
)

// CoreFS is the release plugin's own tree: its own files directly at the
// root, with no "release/" folder of its own (U5, R-S5).
func TestCoreFS_IsTheReleasePluginsOwnTree(t *testing.T) {
	for _, path := range []string{
		".claude-plugin/plugin.json",
		"extension.yaml",
		"agents/builder.md",
		"agents/deployer.md",
		"agents/versioner.md",
		"commands/build.md",
		"commands/deploy.md",
		"commands/deploy-setup.md",
		"commands/setup.md",
		"commands/ship.md",
		"commands/version.md",
		"skills/release/SKILL.md",
		"skills/deploy/SKILL.md",
	} {
		if _, err := fs.Stat(CoreFS(), path); err != nil {
			t.Errorf("CoreFS() is missing %s: %v", path, err)
		}
	}
}

// ExtensionsFS folds CoreFS in as a "release" folder, a sibling of the 6
// provider plugins, so the release engine (internal/releaseplan.Providers
// and friends) keeps seeing one flat tree, as it did before the
// plugin/release + plugin/extensions split.
func TestExtensionsFS_HasReleaseAsASiblingOfItsProviders(t *testing.T) {
	entries, err := fs.ReadDir(ExtensionsFS(), ".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if !e.IsDir() {
			t.Errorf("entry %q is not reported as a directory", e.Name())
		}
	}
	want := []string{
		"release", "release-bash", "release-conventional-commits",
		"release-docker-compose", "release-github", "release-script",
		"release-ssh-compose",
	}
	if len(names) != len(want) {
		t.Fatalf("ReadDir(.) = %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("ReadDir(.)[%d] = %q, want %q (full: %v)", i, names[i], w, names)
		}
	}
}

// release/extension.yaml through ExtensionsFS() is the same file CoreFS()
// serves at its own root: one source of truth, materialized twice.
func TestExtensionsFS_ReleaseFolderIsCoreFS(t *testing.T) {
	viaCore, err := fs.ReadFile(CoreFS(), "extension.yaml")
	if err != nil {
		t.Fatalf("CoreFS() extension.yaml: %v", err)
	}
	viaExtensions, err := fs.ReadFile(ExtensionsFS(), "release/extension.yaml")
	if err != nil {
		t.Fatalf("ExtensionsFS() release/extension.yaml: %v", err)
	}
	if !bytes.Equal(viaCore, viaExtensions) {
		t.Errorf("release/extension.yaml via ExtensionsFS() does not match CoreFS()'s")
	}

	if _, err := fs.Stat(ExtensionsFS(), "release"); err != nil {
		t.Errorf(`Stat("release"): %v`, err)
	}
}

// Every provider plugin is reachable from ExtensionsFS() by its folder
// name, exactly as the release engine resolves release-<name>.
func TestExtensionsFS_HasEveryProviderManifest(t *testing.T) {
	for _, dir := range []string{
		"release-bash", "release-conventional-commits", "release-docker-compose",
		"release-github", "release-script", "release-ssh-compose",
	} {
		if _, err := fs.Stat(ExtensionsFS(), dir+"/provider.yaml"); err != nil {
			t.Errorf("%s/provider.yaml: %v", dir, err)
		}
		if _, err := fs.Stat(ExtensionsFS(), dir+"/README.md"); err != nil {
			t.Errorf("%s/README.md: %v", dir, err)
		}
		if _, err := fs.Stat(ExtensionsFS(), dir+"/.claude-plugin/plugin.json"); err != nil {
			t.Errorf("%s/.claude-plugin/plugin.json: %v", dir, err)
		}
	}
}

// fs.WalkDir is how the prose tests sweep every plugin file (for stray
// {nerv} or bare nerv CLI mentions); it must work over the whole union,
// descending into "release" and into every provider folder.
func TestExtensionsFS_WalkDirCoversEveryPlugin(t *testing.T) {
	seen := map[string]bool{}
	err := fs.WalkDir(ExtensionsFS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			seen[path] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	for _, path := range []string{
		"release/extension.yaml",
		"release/commands/build.md",
		"release/skills/release/SKILL.md",
		"release-bash/provider.yaml",
		"release-ssh-compose/provider.yaml",
	} {
		if !seen[path] {
			t.Errorf("WalkDir never visited %s (seen: %v)", path, seen)
		}
	}
}
