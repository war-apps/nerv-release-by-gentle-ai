package versioning

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versionfile"
)

// ImportResult is a semantic-release configuration translated into Config.
type ImportResult struct {
	// Config is the translation, with its defaults applied.
	Config Config
	// Source is the configuration file, repository-relative
	// ("package.json" for its release key).
	Source string
	// Warnings lists every part that could not be translated.
	Warnings []Warning
	// VersionFiles are the files semantic-release writes the version into
	// (@semantic-release/npm's package.json and lock file,
	// @semantic-release/git's assets in a known format), repository-relative,
	// for a project's version_files.
	VersionFiles []string
}

// Warning is one untranslatable part of a semantic-release configuration.
type Warning struct {
	// Where is the plugin or option ("@semantic-release/exec",
	// "commit-analyzer.presetConfig", "extends").
	Where string
	// Message says what is lost and what to do.
	Message string
}

func (w Warning) String() string { return w.Where + ": " + w.Message }

// ManualMigrationError reports a JavaScript configuration, which is never
// imported: it must be migrated by hand.
type ManualMigrationError struct {
	// File is the configuration file, repository-relative.
	File string
}

func (e *ManualMigrationError) Error() string {
	return fmt.Sprintf("%s is a JavaScript semantic-release configuration: it cannot be imported; migrate it by hand", e.File)
}

// ErrNoSemanticReleaseConfig is returned by Import when the repository has
// no semantic-release configuration.
var ErrNoSemanticReleaseConfig = errors.New("no semantic-release configuration found")

// searchPlaces is cosmiconfig 9's search order for semantic-release
// ("release"): package.json counts only when it has a release key.
var searchPlaces = []string{
	"package.json",
	".releaserc", ".releaserc.json", ".releaserc.yaml", ".releaserc.yml",
	".releaserc.js", ".releaserc.ts", ".releaserc.mjs", ".releaserc.cjs",
	".config/releaserc", ".config/releaserc.json", ".config/releaserc.yaml", ".config/releaserc.yml",
	".config/releaserc.js", ".config/releaserc.ts", ".config/releaserc.mjs", ".config/releaserc.cjs",
	"release.config.js", "release.config.ts", "release.config.mjs", "release.config.cjs",
}

// isScriptConfig reports whether a configuration file is JavaScript (or
// TypeScript), which is never imported.
func isScriptConfig(name string) bool {
	switch filepath.Ext(name) {
	case ".js", ".cjs", ".mjs", ".ts":
		return true
	}
	return false
}

// Import finds the repository's semantic-release configuration the way
// semantic-release does (cosmiconfig, in repoDir only) and translates it.
// A JavaScript configuration returns a *ManualMigrationError; no
// configuration returns ErrNoSemanticReleaseConfig.
func Import(repoDir string) (ImportResult, error) {
	for _, place := range searchPlaces {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(place)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ImportResult{}, err
		}
		if isScriptConfig(place) {
			return ImportResult{}, &ManualMigrationError{File: place}
		}
		var raw map[string]any
		var pkg map[string]any
		if place == "package.json" {
			if err := json.Unmarshal(data, &pkg); err != nil {
				return ImportResult{}, fmt.Errorf("package.json: %w", err)
			}
			release, ok := pkg["release"].(map[string]any)
			if !ok {
				continue
			}
			raw = release
		} else {
			if err := yaml.Unmarshal(data, &raw); err != nil {
				return ImportResult{}, fmt.Errorf("%s: %w", place, err)
			}
			if raw == nil {
				raw = map[string]any{}
			}
			if pdata, err := os.ReadFile(filepath.Join(repoDir, "package.json")); err == nil {
				_ = json.Unmarshal(pdata, &pkg)
			}
		}
		res, err := translate(raw, pkg, repoDir)
		if err != nil {
			return ImportResult{}, fmt.Errorf("%s: %w", place, err)
		}
		res.Source = place
		return res, nil
	}
	return ImportResult{}, ErrNoSemanticReleaseConfig
}

