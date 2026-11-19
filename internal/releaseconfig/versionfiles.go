package releaseconfig

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versionfile"
)

// VersionFile is one version_files entry: a version file (Path, the plain
// string form, or the mapping {path, key, keys, pattern, format, style})
// or a command and the files it changes ({command, files, style}).
type VersionFile struct {
	// Path is the version file, relative to the repository root.
	Path string `yaml:"path"`
	// Key is one key path, shorthand for Keys with one key.
	Key string `yaml:"key"`
	// Keys are the key paths to write; empty means the file's defaults.
	Keys []string `yaml:"keys"`
	// Pattern is the regex format's pattern, with a (?P<version>...) group.
	Pattern string `yaml:"pattern"`
	// Format is the file's format; empty means inferred from Path.
	Format string `yaml:"format"`
	// Style renders the version: empty (as computed) or pep440.
	Style string `yaml:"style"`
	// Command runs in the release clone with {version} replaced by the
	// version (rendered with Style).
	Command string `yaml:"command"`
	// Files are the files Command changes, captured after it ran.
	Files []string `yaml:"files"`
}

// UnmarshalYAML decodes a plain path or a mapping.
func (f *VersionFile) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*f = VersionFile{}
		return n.Decode(&f.Path)
	case yaml.MappingNode:
		type plain VersionFile
		var m plain
		if err := n.Decode(&m); err != nil {
			return err
		}
		*f = VersionFile(m)
		return nil
	}
	return fmt.Errorf("line %d: a version_files entry is a path or a mapping {path, key, keys, pattern, format, style} or {command, files, style}", n.Line)
}

// Spec is a path entry as versionfile takes it (Key becomes Keys).
func (f VersionFile) Spec() versionfile.Spec {
	keys := f.Keys
	if f.Key != "" {
		keys = []string{f.Key}
	}
	return versionfile.Spec{Path: f.Path, Format: f.Format, Keys: keys, Pattern: f.Pattern, Style: f.Style}
}

// checkVersionFile validates one version_files entry: a path entry is
// resolved by versionfile (its format inferred or known, its keys, pattern
// and style valid for it); a command entry needs files and takes only
// files and style.
func (v *validator) checkVersionFile(at string, f VersionFile) {
	switch {
	case f.Path != "" && f.Command != "":
		v.addf("%s: set path or command, not both", at)
	case f.Command != "":
		v.checkVersionCommand(at, f)
	case f.Path == "":
		v.addf("%s: set path (a version file) or command (a command and the files it changes)", at)
	case f.Key != "" && len(f.Keys) > 0:
		v.addf("%s: set key or keys, not both", at)
	case !v.pathProblem(at, f.Path):
		s := f.Spec()
		s.Path = normalize(s.Path)
		if _, err := versionfile.Resolve(s); err != nil {
			v.addf("%s: %v", at, err)
		}
	}
}

// checkVersionCommand validates a {command, files, style} entry.
func (v *validator) checkVersionCommand(at string, f VersionFile) {
	if f.Key != "" || len(f.Keys) > 0 || f.Pattern != "" || f.Format != "" {
		v.addf("%s: command takes only files and style (not key, keys, pattern or format)", at)
	}
	if f.Style != "" && f.Style != versionfile.StylePEP440 {
		v.addf("%s.style: unknown style %q (only %q)", at, f.Style, versionfile.StylePEP440)
	}
	if len(f.Files) == 0 {
		v.addf("%s.files: required with command (the files the command changes)", at)
	}
	for k, file := range f.Files {
		fat := fmt.Sprintf("%s.files[%d]", at, k)
		switch {
		case file == "":
			v.addf("%s: required", fat)
		case !v.pathProblem(fat, file):
			if err := versionfile.CheckRelative(normalize(file)); err != nil {
				v.addf("%s: %v", fat, err)
			}
		}
	}
}
