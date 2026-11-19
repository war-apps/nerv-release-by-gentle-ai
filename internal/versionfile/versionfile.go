// Package versionfile writes a release version into the version files of
// any ecosystem (package.json, Cargo.toml, pyproject.toml, Chart.yaml,
// pom.xml, gradle.properties, VERSION, or any file through a regex).
//
// Every edit is a value-span edit: the existing value's bytes are located
// with a small scanner and only they are replaced, so the rest of the file
// (BOM, line endings, comments, indentation, key order) stays byte for byte.
// MSBuild project files are not written here: releaseplan.WriteCsproj owns
// them because it inserts a missing <Version>.
package versionfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/war-apps/nerv-kit-by-gentle-ai/atomicfile"
)

// Spec is one version file edit.
type Spec struct {
	Path    string   // repository-relative, forward slashes
	Format  string   // "" = inferred from Path
	Keys    []string // key paths; nil = the format's/file's defaults
	Pattern string   // format regex only: must have a named group (?P<version>...)
	Style   string   // "" (the version as given) or "pep440"

	// firstKey is set by Resolve when Keys are pyproject.toml's defaults:
	// only the first key that exists is written.
	firstKey bool
}

// The formats a Spec may name.
const (
	FormatMSBuild    = "msbuild"
	FormatJSON       = "json"
	FormatTOML       = "toml"
	FormatYAML       = "yaml"
	FormatXML        = "xml"
	FormatProperties = "properties"
	FormatText       = "text"
	FormatRegex      = "regex"
)

// StylePEP440 renders SemVer prereleases as PEP 440 versions.
const StylePEP440 = "pep440"

var formats = []string{FormatMSBuild, FormatJSON, FormatTOML, FormatYAML, FormatXML, FormatProperties, FormatText, FormatRegex}

// msbuildExtensions mirror releaseconfig.VersionFileExtensions.
var msbuildExtensions = []string{".csproj", ".fsproj", ".vbproj", ".props", ".targets"}

// known is a file whose format and keys are inferred from its base name.
type known struct {
	format   string
	keys     []string
	firstKey bool
}

// knownFiles are keyed by lower-case base name.
var knownFiles = map[string]known{
	"package.json":      {format: FormatJSON, keys: []string{"version"}},
	"package-lock.json": {format: FormatJSON, keys: []string{"version", `packages[""].version`}},
	"composer.json":     {format: FormatJSON, keys: []string{"version"}},
	"cargo.toml":        {format: FormatTOML, keys: []string{"package.version"}},
	"pyproject.toml":    {format: FormatTOML, keys: []string{"project.version", "tool.poetry.version"}, firstKey: true},
	"chart.yaml":        {format: FormatYAML, keys: []string{"version", "appVersion"}},
	"pubspec.yaml":      {format: FormatYAML, keys: []string{"version"}},
	"pom.xml":           {format: FormatXML, keys: []string{"project/version"}},
	"gradle.properties": {format: FormatProperties, keys: []string{"version"}},
	"version":           {format: FormatText},
	"version.txt":       {format: FormatText},
}

func lookup(p string) (known, bool) {
	base := strings.ToLower(path.Base(p))
	if k, ok := knownFiles[base]; ok {
		return k, true
	}
	if slices.Contains(msbuildExtensions, path.Ext(base)) {
		return known{format: FormatMSBuild}, true
	}
	return known{}, false
}

// Resolve fills Format and default Keys from Path when empty, and validates
// the spec (known format, keys valid for it, regex compiles with a `version`
// group and Pattern only with regex, Style known). It returns the resolved
// copy or an error naming the problem.
func Resolve(s Spec) (Spec, error) {
	s.Keys = slices.Clone(s.Keys)
	if s.Path == "" {
		return s, errors.New("version file: path is empty")
	}
	if path.IsAbs(s.Path) || filepath.IsAbs(s.Path) || filepath.VolumeName(s.Path) != "" ||
		slices.Contains(strings.Split(s.Path, "/"), "..") {
		return s, fmt.Errorf("version file %s: path must be relative to the repository, without ..", s.Path)
	}
	if s.Style != "" && s.Style != StylePEP440 {
		return s, fmt.Errorf("version file %s: unknown style %q (only %q)", s.Path, s.Style, StylePEP440)
	}
	k, isKnown := lookup(s.Path)
	if s.Format == "" {
		if !isKnown {
			return s, fmt.Errorf("version file %s: cannot infer its format; set format (%s)", s.Path, strings.Join(formats, ", "))
		}
		s.Format = k.format
	}
	if !slices.Contains(formats, s.Format) {
		return s, fmt.Errorf("version file %s: unknown format %q (%s)", s.Path, s.Format, strings.Join(formats, ", "))
	}
	if s.Pattern != "" && s.Format != FormatRegex {
		return s, fmt.Errorf("version file %s: pattern is only valid with format regex", s.Path)
	}
	switch s.Format {
	case FormatMSBuild, FormatText:
		if len(s.Keys) > 0 {
			return s, fmt.Errorf("version file %s: format %s takes no keys", s.Path, s.Format)
		}
		s.Keys = nil
	case FormatRegex:
		if len(s.Keys) > 0 {
			return s, fmt.Errorf("version file %s: format regex takes no keys, only a pattern", s.Path)
		}
		s.Keys = nil
		if s.Pattern == "" {
			return s, fmt.Errorf("version file %s: format regex needs a pattern", s.Path)
		}
		if _, err := compilePattern(s.Pattern); err != nil {
			return s, fmt.Errorf("version file %s: pattern: %w", s.Path, err)
		}
	default:
		if len(s.Keys) == 0 {
			if !isKnown || k.format != s.Format {
				return s, fmt.Errorf("version file %s: format %s needs keys", s.Path, s.Format)
			}
			s.Keys = slices.Clone(k.keys)
			s.firstKey = k.firstKey
		} else if s.firstKey && !(isKnown && slices.Equal(s.Keys, k.keys)) {
			s.firstKey = false
		}
		for _, key := range s.Keys {
			if _, err := parseKey(s.Format, key); err != nil {
				return s, fmt.Errorf("version file %s: key %s: %w", s.Path, key, err)
			}
		}
	}
	return s, nil
}