// importer accumulates the translation and its warnings.
type importer struct {
	// dir is the repository, to find the lock files the npm plugin writes.
	dir      string
	cfg      Config
	warnings []Warning
	analyzer map[string]any
	notes    map[string]any
	// versionFiles are the files found so far; gitAssets are
	// @semantic-release/git's assets, mapped once the changelog file is
	// known (the plugins' order does not matter).
	versionFiles []string
	gitAssets    []string
}

func (im *importer) warn(where, format string, args ...any) {
	im.warnings = append(im.warnings, Warning{Where: where, Message: fmt.Sprintf(format, args...)})
}

// semanticReleaseDefaultBranches are semantic-release's default branches
// without its maintenance pattern.
var semanticReleaseDefaultBranches = []Branch{
	{Name: "master"}, {Name: "main"}, {Name: "next"}, {Name: "next-major"},
	{Name: "beta", Prerelease: PrereleaseFromName()}, {Name: "alpha", Prerelease: PrereleaseFromName()},
}

// semanticReleaseDefaultPlugins are semantic-release's default plugins.
var semanticReleaseDefaultPlugins = []any{
	"@semantic-release/commit-analyzer",
	"@semantic-release/release-notes-generator",
	"@semantic-release/npm",
	"@semantic-release/github",
}

func translate(raw, pkg map[string]any, dir string) (ImportResult, error) {
	im := &importer{dir: dir}
	for _, key := range sortedKeys(raw) {
		v := raw[key]
		switch key {
		case "branches":
			im.branches(v)
		case "tagFormat":
			s, ok := v.(string)
			if !ok {
				return ImportResult{}, fmt.Errorf("tagFormat must be a string")
			}
			im.cfg.TagFormat = s
		case "repositoryUrl":
			if s, ok := v.(string); ok {
				im.cfg.RepositoryURL = s
			}
		case "plugins":
			im.plugins(v)
		case "extends":
			im.warn("extends", "shareable configurations (%v) are not loaded: merge their settings into the version configuration by hand", v)
		case "ci", "dryRun", "debug":
			im.warn(key, "a semantic-release run option; the version engine has no equivalent")
		default:
			im.warn(key, "unknown semantic-release option, not translated")
		}
	}
	if _, ok := raw["branches"]; !ok {
		im.cfg.Branches = slices.Clone(semanticReleaseDefaultBranches)
		im.warn("branches", "not set: semantic-release's default branches were imported (master, main, next, next-major, beta, alpha); its maintenance branch pattern is not supported")
	}
	if _, ok := raw["plugins"]; !ok {
		im.plugins(semanticReleaseDefaultPlugins)
	}
	if im.cfg.RepositoryURL == "" && pkg != nil {
		switch r := pkg["repository"].(type) {
		case string:
			im.cfg.RepositoryURL = r
		case map[string]any:
			if u, ok := r["url"].(string); ok {
				im.cfg.RepositoryURL = u
			}
		}
	}
	im.parserOptions()
	im.mapGitAssets()
	cfg := im.cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Config: cfg, Warnings: im.warnings, VersionFiles: im.versionFiles}, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (im *importer) branches(v any) {
	list, ok := v.([]any)
	if !ok {
		list = []any{v}
	}
	for i, item := range list {
		at := fmt.Sprintf("branches[%d]", i)
		switch b := item.(type) {
		case string:
			if maintenanceRangeRe.MatchString(b) {
				im.warn(at, "%s is a maintenance branch, which the version engine does not support; it was dropped", b)
				continue
			}
			im.cfg.Branches = append(im.cfg.Branches, Branch{Name: b})
		case map[string]any:
			name, _ := b["name"].(string)
			if name == "" {
				im.warn(at, "a branch without a name; it was dropped")
				continue
			}
			if r, has := b["range"]; has && r != false || maintenanceRangeRe.MatchString(name) && b["prerelease"] == nil {
				im.warn(at, "%s is a maintenance branch, which the version engine does not support; it was dropped", name)
				continue
			}
			br := Branch{Name: name}
			switch p := b["prerelease"].(type) {
			case bool:
				if p {
					br.Prerelease = PrereleaseFromName()
					if !validVersion("1.0.0-" + strings.ReplaceAll(name, "*", "x") + ".1") {
						im.warn(at, "prerelease: true uses the branch name as the prerelease identifier, and %s gives an invalid one: semantic-release and the version engine refuse such a branch (EPRERELEASEBRANCH); set a fixed identifier such as beta", name)
					}
				}
			case string:
				br.Prerelease = PrereleaseID(p)
			case nil:
			default:
				im.warn(at+".prerelease", "unsupported value %v, not translated", p)
			}
			if c, has := b["channel"]; has {
				switch c := c.(type) {
				case string:
					br.Channel = c
				case bool:
					if !c {
						im.warn(at+".channel", "channel: false (the default channel) is not supported; the branch keeps semantic-release's default channel")
					}
				}
			}
			for _, k := range sortedKeys(b) {
				switch k {
				case "name", "prerelease", "channel", "range":
				default:
					im.warn(at+"."+k, "unknown branch option, not translated")
				}
			}
			im.cfg.Branches = append(im.cfg.Branches, br)
		default:
			im.warn(at, "unsupported branch entry %v, not translated", item)
		}
	}
}

