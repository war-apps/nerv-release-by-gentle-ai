package releaseconfig_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
)

// ImportSection writes the imported keys under
// release.providers.<provider>, keeping the rest of the file (its
// comments included) and every provider key the import did not set.
func TestImportSection_WritesNewProviderBlockAndKeepsTheRest(t *testing.T) {
	data := []byte("# team settings\nenabled: true\nrelease:\n  providers:\n    conventional-commits:\n      tag_format: v${version}\n  projects:\n    - name: api\n      path: .\n      build: bash\n")
	out, notes, err := releaseconfig.ImportSection(data, "conventional-commits", "branches: [main]\nrelease_rules: [{type: docs, release: patch}]\n", false)
	if err != nil {
		t.Fatalf("ImportSection: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, want none (no conflict)", notes)
	}
	for _, want := range []string{"# team settings\nenabled: true\n", "- name: api", "tag_format: v${version}", "branches:", "release_rules:"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	rel, err := releaseconfig.Load(writeConfig(t, out))
	if err != nil {
		t.Fatalf("the written section does not load: %v\n%s", err, out)
	}
	engine := rel.Providers["conventional-commits"]
	if engine["tag_format"] != "v${version}" {
		t.Errorf("tag_format = %v", engine["tag_format"])
	}
	branches, _ := engine["branches"].([]any)
	if len(branches) != 1 || branches[0] != "main" {
		t.Errorf("branches = %v", engine["branches"])
	}
}

// Without force, an engine key the section already sets to a different
// value is never replaced silently: ImportSection refuses, naming both
// values.
func TestImportSection_ConflictingKeyRefusesWithoutForce(t *testing.T) {
	data := []byte("release:\n  providers:\n    conventional-commits:\n      tag_format: custom-${version}\n")
	_, _, err := releaseconfig.ImportSection(data, "conventional-commits", "tag_format: v${version}\n", false)
	var conflict *releaseconfig.EngineConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want *EngineConflictError", err)
	}
	if len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Key != "tag_format" ||
		conflict.Conflicts[0].Current != "custom-${version}" || conflict.Conflicts[0].Imported != "v${version}" {
		t.Errorf("conflicts = %+v", conflict.Conflicts)
	}
	if !strings.Contains(conflict.Error(), "custom-${version}") || !strings.Contains(conflict.Error(), "v${version}") {
		t.Errorf("message %q lacks both values", conflict.Error())
	}
}

// force replaces a conflicting key in place, keeping its comment, and
// notes the replacement; a key the import does not set is left alone.
func TestImportSection_ForceReplacesAndKeepsComments(t *testing.T) {
	data := []byte("release:\n  providers:\n    conventional-commits:\n      # hand-tuned\n      tag_format: custom-${version}\n      branches: [main]\n")
	out, notes, err := releaseconfig.ImportSection(data, "conventional-commits", "tag_format: v${version}\n", true)
	if err != nil {
		t.Fatalf("ImportSection: %v", err)
	}
	if !strings.Contains(out, "# hand-tuned") || !strings.Contains(out, "tag_format: v${version}") || strings.Contains(out, "custom-${version}") {
		t.Errorf("result:\n%s", out)
	}
	if !strings.Contains(out, "branches: [main]") && !strings.Contains(out, "branches:\n") {
		t.Errorf("an untouched key was dropped:\n%s", out)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "custom-${version}") || !strings.Contains(notes[0], "v${version}") {
		t.Errorf("notes = %v, want one naming both values", notes)
	}
}

// An equal value is no conflict, even without force.
func TestImportSection_EqualValueIsNoConflict(t *testing.T) {
	data := []byte("release:\n  providers:\n    conventional-commits:\n      tag_format: v${version}\n")
	_, notes, err := releaseconfig.ImportSection(data, "conventional-commits", "tag_format: v${version}\n", false)
	if err != nil {
		t.Fatalf("ImportSection: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, want none", notes)
	}
}

