package releaseplan

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	nerv "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/provider"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
)

// recordingRunner records every provider step it is asked to run, with
// its request, and answers them with answer (ok with no data when nil).
type recordingRunner struct {
	argv     [][]string
	requests []provider.Request
	answer   func(req provider.Request) provider.Response
}

func (r *recordingRunner) Run(_ context.Context, c proc.Command) (proc.Result, error) {
	r.argv = append(r.argv, append([]string{c.Name}, c.Args...))
	var req provider.Request
	if err := json.Unmarshal(c.Stdin, &req); err != nil {
		return proc.Result{}, err
	}
	r.requests = append(r.requests, req)
	resp := provider.Response{Status: provider.StatusOK}
	if r.answer != nil {
		resp = r.answer(req)
	}
	out, _ := json.Marshal(resp)
	return proc.Result{Stdout: out}, nil
}

func testProviders(r proc.Runner) Providers {
	return Providers{FS: nerv.ExtensionsFS(), Runner: r, Nerv: "/opt/nerv"}
}

func invalidProblems(t *testing.T, err error) string {
	t.Helper()
	var invalid *releaseconfig.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *releaseconfig.ValidationError", err)
	}
	return strings.Join(invalid.Problems, "\n")
}

// S4, S6: the version engine is the version slot's provider, called with
// the common envelope: release.version names it (conventional-commits by
// default), its keys are release.providers.<provider>, the payload carries
// the repository, the branch and the projects.
func TestVersionEngine_CallsTheSelectedProviderWithTheEnvelope(t *testing.T) {
	r := &recordingRunner{answer: func(provider.Request) provider.Response {
		data, _ := json.Marshal(NextData{Results: nil})
		return provider.Response{Status: provider.StatusOK, Data: data}
	}}
	rel := &releaseconfig.Release{
		Providers: map[string]map[string]any{"conventional-commits": {"branches": []any{"main"}}},
		Projects:  []releaseconfig.Project{{Name: "api", Path: "api", Scopes: []string{"api"}}},
	}
	engine, err := NewVersionEngine(testProviders(r), rel)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Name() != "conventional-commits" {
		t.Errorf("engine %q, want the slot's default conventional-commits", engine.Name())
	}
	if _, err := engine.Next(context.Background(), "/clone", "develop", []string{"api"}, "1.2.0"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/opt/nerv", "provider", "release-conventional-commits", "next"}; len(r.argv) != 1 || !slices.Equal(r.argv[0], want) {
		t.Fatalf("argv = %q, want %q", r.argv, want)
	}
	req := r.requests[0]
	if req.Contract != provider.ContractVersion || req.Extension != "release" || req.Slot != "version" || req.Provider != "conventional-commits" || req.Operation != "next" {
		t.Errorf("request = %+v", req)
	}
	if got, ok := req.Config["branches"].([]any); !ok || len(got) != 1 || got[0] != "main" {
		t.Errorf("config = %v, want release.providers.conventional-commits", req.Config)
	}
	if _, set := req.Config["tag_format"]; !set {
		t.Errorf("config = %v, want the declared default tag_format", req.Config)
	}
	var payload VersionPayload
	if err := req.DecodePayload(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Dir != "/clone" || payload.Branch != "develop" || !slices.Equal(payload.Only, []string{"api"}) || payload.Version != "1.2.0" ||
		len(payload.Projects) != 1 || payload.Projects[0].Name != "api" || payload.Projects[0].Path != "api" {
		t.Errorf("payload = %+v", payload)
	}
}

// S4: an unknown provider, or one of the other slot, is refused naming the
// key that selected it and the slot's providers.
//
// NewBuildProvider's half of this nerv test (an unknown or missing build
// provider) is deferred to U4 with BuildProvider itself.
func TestProviders_UnknownProviderIsRefused(t *testing.T) {
	p := testProviders(&recordingRunner{})
	_, err := NewVersionEngine(p, &releaseconfig.Release{Version: "semver"})
	if got := invalidProblems(t, err); got != `release.version: unknown version provider "semver" (want conventional-commits)` {
		t.Errorf("problems = %s", got)
	}
}

// S7: a provider's keys are the ones its provider.yaml declares, of their
// type; a release.providers entry names a shipped provider (now that
// NewBuildProvider ships the build providers too, U4's shipped list
// includes bash and docker-compose, not only the version engine).
func TestProviders_KeysAreCheckedAgainstTheProvider(t *testing.T) {
	p := testProviders(&recordingRunner{})
	rel := &releaseconfig.Release{
		Build: "bash",
		Providers: map[string]map[string]any{
			"bash":                 {"shared_host": "yes", "sdk": "8"},
			"conventional-commits": {"branches": "main"},
			"maven":                {},
		},
		Projects: []releaseconfig.Project{{Name: "api", Path: ".", Providers: map[string]map[string]any{"bash": {"shell": "pwsh"}}}},
	}
	_, err := NewVersionEngine(p, rel)
	got := invalidProblems(t, err)
	for _, want := range []string{
		"release.providers.maven: unknown release provider (want bash, conventional-commits, docker-compose)",
		"release.providers.conventional-commits.branches: must be a list",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("version engine problems lack %q:\n%s", want, got)
		}
	}
}

