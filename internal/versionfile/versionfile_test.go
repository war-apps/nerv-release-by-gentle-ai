package versionfile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/versionfile"
)

// apply resolves s and applies version, failing the test on any error.
func apply(t *testing.T, s versionfile.Spec, content, version string) string {
	t.Helper()
	r, err := versionfile.Resolve(s)
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", s, err)
	}
	out, err := versionfile.Apply([]byte(content), r, version)
	if err != nil {
		t.Fatalf("Apply(%s): %v", s.Path, err)
	}
	return string(out)
}

// applyErr resolves s, applies version and returns the Apply error.
func applyErr(t *testing.T, s versionfile.Spec, content, version string) error {
	t.Helper()
	r, err := versionfile.Resolve(s)
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", s, err)
	}
	out, err := versionfile.Apply([]byte(content), r, version)
	if err == nil {
		t.Fatalf("Apply(%s) = %q, want an error", s.Path, out)
	}
	return err
}

func wantErrContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not contain %q", err, p)
		}
	}
}

func TestResolveInfersFormatAndDefaultKeys(t *testing.T) {
	tests := []struct {
		path   string
		format string
		keys   []string
	}{
		{"package.json", versionfile.FormatJSON, []string{"version"}},
		{"web/package-lock.json", versionfile.FormatJSON, []string{"version", `packages[""].version`}},
		{"composer.json", versionfile.FormatJSON, []string{"version"}},
		{"crates/x/Cargo.toml", versionfile.FormatTOML, []string{"package.version"}},
		{"pyproject.toml", versionfile.FormatTOML, []string{"project.version", "tool.poetry.version"}},
		{"charts/app/Chart.yaml", versionfile.FormatYAML, []string{"version", "appVersion"}},
		{"pubspec.yaml", versionfile.FormatYAML, []string{"version"}},
		{"pom.xml", versionfile.FormatXML, []string{"project/version"}},
		{"gradle.properties", versionfile.FormatProperties, []string{"version"}},
		{"VERSION", versionfile.FormatText, nil},
		{"sub/version.txt", versionfile.FormatText, nil},
		{"src/App/App.csproj", versionfile.FormatMSBuild, nil},
		{"Directory.Build.props", versionfile.FormatMSBuild, nil},
		{"Lib.FSPROJ", versionfile.FormatMSBuild, nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r, err := versionfile.Resolve(versionfile.Spec{Path: tt.path})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if r.Path != tt.path || r.Format != tt.format || !reflect.DeepEqual(r.Keys, tt.keys) {
				t.Errorf("Resolve = {%q %q %q}, want {%q %q %q}", r.Path, r.Format, r.Keys, tt.path, tt.format, tt.keys)
			}
		})
	}
}

