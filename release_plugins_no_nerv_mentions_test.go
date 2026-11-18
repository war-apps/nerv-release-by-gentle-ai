package nervrelease

import (
	"io/fs"
	"regexp"
	"testing"
)

// strayNervToken matches what R-S3 says every plugin file must have left
// behind: the literal step placeholder {nerv} (now {nerv-release}), and a
// bare "nerv release", "nerv deploy", "nerv version", "nerv configure" or
// "nerv install" CLI invocation (now "nerv-release build",
// "nerv-release deploy", "nerv-release version next",
// "nerv-release configure" and "nerv-release install"). It never matches
// "nerv-release" itself (a hyphen, not a space, follows "nerv"), nor
// "nerv migrate", "nerv pipeline" or "nerv's" (the source nerv tool, named
// only in ported history and comments).
var strayNervToken = regexp.MustCompile(`\{nerv\}|\bnerv (?:release|deploy|version|configure|install)\b`)

// No file of the release plugin or its 6 providers names the old {nerv}
// step placeholder or a bare "nerv release"/"nerv deploy"/"nerv version"/
// "nerv configure"/"nerv install" invocation: R-S3 moved every one of them
// to {nerv-release}, "nerv-release build", "nerv-release deploy",
// "nerv-release version next", "nerv-release configure" and
// "nerv-release install".
func TestReleasePluginAndProviders_NameNoOldNervCommand(t *testing.T) {
	check := func(label string, fsys fs.FS) {
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			if loc := strayNervToken.FindIndex(data); loc != nil {
				t.Errorf("%s/%s still names the old nerv command: %q", label, path, data[loc[0]:loc[1]])
			}
			return nil
		})
		if err != nil {
			t.Errorf("walking %s: %v", label, err)
		}
	}

	check("release (core)", CoreFS())
	for _, root := range []string{
		"release-bash", "release-conventional-commits", "release-docker-compose",
		"release-github", "release-script", "release-ssh-compose",
	} {
		sub, err := fs.Sub(ExtensionsFS(), root)
		if err != nil {
			t.Fatalf("fs.Sub(%s): %v", root, err)
		}
		check(root, sub)
	}
}