// pluginRef splits a plugin entry: "name", [name, config] or {path, ...}.
func pluginRef(p any) (string, map[string]any) {
	switch p := p.(type) {
	case string:
		return p, map[string]any{}
	case []any:
		if len(p) == 0 {
			return "", nil
		}
		name, _ := p[0].(string)
		cfg := map[string]any{}
		if len(p) > 1 {
			if m, ok := p[1].(map[string]any); ok {
				cfg = m
			}
		}
		return name, cfg
	case map[string]any:
		name, _ := p["path"].(string)
		cfg := map[string]any{}
		for k, v := range p {
			if k != "path" {
				cfg[k] = v
			}
		}
		return name, cfg
	}
	return "", nil
}

func (im *importer) plugins(v any) {
	list, ok := v.([]any)
	if !ok {
		im.warn("plugins", "not a list, not translated")
		return
	}
	for _, p := range list {
		name, cfg := pluginRef(p)
		switch name {
		case "@semantic-release/commit-analyzer":
			im.analyzer = cfg
			if cfg == nil {
				im.analyzer = map[string]any{}
			}
		case "@semantic-release/release-notes-generator":
			im.notes = cfg
			if cfg == nil {
				im.notes = map[string]any{}
			}
		case "@semantic-release/changelog":
			for _, k := range sortedKeys(cfg) {
				switch k {
				case "changelogFile":
					if s, ok := cfg[k].(string); ok {
						im.cfg.Changelog.File = s
					}
				case "changelogTitle":
					if s, ok := cfg[k].(string); ok {
						im.cfg.Changelog.Title = s
					}
				default:
					im.warn(name+"."+k, "unknown option, not translated")
				}
			}
		case "@semantic-release/git":
			im.git(cfg)
		case "@semantic-release/github":
			im.warn(name, "the GitHub release is not part of the version engine: nerv-release deploy's git host provider publishes it")
		case "@semantic-release/npm":
			im.npm(cfg)
		case "@semantic-release/exec":
			var cmds []string
			for _, k := range sortedKeys(cfg) {
				cmds = append(cmds, fmt.Sprintf("%s: %v", k, cfg[k]))
			}
			im.warn(name, "not translatable: its commands run outside the version engine; move them to the project's build scripts or deploy hooks (%s)", strings.Join(cmds, "; "))
		case "":
			im.warn("plugins", "unsupported plugin entry %v, not translated", p)
		default:
			im.warn(name, "unknown plugin, not translated")
		}
	}
	if im.analyzer == nil {
		im.warn("@semantic-release/commit-analyzer", "missing from plugins: semantic-release would fail without an analyzer; the version engine's commit analysis is used")
	}
}

const (
	npmPlugin = "@semantic-release/npm"
	gitPlugin = "@semantic-release/git"
)