func compilePattern(p string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	if re.SubexpIndex("version") < 0 {
		return nil, errors.New("needs a named group (?P<version>...)")
	}
	return re, nil
}

var (
	semverRe       = regexp.MustCompile(`^(\d+\.\d+\.\d+)(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?$`)
	pep440PreRe    = regexp.MustCompile(`^(alpha|beta|rc)\.(\d+)$`)
	pep440Prefixes = map[string]string{"alpha": "a", "beta": "b", "rc": "rc"}
	safeVersionRe  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)
)

// Render formats version for style: "" returns it unchanged; pep440 maps
// X.Y.Z-alpha.N to X.Y.ZaN, beta to bN and rc to rcN and keeps a release
// unchanged; any other prerelease, or build metadata, is an error.
func Render(version, style string) (string, error) {
	switch style {
	case "":
		return version, nil
	case StylePEP440:
		m := semverRe.FindStringSubmatch(version)
		if m == nil {
			return "", fmt.Errorf("version %q is not X.Y.Z[-prerelease]: cannot render it as pep440", version)
		}
		if m[3] != "" {
			return "", fmt.Errorf("version %q has build metadata, which pep440 cannot express", version)
		}
		if m[2] == "" {
			return m[1], nil
		}
		pre := pep440PreRe.FindStringSubmatch(m[2])
		if pre == nil {
			return "", fmt.Errorf("version %q: pep440 renders only alpha.N, beta.N and rc.N prereleases", version)
		}
		return m[1] + pep440Prefixes[pre[1]] + pre[2], nil
	default:
		return "", fmt.Errorf("unknown version style %q (only %q)", style, StylePEP440)
	}
}

// Apply returns content with the version (rendered with the spec's style)
// written at every key, or regex match, of the spec, which it resolves
// first. It is pure: no I/O. MSBuild files are refused: they are written by
// releaseplan.WriteCsproj.
func Apply(content []byte, s Spec, version string) ([]byte, error) {
	s, err := Resolve(s)
	if err != nil {
		return nil, err
	}
	if s.Format == FormatMSBuild {
		return nil, fmt.Errorf("version file %s: msbuild files are written by releaseplan.WriteCsproj, not versionfile", s.Path)
	}
	v, err := Render(version, s.Style)
	if err != nil {
		return nil, fmt.Errorf("version file %s: %w", s.Path, err)
	}
	if !safeVersionRe.MatchString(v) {
		return nil, fmt.Errorf("version file %s: version %q may hold only [0-9A-Za-z.+-] and must start with a letter or digit", s.Path, v)
	}
	bom := 0
	if bytes.HasPrefix(content, utf8BOM) {
		bom = len(utf8BOM)
	}
	edits, err := locate(content[bom:], s)
	if err != nil {
		return nil, err
	}
	return replace(content, bom, edits, v), nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Write applies the spec to root/Path and replaces the file atomically,
// keeping its permission bits; on any error the file is unchanged.
func Write(root string, s Spec, version string) error {
	file := filepath.Join(root, filepath.FromSlash(s.Path))
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("version file %s: %w", s.Path, err)
	}
	out, err := Apply(content, s, version)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(file, out, 0o644); err != nil {
		return fmt.Errorf("version file %s: %w", s.Path, err)
	}
	return nil
}

// edit replaces body[start:end] with wrap+version+wrap.
type edit struct {
	start, end int
	wrap       string
}