func nativeCall(t *testing.T, plugin, op string, config map[string]any, payload any) provider.Response {
	t.Helper()
	req := provider.Request{Contract: provider.ContractVersion, Extension: Extension, Operation: op, Config: config, Payload: payload}
	data, _ := json.Marshal(req)
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatal(err)
	}
	if plugin != ConventionalCommits {
		t.Fatalf("native plugin %q is not implemented yet (U4)", plugin)
	}
	return conventionalCommits(context.Background(), op, req, NativeDeps{})
}

func problemsOf(t *testing.T, resp provider.Response) string {
	t.Helper()
	var data ProblemsData
	if err := resp.DecodeData(&data); err != nil || resp.Status != provider.StatusFailed {
		t.Fatalf("response %+v, want failed with problems", resp)
	}
	return strings.Join(data.Problems, "\n")
}

// The conventional-commits engine checks its keys and the projects, each
// problem named by its path in .ai-workflow/release.yaml.
func TestConventionalCommits_ValidateNamesTheKeys(t *testing.T) {
	got := problemsOf(t, nativeCall(t, ConventionalCommits, OpValidate,
		map[string]any{"release_rules": []any{map[string]any{"type": "docs", "release": "huge"}}, "tag_format": "v1"},
		VersionPayload{Projects: []VersionProject{{Name: "a", Path: "a", TagFormat: "x-${version}"}, {Name: "b", Path: "b", TagFormat: "x-${version}"}}}))
	for _, want := range []string{
		`release.providers.conventional-commits.release_rules[0]: release "huge" is not major, minor, patch or none`,
		`release.providers.conventional-commits.tag_format: "v1" must contain ${version} exactly once`,
		`release.projects[1].tag_format: "x-${version}" is also the tag format of project "a"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
	if resp := nativeCall(t, ConventionalCommits, OpValidate, map[string]any{"tag_format": "v${version}"}, VersionPayload{}); resp.Failure() {
		t.Errorf("valid config: %+v", resp)
	}
}

// The engine's defaults give each project its tag format and CHANGELOG:
// its own, or the engine's tag_format (prefixed with the project's name in
// a monorepo) and <path>/<changelog.file>.
func TestConventionalCommits_ProjectDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		projects []VersionProject
		tag      map[string]string
		log      map[string]string
	}{
		{"single project", nil, []VersionProject{{Name: "api", Path: "."}}, map[string]string{"api": "v${version}"}, map[string]string{"api": "CHANGELOG.md"}},
		{"explicit", nil, []VersionProject{{Name: "api", Path: "src/Api", TagFormat: "api@${version}", Changelog: "src/Api/CHANGES.md"}},
			map[string]string{"api": "api@${version}"}, map[string]string{"api": "src/Api/CHANGES.md"}},
		{"monorepo and engine keys", map[string]any{"tag_format": "r${version}", "changelog": map[string]any{"file": "HISTORY.md"}},
			[]VersionProject{{Name: "api", Path: "api"}, {Name: "web", Path: "web", Changelog: `docs\web.md`}},
			map[string]string{"api": "api-r${version}", "web": "web-r${version}"}, map[string]string{"api": "api/HISTORY.md", "web": "docs/web.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, problems := engineConfig(provider.Request{Config: tt.config}, tt.projects)
			if len(problems) > 0 {
				t.Fatal(problems)
			}
			for _, p := range cfg.Projects {
				if p.TagFormat != tt.tag[p.Name] || p.Changelog != tt.log[p.Name] {
					t.Errorf("%s: tag %q changelog %q, want %q %q", p.Name, p.TagFormat, p.Changelog, tt.tag[p.Name], tt.log[p.Name])
				}
			}
		})
	}
}