// npm translates @semantic-release/npm: the version it writes into
// <pkgRoot>/package.json (and the lock file next to it, when the
// repository has one) becomes version files; publishing is not
// translated.
func (im *importer) npm(cfg map[string]any) {
	root := "."
	publish := true
	for _, k := range sortedKeys(cfg) {
		switch k {
		case "pkgRoot":
			if s, ok := cfg[k].(string); ok && s != "" {
				root = s
			}
		case "npmPublish":
			if b, ok := cfg[k].(bool); ok {
				publish = b
			}
		default:
			im.warn(npmPlugin+"."+k, "npm option not translated (%v)", cfg[k])
		}
	}
	if publish {
		im.warn(npmPlugin, "publishing to npm is not translated: nerv-release does not publish packages; publish from a deploy hook if needed (the version it writes is imported as version files)")
	}
	pkg := path.Join(filepath.ToSlash(root), "package.json")
	if err := versionfile.CheckRelative(pkg); err != nil {
		im.warn(npmPlugin+".pkgRoot", "%v: no version file imported", err)
		return
	}
	im.addVersionFile(pkg)
	dir := path.Dir(pkg)
	if lock := path.Join(dir, "package-lock.json"); im.exists(lock) {
		im.addVersionFile(lock)
	}
	if shrinkwrap := path.Join(dir, "npm-shrinkwrap.json"); im.exists(shrinkwrap) {
		im.warn(npmPlugin, "%s has no known version file format: add it to version_files by hand as {path: %s, format: json, keys: [version, 'packages[\"\"].version']}", shrinkwrap, shrinkwrap)
	}
}

// git translates @semantic-release/git: its assets are kept for
// mapGitAssets; the release commit is nerv-release deploy's.
func (im *importer) git(cfg map[string]any) {
	for _, k := range sortedKeys(cfg) {
		switch k {
		case "assets":
			switch a := cfg[k].(type) {
			case string:
				im.gitAssets = append(im.gitAssets, a)
			case []any:
				for _, item := range a {
					if s, ok := item.(string); ok {
						im.gitAssets = append(im.gitAssets, s)
					} else {
						im.warn(gitPlugin+".assets", "asset %v is not a path, not translated", item)
					}
				}
			default:
				im.warn(gitPlugin+".assets", "%v is not a path or a list of paths, not translated", a)
			}
		case "message":
			im.warn(gitPlugin+"."+k, "the release commit message %v is not translated: nerv-release deploy makes the release commit", cfg[k])
		default:
			im.warn(gitPlugin+"."+k, "git option not translated (%v)", cfg[k])
		}
	}
}

// mapGitAssets turns each @semantic-release/git asset that is a literal
// path in a known version file format into a version file. The changelog
// is the changelog configuration's, so it is skipped; every other asset (a
// glob, an unknown format) is named in a warning.
func (im *importer) mapGitAssets() {
	changelog := strings.TrimPrefix(path.Clean(filepath.ToSlash(im.cfg.Changelog.File)), "./")
	for _, asset := range im.gitAssets {
		p := strings.TrimPrefix(path.Clean(filepath.ToSlash(asset)), "./")
		switch {
		case strings.ContainsAny(asset, "*?[]{}!"):
			im.warn(gitPlugin+".assets", "%s is a glob: it is not imported; list the version files it matches in version_files", asset)
		case p == changelog || strings.HasPrefix(strings.ToLower(path.Base(p)), "changelog"):
		default:
			if _, err := versionfile.Resolve(versionfile.Spec{Path: p}); err != nil {
				im.warn(gitPlugin+".assets", "%s is not imported: %v (add it to version_files as a mapping, or leave it out if it holds no version)", asset, err)
				continue
			}
			im.addVersionFile(p)
		}
	}
}

// addVersionFile adds p to the version files, once.
func (im *importer) addVersionFile(p string) {
	if !slices.Contains(im.versionFiles, p) {
		im.versionFiles = append(im.versionFiles, p)
	}
}

