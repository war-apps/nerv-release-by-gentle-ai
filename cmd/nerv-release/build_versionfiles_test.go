package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseplan"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaserun"
)

// cloneOnly is a release workspace that only has a directory and a public
// address: what writeReleaseFiles reads from it.
type cloneOnly struct {
	releaseWorkspace
	dir, public string
}

func (c cloneOnly) Dir() string       { return c.dir }
func (c cloneOnly) PublicURL() string { return c.public }

// fakeSteps records the steps it is asked to run and runs fn for each.
type fakeSteps struct {
	steps []releaserun.Step
	fn    func(releaserun.Step) error
}

func (f *fakeSteps) Run(_ context.Context, step releaserun.Step, _ releaserun.Vars) error {
	f.steps = append(f.steps, step)
	if f.fn != nil {
		return f.fn(step)
	}
	return nil
}

func versionFilesRun(t *testing.T, steps *fakeSteps) (*releaseRun, string) {
	t.Helper()
	dir := t.TempDir()
	return &releaseRun{
		stderr: &bytes.Buffer{},
		ws:     cloneOnly{dir: dir},
		exec:   releaseplan.Executor{Steps: steps},
	}, dir
}

func versionFilesItem(files ...releaseconfig.VersionFile) releaseItem {
	return releaseItem{
		version: releaseplan.Version{Version: "1.1.0", Tag: "v1.1.0"},
		project: releaseconfig.Project{Name: "widgets", Path: ".", VersionFiles: files},
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// A committed symlink must not let the release clone write a .csproj
// outside it: the MSBuild write is refused and the target is unchanged.
func TestWriteReleaseFiles_RefusesASymlinkedCsproj(t *testing.T) {
	s, dir := versionFilesRun(t, &fakeSteps{})
	outside := filepath.Join(t.TempDir(), "Outside.csproj")
	const original = "<Project><PropertyGroup><Version>1.0.0</Version></PropertyGroup></Project>\n"
	if err := os.WriteFile(outside, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(dir, "App.csproj"))
	_, err := s.writeReleaseFiles(context.Background(), versionFilesItem(releaseconfig.VersionFile{Path: "App.csproj"}), releaserun.Vars{})
	if err == nil || !strings.Contains(err.Error(), "App.csproj") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want a symlink refusal naming App.csproj", err)
	}
	if got, _ := os.ReadFile(outside); string(got) != original {
		t.Errorf("outside csproj = %q, want it unchanged", got)
	}
}

// A committed CHANGELOG symlink must not let the release clone prepend the
// section to a file outside it.
func TestWriteReleaseFiles_RefusesASymlinkedChangelog(t *testing.T) {
	s, dir := versionFilesRun(t, &fakeSteps{})
	outside := filepath.Join(t.TempDir(), "notes.md")
	const original = "# Outside\n"
	if err := os.WriteFile(outside, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(dir, "CHANGELOG.md"))
	it := versionFilesItem()
	it.version.Changelog, it.version.ChangelogSection = "CHANGELOG.md", "## 1.1.0\n\n* gadgets\n"
	_, err := s.writeReleaseFiles(context.Background(), it, releaserun.Vars{})
	if err == nil || !strings.Contains(err.Error(), "CHANGELOG.md") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want a symlink refusal naming CHANGELOG.md", err)
	}
	if got, _ := os.ReadFile(outside); string(got) != original {
		t.Errorf("outside file = %q, want it unchanged", got)
	}
}

// A command cannot capture a file outside the clone into the artifact
// through a symlink it names in files.
func TestWriteReleaseFiles_RefusesCapturingASymlinkedCommandFile(t *testing.T) {
	steps := &fakeSteps{}
	s, dir := versionFilesRun(t, steps)
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, secret, filepath.Join(dir, "lock.json"))
	files, err := s.writeReleaseFiles(context.Background(), versionFilesItem(releaseconfig.VersionFile{Command: "true", Files: []string{"lock.json"}}), releaserun.Vars{})
	if err == nil || !strings.Contains(err.Error(), "lock.json") || !strings.Contains(err.Error(), "symlink") || !strings.Contains(err.Error(), "version_files[0]") {
		t.Fatalf("error = %v (files %v), want a symlink refusal naming lock.json and the entry", err, files)
	}
}

