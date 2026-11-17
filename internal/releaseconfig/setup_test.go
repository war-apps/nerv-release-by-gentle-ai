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
