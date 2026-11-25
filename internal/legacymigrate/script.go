package legacymigrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/provider"
)

// The deploy script target was named bash while it belonged to the deploy
// extension (deploy-bash); in the release extension, whose bash is a build
// provider, it is script (release-script).
const (
	removedScriptTarget = "bash"
	scriptTarget        = "script"
)

// bashTarget matches a provider: bash entry of a deploy target, in block
// or flow style, quoted or not.
var bashTarget = regexp.MustCompile(`(?m)(provider:[ \t]*["']?)bash(["']?)([ \t,}]|$)`)

// renameScriptTargets rewrites, in doc's key block (deploy, or defaults
// for the global release.yaml's defaults.deploy), every target provider
// bash to script, comments and layout kept, and reports whether it changed
// anything. Only deploy targets have a provider: key.
func renameScriptTargets(p *Plan, doc *config.Document, key, where string) bool {
	block, found := doc.Block(key)
	if !found || !bashTarget.MatchString(block) {
		return false
	}
	doc.SetBlock(key, bashTarget.ReplaceAllString(block, "${1}"+scriptTarget+"${2}${3}"))
	p.note("%s: deploy target provider %s renamed %s (the deploy script target of the release extension, release-%s)", where, removedScriptTarget, scriptTarget, scriptTarget)
	return true
}

// legacyDeployExtension matches the extension: deploy line of a project
// deploy provider's provider.yaml.
var legacyDeployExtension = regexp.MustCompile(`(?m)^extension:[ \t]*["']?deploy["']?([ \t]*(#.*)?)$`)

// moveProjectDeployProviders plans the move of each project deploy
// provider of the repository at root, .ai-workflow/providers/deploy-<name>,
// to release-<name>, its provider.yaml's extension: deploy rewritten
// release: a deploy provider belongs to the release extension now. A
// destination that already exists, or a name the release extension's
// build providers use, is refused.
func moveProjectDeployProviders(p *Plan, root string) ([]Change, error) {
	dir := filepath.Join(root, filepath.FromSlash(ProjectProvidersDir))
	if info, err := os.Lstat(dir); err != nil || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			return nil, refuse("refusing %s: a cloned repository must not redirect its providers through a symlink", dir)
		}
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), "deploy-")
		if !ok || !e.IsDir() {
			continue
		}
		from, to := filepath.Join(dir, e.Name()), filepath.Join(dir, "release-"+name)
		rel := ProjectProvidersDir + "/" + e.Name()
		if _, err := os.Lstat(to); err == nil {
			return nil, refuse("%s cannot move: %s/release-%s already exists; merge them by hand", rel, ProjectProvidersDir, name)
		}
		if name == removedScriptTarget {
			return nil, refuse("%s: a project deploy provider named %s would be release-%s, the release extension's build provider; rename it (its folder and name:) and run nerv-release migrate again", rel, name, name)
		}
		changes = append(changes, Change{Kind: Rename, Path: from, To: to, Why: "the deploy provider, now of the release extension"})
		manifest, exists, err := readOptional(filepath.Join(from, provider.ProviderFile))
		if err != nil {
			return nil, err
		}
		if exists && legacyDeployExtension.Match(manifest) {
			changes = append(changes, Change{Kind: Write, Path: filepath.Join(to, provider.ProviderFile), Old: manifest,
				New: legacyDeployExtension.ReplaceAll(manifest, []byte("extension: release$1")), Why: "the deploy provider, now of the release extension"})
		}
		p.note("%s moved to release-%s: the deploy extension merged into release", rel, name)
	}
	return changes, nil
}
