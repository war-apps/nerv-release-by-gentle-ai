// Package nervrelease embeds the nerv-release plugin tree: the release
// plugin itself (plugin/release, the extension's manifest, its commands,
// agents and skills) and its 6 provider plugins (plugin/extensions,
// release-bash, release-conventional-commits, release-docker-compose,
// release-github, release-script and release-ssh-compose).
//
// The source tree keeps the release plugin and its providers in two
// separate roots, plugin/release and plugin/extensions (the layout
// nerv-kit's claudeplugin.MaterializeBundle and nerv-task-by-gentle-ai's
// embed.go both use: a core plugin tree plus one folder per extension
// plugin). The release engine (internal/releaseplan.Providers,
// internal/deployprovider, internal/workflowconfig.LoadPersonal, ...)
// instead needs the release extension's own manifest and its
// release-<name> providers as siblings of one tree, exactly as the single
// flat plugins/ folder gave them before this split (U5, R-S5). ExtensionsFS
// keeps giving them that shape: it is CoreFS's own tree folded in as one
// more top-level folder, named "release", next to the provider plugins.
package nervrelease

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed all:plugin/release
var coreFS embed.FS

//go:embed all:plugin/extensions
var extensionsFS embed.FS

// coreName is CoreFS's own folder name inside ExtensionsFS: the release
// extension's name, and the prefix of every provider it owns
// (release-<name>).
const coreName = "release"

// CoreFS is the release plugin's own tree, rooted at its own files
// (.claude-plugin/plugin.json, extension.yaml, agents/, commands/,
// skills/).
func CoreFS() fs.FS {
	sub, err := fs.Sub(coreFS, "plugin/release")
	if err != nil {
		// Unreachable: "plugin/release" is a constant root name.
		panic(err)
	}
	return sub
}

// rawExtensionsFS is the tree of the 6 provider plugins this build ships,
// one top-level folder each (release-bash, release-conventional-commits,
// release-docker-compose, release-github, release-script,
// release-ssh-compose), with no "release" folder of its own: the shape
// claudeplugin.MaterializeBundle expects alongside CoreFS.
func rawExtensionsFS() fs.FS {
	sub, err := fs.Sub(extensionsFS, "plugin/extensions")
	if err != nil {
		// Unreachable: "plugin/extensions" is a constant root name.
		panic(err)
	}
	return sub
}

// ProvidersFS returns the 6 provider plugins alone, with no "release"
// folder of its own: the shape claudeplugin.MaterializeBundle wants for
// its own extensions argument (nerv-release install, U6: the release
// extension itself is materialized through CoreFS as the marketplace's
// core plugin, never a second time as if it were one of its own
// providers).
func ProvidersFS() fs.FS {
	return rawExtensionsFS()
}

// ExtensionsFS returns the release extension and its providers as one
// tree, one folder each, such as "release/extension.yaml" and
// "release-bash/provider.yaml", so the release engine can load the
// extension's manifest and resolve a provider by its folder name without
// knowing the two live in separate source roots.
func ExtensionsFS() fs.FS {
	return unionFS{core: CoreFS(), ext: rawExtensionsFS()}
}

// unionFS folds core in as one more top-level folder of ext, named
// coreName, so a caller sees one flat tree with every plugin (the release
// extension and its providers) as a sibling.
type unionFS struct {
	core fs.FS
	ext  fs.FS
}

// target is the FS and the path within it that name resolves to: core for
// coreName itself or anything under it, ext otherwise.
func (u unionFS) target(name string) (fsys fs.FS, rel string) {
	if name == coreName {
		return u.core, "."
	}
	if rest, ok := strings.CutPrefix(name, coreName+"/"); ok {
		return u.core, rest
	}
	return u.ext, name
}

func (u unionFS) Open(name string) (fs.File, error) {
	fsys, rel := u.target(name)
	return fsys.Open(rel)
}

func (u unionFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." {
		return syntheticDir("."), nil
	}
	if name == coreName {
		return syntheticDir(coreName), nil
	}
	fsys, rel := u.target(name)
	return fs.Stat(fsys, rel)
}

func (u unionFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		entries, err := fs.ReadDir(u.ext, ".")
		if err != nil {
			return nil, err
		}
		entries = append(entries, syntheticDirEntry(coreName))
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		return entries, nil
	}
	fsys, rel := u.target(name)
	return fs.ReadDir(fsys, rel)
}

// syntheticDir and syntheticDirEntry stand in for coreName (and the union
// root) as a plain directory: embed.FS roots report their own name (often
// ".") rather than the folder name the union gives them, so these are
// built directly instead of asking core or ext for their FileInfo.
type syntheticDir string

func (d syntheticDir) Name() string       { return string(d) }
func (d syntheticDir) Size() int64        { return 0 }
func (d syntheticDir) Mode() fs.FileMode  { return fs.ModeDir }
func (d syntheticDir) ModTime() time.Time { return time.Time{} }
func (d syntheticDir) IsDir() bool        { return true }
func (d syntheticDir) Sys() any           { return nil }

type syntheticDirEntry string

func (d syntheticDirEntry) Name() string               { return string(d) }
func (d syntheticDirEntry) IsDir() bool                { return true }
func (d syntheticDirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (d syntheticDirEntry) Info() (fs.FileInfo, error) { return syntheticDir(d), nil }