// Without an existing release: section, ImportSection creates one.
func TestImportSection_CreatesTheSectionWhenAbsent(t *testing.T) {
	data := []byte("enabled: true\n")
	out, _, err := releaseconfig.ImportSection(data, "conventional-commits", "tag_format: v${version}\n", false)
	if err != nil {
		t.Fatalf("ImportSection: %v", err)
	}
	if !strings.Contains(out, "enabled: true") || !strings.Contains(out, "release:") || !strings.Contains(out, "tag_format: v${version}") {
		t.Errorf("result:\n%s", out)
	}
}

// R-S7: the imported version files go to the one project's version_files:
// appended after its entries (string or mapping), each once, comments and
// the rest of the file kept.
func TestImportVersionFiles_OneProjectAppendsOnceAndKeepsTheRest(t *testing.T) {
	data := []byte("# team settings\nenabled: true\nrelease:\n  projects:\n    - name: api\n      path: .\n      build: bash\n      version_files:\n        - src/Api.csproj # the assembly version\n        - {path: package.json, key: version}\ndeploy:\n  host: github\n")
	out, notes, warnings, err := releaseconfig.ImportVersionFiles(data, []string{"package.json", "package-lock.json", "src/Api.csproj"})
	if err != nil {
		t.Fatalf("ImportVersionFiles: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "package-lock.json") || strings.Contains(notes[0], "src/Api.csproj") || !strings.Contains(notes[0], "release.projects[0].version_files") {
		t.Errorf("notes = %v, want one naming only package-lock.json and where", notes)
	}
	for _, want := range []string{"# team settings\nenabled: true\n", "deploy:\n  host: github\n", "# the assembly version"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	rel, err := releaseconfig.Load(writeConfig(t, out))
	if err != nil {
		t.Fatalf("the result does not load: %v\n%s", err, out)
	}
	var paths []string
	for _, f := range rel.Projects[0].VersionFiles {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "src/Api.csproj,package.json,package-lock.json" {
		t.Errorf("version_files = %v", paths)
	}
}

// R-S7: a project without version_files gets the key.
func TestImportVersionFiles_CreatesTheKey(t *testing.T) {
	data := []byte("release:\n  projects:\n    - name: api\n      path: .\n")
	out, _, _, err := releaseconfig.ImportVersionFiles(data, []string{"package.json"})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := releaseconfig.Load(writeConfig(t, out))
	if err != nil {
		t.Fatalf("the result does not load: %v\n%s", err, out)
	}
	if fs := rel.Projects[0].VersionFiles; len(fs) != 1 || fs[0].Path != "package.json" {
		t.Errorf("version_files = %+v\n%s", fs, out)
	}
}

// R-S7: with no project or several, no project is chosen or invented: the
// file is unchanged and one warning lists the files.
func TestImportVersionFiles_NoOrSeveralProjectsWarnAndChangeNothing(t *testing.T) {
	for _, data := range []string{
		"release:\n  providers: {}\n",
		"",
		"release:\n  projects:\n    - name: api\n      path: api\n    - name: web\n      path: web\n",
	} {
		out, notes, warnings, err := releaseconfig.ImportVersionFiles([]byte(data), []string{"package.json", "pom.xml"})
		if err != nil {
			t.Fatalf("%q: %v", data, err)
		}
		if out != data || len(notes) != 0 {
			t.Errorf("%q: changed to %q (notes %v)", data, out, notes)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "package.json, pom.xml") || !strings.Contains(warnings[0], "version_files") {
			t.Errorf("%q: warnings = %v", data, warnings)
		}
	}
}

// Nothing to import changes nothing and says nothing.
func TestImportVersionFiles_NoFiles(t *testing.T) {
	data := "release:\n  projects:\n    - name: api\n      path: .\n"
	out, notes, warnings, err := releaseconfig.ImportVersionFiles([]byte(data), nil)
	if err != nil || out != data || len(notes)+len(warnings) != 0 {
		t.Errorf("out %q notes %v warnings %v err %v", out, notes, warnings, err)
	}
}