// exists reports whether the repository has the regular file p.
func (im *importer) exists(p string) bool {
	info, err := os.Lstat(filepath.Join(im.dir, filepath.FromSlash(p)))
	return err == nil && info.Mode().IsRegular()
}

// parserOptions translates the analyzer's and the notes generator's
// options: the preset, the note keywords and the release rules.
func (im *importer) parserOptions() {
	var analyzerKeywords, notesKeywords []string
	if im.analyzer != nil {
		analyzerKeywords = im.pluginOptions("commit-analyzer", im.analyzer, true)
	}
	if im.notes != nil {
		notesKeywords = im.pluginOptions("release-notes-generator", im.notes, false)
	}
	switch {
	case analyzerKeywords != nil:
		im.cfg.NoteKeywords = analyzerKeywords
		if notesKeywords != nil && !slices.Equal(analyzerKeywords, notesKeywords) {
			im.warn("release-notes-generator.parserOpts.noteKeywords", "differs from the analyzer's %v; the version engine uses one list, the analyzer's", analyzerKeywords)
		}
	case notesKeywords != nil:
		im.cfg.NoteKeywords = notesKeywords
	}
}

// pluginOptions translates one plugin's options and returns its note
// keywords (nil when unset).
func (im *importer) pluginOptions(plugin string, cfg map[string]any, analyzer bool) []string {
	var keywords []string
	if preset, _ := cfg["preset"].(string); !strings.EqualFold(preset, "conventionalcommits") {
		if preset == "" {
			preset = "angular (the default)"
		}
		im.warn(plugin+".preset", "preset %s is not supported: the version engine always uses conventionalcommits", preset)
	}
	for _, k := range sortedKeys(cfg) {
		v := cfg[k]
		switch k {
		case "preset":
		case "parserOpts":
			opts, _ := v.(map[string]any)
			for _, ok := range sortedKeys(opts) {
				if ok == "noteKeywords" {
					list, _ := opts[ok].([]any)
					keywords = []string{}
					for _, item := range list {
						if s, isStr := item.(string); isStr {
							keywords = append(keywords, s)
						}
					}
					continue
				}
				im.warn(plugin+".parserOpts."+ok, "parser option not supported, not translated")
			}
		case "releaseRules":
			if !analyzer {
				im.warn(plugin+"."+k, "not a release-notes-generator option, ignored")
				continue
			}
			im.releaseRules(v)
		default:
			im.warn(plugin+"."+k, "option not supported, not translated")
		}
	}
	return keywords
}

func (im *importer) releaseRules(v any) {
	list, ok := v.([]any)
	if !ok {
		im.warn("commit-analyzer.releaseRules", "a releaseRules module (%v) is not loaded; write its rules as release_rules", v)
		return
	}
	for i, item := range list {
		at := fmt.Sprintf("commit-analyzer.releaseRules[%d]", i)
		m, ok := item.(map[string]any)
		if !ok {
			im.warn(at, "not a rule, not translated")
			continue
		}
		rule := ReleaseRule{}
		usable := true
		for _, k := range sortedKeys(m) {
			switch val := m[k]; k {
			case "type":
				rule.Type, _ = val.(string)
			case "scope":
				rule.Scope, _ = val.(string)
			case "breaking":
				rule.Breaking, _ = val.(bool)
			case "revert":
				rule.Revert, _ = val.(bool)
			case "release":
				switch r := val.(type) {
				case string:
					rule.Release = r
				case bool, nil:
					rule.Release = ReleaseNone
				}
			default:
				im.warn(at, "matches on %s, which the version engine's rules do not support; the rule was dropped", k)
				usable = false
			}
		}
		switch rule.Release {
		case ReleaseMajor, ReleaseMinor, ReleasePatch, ReleaseNone:
		default:
			im.warn(at, "release %q is not major, minor, patch or false; the rule was dropped", rule.Release)
			usable = false
		}
		if usable {
			im.cfg.ReleaseRules = append(im.cfg.ReleaseRules, rule)
		}
	}
}