func TestResolveKeepsExplicitKeysOfAKnownFile(t *testing.T) {
	r, err := versionfile.Resolve(versionfile.Spec{Path: "package.json", Keys: []string{"config.version"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Format != versionfile.FormatJSON || !reflect.DeepEqual(r.Keys, []string{"config.version"}) {
		t.Errorf("Resolve = %+v", r)
	}
}

func TestResolveIsIdempotent(t *testing.T) {
	for _, p := range []string{"pyproject.toml", "package-lock.json", "VERSION"} {
		r1, err := versionfile.Resolve(versionfile.Spec{Path: p})
		if err != nil {
			t.Fatal(err)
		}
		r2, err := versionfile.Resolve(r1)
		if err != nil {
			t.Fatalf("Resolve(Resolve(%s)): %v", p, err)
		}
		if !reflect.DeepEqual(r1, r2) {
			t.Errorf("Resolve(Resolve(%s)) = %+v, want %+v", p, r2, r1)
		}
	}
}

func TestResolveRejects(t *testing.T) {
	tests := []struct {
		name string
		spec versionfile.Spec
		want []string
	}{
		{"empty path", versionfile.Spec{}, []string{"path"}},
		{"absolute path", versionfile.Spec{Path: "/etc/VERSION"}, []string{"/etc/VERSION", "relative"}},
		{"parent path", versionfile.Spec{Path: "../VERSION"}, []string{"../VERSION"}},
		{"unknown file without format", versionfile.Spec{Path: "app/meta.cfg"}, []string{"app/meta.cfg", "format"}},
		{"unknown format", versionfile.Spec{Path: "a.ini", Format: "ini"}, []string{"ini"}},
		{"explicit json without keys", versionfile.Spec{Path: "app.json", Format: "json"}, []string{"app.json", "keys"}},
		{"explicit yaml without keys", versionfile.Spec{Path: "values.yaml", Format: "yaml"}, []string{"keys"}},
		{"text with keys", versionfile.Spec{Path: "VERSION", Keys: []string{"version"}}, []string{"text", "keys"}},
		{"msbuild with keys", versionfile.Spec{Path: "a.csproj", Keys: []string{"Version"}}, []string{"msbuild"}},
		{"regex without pattern", versionfile.Spec{Path: "a.go", Format: "regex"}, []string{"pattern"}},
		{"regex with keys", versionfile.Spec{Path: "a.go", Format: "regex", Pattern: `v(?P<version>\S+)`, Keys: []string{"x"}}, []string{"keys"}},
		{"regex without version group", versionfile.Spec{Path: "a.go", Format: "regex", Pattern: `const V = "(\S+)"`}, []string{"version"}},
		{"regex that does not compile", versionfile.Spec{Path: "a.go", Format: "regex", Pattern: `(?P<version>[`}, []string{"a.go", "pattern"}},
		{"pattern without regex format", versionfile.Spec{Path: "package.json", Pattern: `(?P<version>x)`}, []string{"pattern", "regex"}},
		{"unknown style", versionfile.Spec{Path: "VERSION", Style: "maven"}, []string{"maven"}},
		{"empty key segment", versionfile.Spec{Path: "package.json", Keys: []string{"a..b"}}, []string{"a..b"}},
		{"unterminated bracket key", versionfile.Spec{Path: "package.json", Keys: []string{`packages["`}}, []string{`packages["`}},
		{"empty xml segment", versionfile.Spec{Path: "pom.xml", Keys: []string{"project//version"}}, []string{"project//version"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := versionfile.Resolve(tt.spec)
			wantErrContains(t, err, tt.want...)
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct{ version, style, want string }{
		{"1.2.3-alpha.1", "", "1.2.3-alpha.1"},
		{"1.2.3", "pep440", "1.2.3"},
		{"1.2.3-alpha.4", "pep440", "1.2.3a4"},
		{"1.2.3-beta.2", "pep440", "1.2.3b2"},
		{"10.0.0-rc.12", "pep440", "10.0.0rc12"},
	}
	for _, tt := range tests {
		got, err := versionfile.Render(tt.version, tt.style)
		if err != nil || got != tt.want {
			t.Errorf("Render(%q, %q) = %q, %v; want %q", tt.version, tt.style, got, err, tt.want)
		}
	}
}

func TestRenderPep440Refuses(t *testing.T) {
	for _, v := range []string{"1.2.3-dev.1", "1.2.3-alpha", "1.2.3-rc.1.2", "1.2.3+build.5", "1.2.3-beta.1+sha", "1.2", "v1.2.3"} {
		if got, err := versionfile.Render(v, "pep440"); err == nil {
			t.Errorf("Render(%q, pep440) = %q, want an error", v, got)
		} else if !strings.Contains(err.Error(), v) {
			t.Errorf("Render(%q) error %q does not name the version", v, err)
		}
	}
	if _, err := versionfile.Render("1.2.3", "maven"); err == nil {
		t.Error("Render with an unknown style: want an error")
	}
}

func TestApplyPackageLockWritesBothKeysOnly(t *testing.T) {
	in := `{
  "name": "web",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "web",
      "version": "1.0.0",
      "dependencies": { "left-pad": "^1.3.0" }
    },
    "node_modules/left-pad": {
      "version": "1.3.0"
    }
  }
}
`
	want := strings.Replace(in, `"version": "1.0.0"`, `"version": "2.1.0-rc.1"`, 2)
	got := apply(t, versionfile.Spec{Path: "package-lock.json"}, in, "2.1.0-rc.1")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyJSONKeepsBOMAndCRLF(t *testing.T) {
	in := "\xEF\xBB\xBF{\r\n\t\"name\" : \"x\",\r\n\t\"version\" :\"0.1.0\" ,\r\n\t\"extra\": [1, {\"version\": \"9\"}]\r\n}"
	want := "\xEF\xBB\xBF{\r\n\t\"name\" : \"x\",\r\n\t\"version\" :\"0.2.0\" ,\r\n\t\"extra\": [1, {\"version\": \"9\"}]\r\n}"
	if got := apply(t, versionfile.Spec{Path: "package.json"}, in, "0.2.0"); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestApplyJSONWritesEveryDuplicateKey(t *testing.T) {
	in := `{"version":"1","version":"1"}`
	want := `{"version":"2.0.0","version":"2.0.0"}`
	if got := apply(t, versionfile.Spec{Path: "composer.json"}, in, "2.0.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyJSONQuotesANonStringScalar(t *testing.T) {
	in := `{"meta": {"version": 1}}`
	want := `{"meta": {"version": "1.0.0"}}`
	if got := apply(t, versionfile.Spec{Path: "app.json", Format: "json", Keys: []string{"meta.version"}}, in, "1.0.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyJSONBracketKeyWithDots(t *testing.T) {
	in := `{"a.b": {"version": "1"}, "a": {"b": {"version": "1"}}}`
	want := `{"a.b": {"version": "3.0.0"}, "a": {"b": {"version": "1"}}}`
	if got := apply(t, versionfile.Spec{Path: "x.json", Format: "json", Keys: []string{`["a.b"].version`}}, in, "3.0.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyPyprojectProjectVersion(t *testing.T) {
	in := `[build-system]
requires = ["hatchling"]

[project]
name = "tool"  # the name
version = "0.1.0"
dependencies = [
  "requests>=2",
  "version = '9'",
]

[tool.poetry]
version = "0.1.0"
`
	want := strings.Replace(in, `version = "0.1.0"`, `version = "0.2.0a1"`, 1)
	got := apply(t, versionfile.Spec{Path: "pyproject.toml", Style: "pep440"}, in, "0.2.0-alpha.1")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyPyprojectPoetryOnly(t *testing.T) {
	in := "[tool.poetry]\r\nname = 'tool'\r\nversion = '1.0.0' # keep\r\n"
	want := "[tool.poetry]\r\nname = 'tool'\r\nversion = '1.1.0' # keep\r\n"
	if got := apply(t, versionfile.Spec{Path: "pyproject.toml"}, in, "1.1.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyPyprojectWithoutEitherKeyFails(t *testing.T) {
	err := applyErr(t, versionfile.Spec{Path: "pyproject.toml"}, "[project]\nname = \"x\"\n", "1.0.0")
	wantErrContains(t, err, "pyproject.toml", "project.version", "tool.poetry.version")
}

func TestApplyCargoTomlLeavesDependencyVersions(t *testing.T) {
	in := `[package]
name = "app"
version = "0.3.0"
edition = "2021"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
version = "0.0.1"

[dependencies.tokio]
version = "1"

[[bin]]
name = "app"
version = "x"

[workspace.package]
version = "0.3.0"
`
	want := strings.Replace(in, `version = "0.3.0"`, `version = "0.4.0"`, 1)
	if got := apply(t, versionfile.Spec{Path: "Cargo.toml"}, in, "0.4.0"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyTOMLDottedAndQuotedKeys(t *testing.T) {
	in := "[tool]\npoetry.version = \"1\"\n\"other\".version = '1'\n"
	want := "[tool]\npoetry.version = \"2.0.0\"\n\"other\".version = '2.0.0'\n"
	s := versionfile.Spec{Path: "x.toml", Format: "toml", Keys: []string{"tool.poetry.version", "tool.other.version"}}
	if got := apply(t, s, in, "2.0.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyChartYAMLKeepsQuotedAppVersion(t *testing.T) {
	in := `---
# Helm chart
apiVersion: v2
name: app
version: 0.1.0   # chart version
appVersion: "0.1.0"
dependencies:
  - name: redis
    version: 17.0.0
annotations:
  version: keep
`
	want := `---
# Helm chart
apiVersion: v2
name: app
version: 1.2.0   # chart version
appVersion: "1.2.0"
dependencies:
  - name: redis
    version: 17.0.0
annotations:
  version: keep
`
	if got := apply(t, versionfile.Spec{Path: "Chart.yaml"}, in, "1.2.0"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyYAMLNestedSingleQuotedCRLFAndBOM(t *testing.T) {
	in := "\xEF\xBB\xBFapp:\r\n  meta:\r\n    version: '1'\r\n  version: x\r\nversion: y\r\n"
	want := "\xEF\xBB\xBFapp:\r\n  meta:\r\n    version: '3.1.4'\r\n  version: x\r\nversion: y\r\n"
	s := versionfile.Spec{Path: "values.yaml", Format: "yaml", Keys: []string{"app.meta.version"}}
	if got := apply(t, s, in, "3.1.4"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyYAMLWritesDuplicateKeys(t *testing.T) {
	in := "version: 1.0.0\nname: x\nversion: 1.0.0\n"
	want := "version: 1.0.0+5\nname: x\nversion: 1.0.0+5\n"
	if got := apply(t, versionfile.Spec{Path: "pubspec.yaml"}, in, "1.0.0+5"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyPomLeavesParentAndDependencyVersions(t *testing.T) {
	in := `<?xml version="1.0" encoding="UTF-8"?>
<!-- <version>0.0.0</version> -->
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent>
    <groupId>org.example</groupId>
    <version>5.0.0</version>
  </parent>
  <artifactId>app</artifactId>
  <version>1.0.0-SNAPSHOT</version>
  <description><![CDATA[<version>x</version>]]></description>
  <dependencies>
    <dependency>
      <version attr="a>b">2.0</version>
    </dependency>
  </dependencies>
</project>
`
	want := strings.Replace(in, "<version>1.0.0-SNAPSHOT</version>", "<version>1.1.0</version>", 1)
	if got := apply(t, versionfile.Spec{Path: "pom.xml"}, in, "1.1.0"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyGradlePropertiesKeepsComments(t *testing.T) {
	in := "# version=0.0.0\r\n! another comment\r\norg.gradle.jvmargs=-Xmx2g\r\nversion = 1.0.0\r\nversionCode: 7\r\n"
	want := "# version=0.0.0\r\n! another comment\r\norg.gradle.jvmargs=-Xmx2g\r\nversion = 1.0.1\r\nversionCode: 7\r\n"
	if got := apply(t, versionfile.Spec{Path: "gradle.properties"}, in, "1.0.1"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyPropertiesSeparators(t *testing.T) {
	in := "app.version: 1\nbuild.version 1\n"
	want := "app.version: 2.0.0\nbuild.version 2.0.0\n"
	s := versionfile.Spec{Path: "app.properties", Format: "properties", Keys: []string{"app.version", "build.version"}}
	if got := apply(t, s, in, "2.0.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyTextKeepsBOMAndTrailingNewlines(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1.0.0\n", "1.1.0\n"},
		{"\xEF\xBB\xBF1.0.0\r\n\r\n", "\xEF\xBB\xBF1.1.0\r\n\r\n"},
		{"1.0.0", "1.1.0"},
	}
	for _, tt := range tests {
		if got := apply(t, versionfile.Spec{Path: "VERSION"}, tt.in, "1.1.0"); got != tt.want {
			t.Errorf("Apply(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestApplyTextRefusesMoreThanAVersion(t *testing.T) {
	err := applyErr(t, versionfile.Spec{Path: "VERSION"}, "1.0.0\nnotes\n", "1.1.0")
	wantErrContains(t, err, "VERSION")
}

func TestApplyRegexReplacesEveryMatch(t *testing.T) {
	in := "package main\n\nconst Version = \"1.0.0\" // v\nconst Other = \"1.0.0\"\nvar alias = Version // \"1.0.0\"\n"
	want := "package main\n\nconst Version = \"1.2.0\" // v\nconst Other = \"1.0.0\"\nvar alias = Version // \"1.0.0\"\n"
	s := versionfile.Spec{Path: "version.go", Format: "regex", Pattern: `const Version = "(?P<version>[^"]+)"`}
	if got := apply(t, s, in, "1.2.0"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	two := "v=1\nv=1\n"
	if got := apply(t, versionfile.Spec{Path: "a.txt", Format: "regex", Pattern: `v=(?P<version>\d+)`}, two, "2.0.0"); got != "v=2.0.0\nv=2.0.0\n" {
		t.Errorf("got %q", got)
	}
}

func TestApplyRegexWithoutMatchFails(t *testing.T) {
	s := versionfile.Spec{Path: "version.go", Format: "regex", Pattern: `const Version = "(?P<version>[^"]+)"`}
	wantErrContains(t, applyErr(t, s, "package main\n", "1.0.0"), "version.go", "match")
}

func TestApplyMissingKeyFails(t *testing.T) {
	tests := []struct {
		spec    versionfile.Spec
		content string
		key     string
	}{
		{versionfile.Spec{Path: "package.json"}, `{"name": "x"}`, "version"},
		{versionfile.Spec{Path: "package-lock.json"}, `{"version": "1", "packages": {}}`, `packages[""].version`},
		{versionfile.Spec{Path: "Cargo.toml"}, "[workspace.package]\nversion = \"1\"\n", "package.version"},
		{versionfile.Spec{Path: "Cargo.toml"}, "[[package]]\nversion = \"1\"\n", "package.version"},
		{versionfile.Spec{Path: "Chart.yaml"}, "version: 1.0.0\n", "appVersion"},
		{versionfile.Spec{Path: "values.yaml", Format: "yaml", Keys: []string{`deps[""].version`}}, "deps:\n  - name: x\n    version: 1\n", `deps[""].version`},
		{versionfile.Spec{Path: "pom.xml"}, "<project><parent><version>1</version></parent></project>", "project/version"},
		{versionfile.Spec{Path: "gradle.properties"}, "# version=1\n", "version"},
	}
	for _, tt := range tests {
		t.Run(tt.spec.Path, func(t *testing.T) {
			wantErrContains(t, applyErr(t, tt.spec, tt.content, "2.0.0"), tt.spec.Path, tt.key, "not found")
		})
	}
}

func TestApplyNonScalarFails(t *testing.T) {
	tests := []struct {
		spec    versionfile.Spec
		content string
	}{
		{versionfile.Spec{Path: "package.json"}, `{"version": {"major": 1}}`},
		{versionfile.Spec{Path: "composer.json"}, `{"version": [1]}`},
		{versionfile.Spec{Path: "Cargo.toml"}, "[package]\nversion = { workspace = true }\n"},
		{versionfile.Spec{Path: "pubspec.yaml"}, "version:\n  major: 1\n"},
		{versionfile.Spec{Path: "pom.xml"}, "<project><version><major>1</major></version></project>"},
	}
	for _, tt := range tests {
		t.Run(tt.spec.Path, func(t *testing.T) {
			wantErrContains(t, applyErr(t, tt.spec, tt.content, "2.0.0"), tt.spec.Path, "scalar")
		})
	}
}

func TestApplyRefusesAnUnsafeVersion(t *testing.T) {
	for _, v := range []string{"", "1.0.0\"", "1.0 0", "1.0.0'", "<1>", "-1.0.0"} {
		r, _ := versionfile.Resolve(versionfile.Spec{Path: "package.json"})
		if out, err := versionfile.Apply([]byte(`{"version": "1"}`), r, v); err == nil {
			t.Errorf("Apply(%q) = %q, want an error", v, out)
		}
	}
}

func TestApplyRendersTheStyle(t *testing.T) {
	r, _ := versionfile.Resolve(versionfile.Spec{Path: "VERSION", Style: "pep440"})
	if _, err := versionfile.Apply([]byte("1.0.0\n"), r, "1.0.0+build"); err == nil {
		t.Error("pep440 with build metadata: want an error")
	}
}

func TestApplyRefusesMSBuild(t *testing.T) {
	r, err := versionfile.Resolve(versionfile.Spec{Path: "App.csproj"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = versionfile.Apply([]byte("<Project></Project>"), r, "1.0.0")
	wantErrContains(t, err, "releaseplan.WriteCsproj")
}

func TestWriteReplacesTheFileAndKeepsItsMode(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "package.json")
	if err := os.WriteFile(file, []byte("{\"version\": \"1.0.0\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := versionfile.Write(root, versionfile.Spec{Path: "web/package.json"}, "1.1.0"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"version\": \"1.1.0\"}\n" {
		t.Errorf("content = %q", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only package.json", len(entries))
	}
}

func TestWriteLeavesTheFileUnchangedOnError(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "package.json")
	const original = "{\"name\": \"x\"}\n"
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	err := versionfile.Write(root, versionfile.Spec{Path: "package.json"}, "1.1.0")
	wantErrContains(t, err, "package.json", "version")
	got, _ := os.ReadFile(file)
	if string(got) != original {
		t.Errorf("content = %q, want it unchanged", got)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only package.json", len(entries))
	}
}

func TestWriteMissingFileFails(t *testing.T) {
	err := versionfile.Write(t.TempDir(), versionfile.Spec{Path: "VERSION"}, "1.0.0")
	wantErrContains(t, err, "VERSION")
}
