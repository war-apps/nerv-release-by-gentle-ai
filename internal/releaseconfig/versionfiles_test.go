package releaseconfig_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versionfile"
)

// A plain string entry is a version file whose format is inferred, as an
// MSBuild project file was before every format was accepted.
func TestLoad_VersionFileStringEntries(t *testing.T) {
	rel := mustLoad(t, project(`      version_files: ['src\Api\Api.csproj', web/package.json, VERSION]
`))
	want := []releaseconfig.VersionFile{{Path: "src/Api/Api.csproj"}, {Path: "web/package.json"}, {Path: "VERSION"}}
	if got := rel.Projects[0].VersionFiles; !reflect.DeepEqual(got, want) {
		t.Errorf("version_files = %+v, want %+v", got, want)
	}
}

func TestLoad_VersionFileMappingEntries(t *testing.T) {
	rel := mustLoad(t, project(`      version_files:
        - { path: 'charts\app\Chart.yaml', key: appVersion }
        - { path: app.json, format: json, keys: [version, meta.version] }
        - { path: src/version.go, format: regex, pattern: 'const Version = "(?P<version>[^"]+)"' }
        - { path: pyproject.toml, style: pep440 }
        - { command: 'npm version {version} --no-git-tag-version --allow-same-version', files: [package.json, 'web\package-lock.json'], style: pep440 }
`))
	want := []releaseconfig.VersionFile{
		{Path: "charts/app/Chart.yaml", Key: "appVersion"},
		{Path: "app.json", Format: "json", Keys: []string{"version", "meta.version"}},
		{Path: "src/version.go", Format: "regex", Pattern: `const Version = "(?P<version>[^"]+)"`},
		{Path: "pyproject.toml", Style: "pep440"},
		{Command: "npm version {version} --no-git-tag-version --allow-same-version", Files: []string{"package.json", "web/package-lock.json"}, Style: "pep440"},
	}
	got := rel.Projects[0].VersionFiles
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("version_files =\n %+v\nwant\n %+v", got, want)
	}
	// Spec is the entry as versionfile takes it: key is one key.
	if s := got[0].Spec(); !reflect.DeepEqual(s, versionfile.Spec{Path: "charts/app/Chart.yaml", Keys: []string{"appVersion"}}) {
		t.Errorf("Spec() = %+v", s)
	}
	if s := got[1].Spec(); !reflect.DeepEqual(s.Keys, []string{"version", "meta.version"}) || s.Format != "json" {
		t.Errorf("Spec() = %+v", s)
	}
}

func TestLoad_VersionFileErrors(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"path and command", "      version_files: [{ path: VERSION, command: 'echo x', files: [VERSION] }]\n",
			"release.projects[0].version_files[0]: set path or command, not both"},
		{"neither path nor command", "      version_files: [{ key: version }]\n",
			"release.projects[0].version_files[0]: set path (a version file) or command (a command and the files it changes)"},
		{"empty string", "      version_files: ['']\n",
			"release.projects[0].version_files[0]: set path (a version file) or command (a command and the files it changes)"},
		{"command without files", "      version_files: [{ command: 'npm version {version}' }]\n",
			"release.projects[0].version_files[0].files: required with command (the files the command changes)"},
		{"command with a path key", "      version_files: [{ command: 'x', files: [a.json], keys: [version] }]\n",
			"release.projects[0].version_files[0]: command takes only files and style (not key, keys, pattern or format)"},
		{"command with an unknown style", "      version_files: [{ command: 'x', files: [a.json], style: maven }]\n",
			`release.projects[0].version_files[0].style: unknown style "maven" (only "pep440")`},
		{"command file escaping", "      version_files: [{ command: 'x', files: [a.json, '..\\b.json'] }]\n",
			`release.projects[0].version_files[0].files[1]: "..\\b.json" escapes the repository root`},
		{"command file absolute", "      version_files: [{ command: 'x', files: [/etc/passwd] }]\n",
			`release.projects[0].version_files[0].files[0]: "/etc/passwd" must be relative to the repository root`},
		{"key with keys", "      version_files: [{ path: package.json, key: version, keys: [version] }]\n",
			"release.projects[0].version_files[0]: set key or keys, not both"},
		{"unknown key", "      version_files: [{ path: package.json, kye: version }]\n",
			"release.projects[0].version_files[0].kye: unknown field"},
		{"rejected by versionfile", "      version_files: [{ path: VERSION, keys: [version] }]\n",
			"release.projects[0].version_files[0]: version file VERSION: format text takes no keys"},
		{"unknown file without format", "      version_files: [app/meta.cfg]\n",
			"release.projects[0].version_files[0]: version file app/meta.cfg: cannot infer its format; set format"},
		{"backslash escaping path", "      version_files: ['..\\x.json']\n",
			`release.projects[0].version_files[0]: "..\\x.json" escapes the repository root`},
		{"not a string or mapping", "      version_files: [[a]]\n",
			"a version_files entry is a path or a mapping {path, key, keys, pattern, format, style} or {command, files, style}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := releaseconfig.Load(writeConfig(t, project(tt.yaml)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