// A command entry runs through the step runner the stages use, with
// {version} rendered in the entry's style, in the default shell.
func TestWriteReleaseFiles_RunsTheCommandThroughTheStepRunner(t *testing.T) {
	steps := &fakeSteps{}
	s, dir := versionFilesRun(t, steps)
	steps.fn = func(releaserun.Step) error {
		return os.WriteFile(filepath.Join(dir, "VERSION.txt"), []byte("x\n"), 0o644)
	}
	it := versionFilesItem(releaseconfig.VersionFile{Command: "bump {version} --to {version}", Files: []string{"VERSION.txt"}, Style: "pep440"})
	it.version.Version = "1.1.0-rc.2"
	files, err := s.writeReleaseFiles(context.Background(), it, releaserun.Vars{})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps.steps) != 1 {
		t.Fatalf("steps = %+v", steps.steps)
	}
	got := steps.steps[0]
	if got.Name != "version_files[0]" || got.Command.Run != "bump 1.1.0rc2 --to 1.1.0rc2" || got.Command.Shell != releaseconfig.DefaultShell {
		t.Errorf("step = %+v", got)
	}
	if len(files) != 1 || files[0].Path != "VERSION.txt" || string(files[0].Content) != "x\n" {
		t.Errorf("files = %+v", files)
	}
}

// A file a command entry names must exist after the command ran.
func TestWriteReleaseFiles_RefusesAMissingCommandFile(t *testing.T) {
	s, _ := versionFilesRun(t, &fakeSteps{})
	_, err := s.writeReleaseFiles(context.Background(), versionFilesItem(releaseconfig.VersionFile{Command: "true", Files: []string{"VERSION.txt"}}), releaserun.Vars{})
	if err == nil || !strings.Contains(err.Error(), "project widgets") || !strings.Contains(err.Error(), "version_files[0]") || !strings.Contains(err.Error(), "VERSION.txt") {
		t.Fatalf("error = %v, want it to name the project, the entry and the file", err)
	}
}

// Two entries naming the same file capture it once, with its final content.
func TestWriteReleaseFiles_CapturesAFileOnceWithItsFinalContent(t *testing.T) {
	steps := &fakeSteps{}
	s, dir := versionFilesRun(t, steps)
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps.fn = func(releaserun.Step) error {
		return os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.1.0-final\n"), 0o644)
	}
	files, err := s.writeReleaseFiles(context.Background(), versionFilesItem(
		releaseconfig.VersionFile{Path: "VERSION"},
		releaseconfig.VersionFile{Command: "true", Files: []string{"VERSION"}},
	), releaserun.Vars{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "VERSION" || string(files[0].Content) != "1.1.0-final\n" {
		t.Errorf("files = %+v, want VERSION once with its final content", files)
	}
}

// MSBuild entries keep their behaviour: with a public address the release
// link becomes <PackageReleaseNotes>; without one only <Version> changes.
func TestWriteReleaseFiles_MSBuildWithAndWithoutTheReleaseLink(t *testing.T) {
	const original = "<Project>\n  <PropertyGroup>\n    <Version>1.0.0</Version>\n    <PackageReleaseNotes>old</PackageReleaseNotes>\n  </PropertyGroup>\n</Project>\n"
	tests := []struct {
		name, public, wantNotes string
		warns                   bool
	}{
		{"without a public address", "", "<PackageReleaseNotes>old</PackageReleaseNotes>", true},
		{"with a public address", "https://github.com/acme/widgets", "<PackageReleaseNotes>https://github.com/acme/widgets/releases/tag/v1.1.0</PackageReleaseNotes>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, dir := versionFilesRun(t, &fakeSteps{})
			s.ws = cloneOnly{dir: dir, public: tt.public}
			if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "src", "App.csproj"), []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			files, err := s.writeReleaseFiles(context.Background(), versionFilesItem(releaseconfig.VersionFile{Path: "src/App.csproj"}), releaserun.Vars{})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[0].Path != "src/App.csproj" {
				t.Fatalf("files = %+v", files)
			}
			got := string(files[0].Content)
			if !strings.Contains(got, "<Version>1.1.0</Version>") || !strings.Contains(got, tt.wantNotes) {
				t.Errorf("csproj:\n%s", got)
			}
			if warned := strings.Contains(s.stderr.(*bytes.Buffer).String(), "no public address"); warned != tt.warns {
				t.Errorf("warning printed = %v, want %v", warned, tt.warns)
			}
		})
	}
}