// replace applies edits (offsets into content[bom:]) to content.
func replace(content []byte, bom int, edits []edit, version string) []byte {
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })
	edits = slices.CompactFunc(edits, func(a, b edit) bool { return a.start == b.start })
	var out bytes.Buffer
	last := 0
	body := content[bom:]
	out.Write(content[:bom])
	for _, e := range edits {
		out.Write(body[last:e.start])
		out.WriteString(e.wrap + version + e.wrap)
		last = e.end
	}
	out.Write(body[last:])
	return out.Bytes()
}

// value is one value a format scanner found: its path from the root, its
// byte span in the body and how a version replaces it.
type value struct {
	path       []string
	start, end int
	wrap       string // quote written around the version ("" keeps the span's own quotes)
	notScalar  string // non-empty: why the value cannot hold a version
}

// scanners return every value of a body by path.
var scanners = map[string]func([]byte) ([]value, error){
	FormatJSON:       scanJSON,
	FormatTOML:       scanTOML,
	FormatYAML:       scanYAML,
	FormatXML:        scanXML,
	FormatProperties: scanProperties,
}

// locate returns the edits that write the version into body for s.
func locate(body []byte, s Spec) ([]edit, error) {
	switch s.Format {
	case FormatText:
		return locateText(body, s.Path)
	case FormatRegex:
		return locateRegex(body, s)
	}
	values, err := scanners[s.Format](body)
	if err != nil {
		return nil, fmt.Errorf("version file %s: cannot parse it as %s: %w", s.Path, s.Format, err)
	}
	var edits []edit
	for _, key := range s.Keys {
		want, _ := parseKey(s.Format, key)
		found := false
		for _, v := range values {
			if !slices.Equal(v.path, want) {
				continue
			}
			if v.notScalar != "" {
				return nil, fmt.Errorf("version file %s: key %s is not a scalar value (%s)", s.Path, key, v.notScalar)
			}
			found = true
			edits = append(edits, edit{v.start, v.end, v.wrap})
		}
		if found && s.firstKey {
			return edits, nil
		}
		if !found && !s.firstKey {
			return nil, fmt.Errorf("version file %s: key %s not found", s.Path, key)
		}
	}
	if len(edits) == 0 {
		return nil, fmt.Errorf("version file %s: none of the keys %s found", s.Path, strings.Join(s.Keys, ", "))
	}
	return edits, nil
}

// locateText edits a file that holds only the version, keeping its
// trailing line breaks.
func locateText(body []byte, file string) ([]edit, error) {
	end := len(bytes.TrimRight(body, "\r\n"))
	if bytes.ContainsAny(body[:end], "\r\n") {
		return nil, fmt.Errorf("version file %s: a text version file must hold only the version", file)
	}
	return []edit{{0, end, ""}}, nil
}

// locateRegex edits the version group of every match.
func locateRegex(body []byte, s Spec) ([]edit, error) {
	re, err := compilePattern(s.Pattern)
	if err != nil {
		return nil, fmt.Errorf("version file %s: pattern: %w", s.Path, err)
	}
	g := re.SubexpIndex("version")
	var edits []edit
	for _, m := range re.FindAllSubmatchIndex(body, -1) {
		if m[2*g] >= 0 {
			edits = append(edits, edit{m[2*g], m[2*g+1], ""})
		}
	}
	if len(edits) == 0 {
		return nil, fmt.Errorf("version file %s: pattern %q has no match", s.Path, s.Pattern)
	}
	return edits, nil
}

// parseKey splits a key into its path segments: slash-separated for xml,
// otherwise dot-separated where a segment may be bracket-quoted (["a.b"],
// [""]). A properties key is one segment, the dotted name itself.
func parseKey(format, key string) ([]string, error) {
	if format == FormatXML {
		segs := strings.Split(key, "/")
		if slices.Contains(segs, "") {
			return nil, errors.New("an xml key is a slash path of element names, without empty segments")
		}
		return segs, nil
	}
	var segs []string
	for i := 0; i < len(key); {
		switch {
		case strings.HasPrefix(key[i:], `["`):
			end := strings.Index(key[i+2:], `"]`)
			if end < 0 {
				return nil, errors.New(`unterminated ["...`)
			}
			segs = append(segs, key[i+2:i+2+end])
			i += 2 + end + 2
		default:
			j := i
			for j < len(key) && key[j] != '.' && key[j] != '[' {
				j++
			}
			if j == i {
				return nil, errors.New("empty key segment")
			}
			segs = append(segs, key[i:j])
			i = j
		}
		if i < len(key) {
			switch key[i] {
			case '.':
				i++
				if i == len(key) {
					return nil, errors.New("empty key segment")
				}
			case '[':
			default:
				return nil, fmt.Errorf("unexpected %q after a bracket segment", key[i])
			}
		}
	}
	if len(segs) == 0 {
		return nil, errors.New("empty key")
	}
	if format == FormatProperties {
		return []string{strings.Join(segs, ".")}, nil
	}
	return segs, nil
}
