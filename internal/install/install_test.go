package install_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/runner/runnertest"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/install"
)

func fakeCoreFS() fstest.MapFS {
	return fstest.MapFS{
		".claude-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"release","version":"1.0.0"}`)},
		"commands/build.md":          &fstest.MapFile{Data: []byte("x")},
	}
}

// fakeExtensionsFS mirrors ProvidersFS's shape: 2 provider plugins, no
// "release" folder of its own.
func fakeExtensionsFS() fstest.MapFS {
	return fstest.MapFS{
		"release-bash/.claude-plugin/plugin.json":   &fstest.MapFile{Data: []byte(`{"name":"release-bash","version":"1.0.0"}`)},
		"release-bash/provider.yaml":                &fstest.MapFile{Data: []byte("x")},
		"release-github/.claude-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"release-github","version":"1.0.0"}`)},
		"release-github/provider.yaml":              &fstest.MapFile{Data: []byte("x")},
	}
}

func newDeps(t *testing.T) (install.Deps, string) {
	t.Helper()
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	marketplaceDir := filepath.Join(home, ".nerv-release", "marketplace")
	return install.Deps{
		Home:           home,
		SettingsPath:   settingsPath,
		MarketplaceDir: marketplaceDir,
		CoreFS:         fakeCoreFS(),
		ExtensionsFS:   fakeExtensionsFS(),
		Runner:         &runnertest.FakeRunner{},
		Now:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Stdout:         os.Stdout,
	}, settingsPath
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing settings: %v", err)
	}
	return m
}

func writeSettings(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInstalledPlugins(t *testing.T, home string, plugins map[string]string) {
	t.Helper()
	type entry struct {
		InstallPath string `json:"installPath"`
		Version     string `json:"version"`
	}
	doc := struct {
		Plugins map[string][]entry `json:"plugins"`
	}{Plugins: map[string][]entry{}}
	for id, path := range plugins {
		doc.Plugins[id] = []entry{{InstallPath: path, Version: "1.0.0"}}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstall_MaterializesAndRegistersCorePlugin(t *testing.T) {
	deps, settingsPath := newDeps(t)

	if _, err := install.Install(deps); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(deps.MarketplaceDir, "plugin", ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("core plugin was not materialized: %v", err)
	}

	m := readSettings(t, settingsPath)
	enabled, _ := m["enabledPlugins"].(map[string]any)
	if enabled == nil || enabled["release@nerv-release"] != true {
		t.Errorf("enabledPlugins = %v, want release@nerv-release enabled", enabled)
	}
	marketplaces, _ := m["extraKnownMarketplaces"].(map[string]any)
	if marketplaces == nil {
		t.Fatal("extraKnownMarketplaces missing")
	}
	if _, ok := marketplaces["nerv-release"]; !ok {
		t.Error("extraKnownMarketplaces does not register nerv-release")
	}
}

func TestInstall_MaterializedMarketplaceListsCorePluginAsRelease(t *testing.T) {
	deps, _ := newDeps(t)

	if _, err := install.Install(deps); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(deps.MarketplaceDir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatalf("reading marketplace.json: %v", err)
	}
	var manifest struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parsing marketplace.json: %v", err)
	}
	if len(manifest.Plugins) == 0 {
		t.Fatal("marketplace.json lists no plugins")
	}
	if manifest.Plugins[0].Name != "release" {
		t.Errorf("marketplace.json core plugin name = %q, want %q", manifest.Plugins[0].Name, "release")
	}
}

// TestInstall_FreshMachine_InstallsEveryBundledProvider is R-S6: on a
// fresh machine, with no provider enabled anywhere (neither @nerv nor
// @nerv-release), release's own providers are installed today bundled
// (extension.yaml's providers: bundled) -- Install falls back to every
// provider ProvidersFS ships, never leaving a user with no provider at
// all.
func TestInstall_FreshMachine_InstallsEveryBundledProvider(t *testing.T) {
	deps, settingsPath := newDeps(t)

	result, err := install.Install(deps)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	m := readSettings(t, settingsPath)
	enabled := m["enabledPlugins"].(map[string]any)
	for _, id := range []string{"release-bash@nerv-release", "release-github@nerv-release"} {
		if enabled[id] != true {
			t.Errorf("enabledPlugins = %v, want %s enabled (bundled fallback)", enabled, id)
		}
	}
	if len(result.BundledProviders) != 2 {
		t.Errorf("Result.BundledProviders = %v, want both providers", result.BundledProviders)
	}
}

// TestInstall_CarriesOverProviderEnabledUnderNerv covers the carry-over
// half of R-S6: a provider enabled as <name>@nerv before install must be
// installed as <name>@nerv-release, with no fallback to the other
// bundled provider the user never had enabled.
func TestInstall_CarriesOverProviderEnabledUnderNerv(t *testing.T) {
	deps, settingsPath := newDeps(t)
	writeSettings(t, settingsPath, `{"enabledPlugins":{"release@nerv":true,"release-github@nerv":true}}`)

	result, err := install.Install(deps)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	m := readSettings(t, settingsPath)
	enabled := m["enabledPlugins"].(map[string]any)
	if enabled["release-github@nerv-release"] != true {
		t.Errorf("enabledPlugins = %v, want release-github@nerv-release carried over", enabled)
	}
	if _, ok := enabled["release-bash@nerv-release"]; ok {
		t.Error("release-bash@nerv-release must not be installed: it was never enabled anywhere, and github was, so there is no fresh-machine fallback")
	}
	if len(result.CarriedOverProviders) != 1 || result.CarriedOverProviders[0] != "release-github" {
		t.Errorf("Result.CarriedOverProviders = %v, want [release-github]", result.CarriedOverProviders)
	}
	if len(result.BundledProviders) != 0 {
		t.Errorf("Result.BundledProviders = %v, want none (not a fresh machine)", result.BundledProviders)
	}
}

func TestInstall_AlreadyEnabledNervReleaseProvider_IsRefreshedWithoutCarryOver(t *testing.T) {
	deps, settingsPath := newDeps(t)
	writeSettings(t, settingsPath, `{"enabledPlugins":{"release@nerv-release":true,"release-bash@nerv-release":true}}`)
	writeInstalledPlugins(t, deps.Home, map[string]string{
		"release@nerv-release":      deps.MarketplaceDir,
		"release-bash@nerv-release": deps.MarketplaceDir,
	})
	fake := &runnertest.FakeRunner{}
	deps.Runner = fake

	if _, err := install.Install(deps); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	assertUninstallThenInstall := func(id string) {
		t.Helper()
		uninstallIdx, installIdx := -1, -1
		for i, c := range fake.Calls {
			if c.Name != "claude" || len(c.Args) < 3 || c.Args[0] != "plugin" {
				continue
			}
			if c.Args[1] == "uninstall" && c.Args[2] == id {
				uninstallIdx = i
			}
			if c.Args[1] == "install" && c.Args[2] == id {
				installIdx = i
			}
		}
		if uninstallIdx == -1 {
			t.Errorf("Install() never uninstalled %s before refreshing it", id)
		}
		if installIdx == -1 || installIdx < uninstallIdx {
			t.Errorf("Install() must reinstall %s after uninstalling (uninstall at %d, install at %d)", id, uninstallIdx, installIdx)
		}
	}
	assertUninstallThenInstall("release@nerv-release")
	assertUninstallThenInstall("release-bash@nerv-release")

	m := readSettings(t, settingsPath)
	enabled := m["enabledPlugins"].(map[string]any)
	if enabled["release-bash@nerv-release"] != true {
		t.Errorf("enabledPlugins = %v, want release-bash@nerv-release to stay enabled", enabled)
	}
	if _, ok := enabled["release-github@nerv-release"]; ok {
		t.Error("release-github@nerv-release must not be installed: it was never enabled, and bash already was, so there is no fallback")
	}
}

func TestInstall_DisablesLegacyNervPlugins(t *testing.T) {
	deps, settingsPath := newDeps(t)
	writeSettings(t, settingsPath, `{"enabledPlugins":{"release@nerv":true,"release-bash@nerv":true,"release-github@nerv":true,"task@nerv":true},"extraKnownMarketplaces":{"nerv":{"source":{"source":"directory","path":"/somewhere"}}}}`)

	result, err := install.Install(deps)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	m := readSettings(t, settingsPath)
	enabled := m["enabledPlugins"].(map[string]any)
	for _, legacy := range []string{"release@nerv", "release-bash@nerv", "release-github@nerv"} {
		if _, ok := enabled[legacy]; ok {
			t.Errorf("%s should have been disabled", legacy)
		}
	}
	if enabled["task@nerv"] != true {
		t.Error("task@nerv must never be touched")
	}
	marketplaces := m["extraKnownMarketplaces"].(map[string]any)
	if _, ok := marketplaces["nerv"]; !ok {
		t.Error("the nerv marketplace must never be unregistered")
	}
	if len(result.DisabledLegacyPlugins) != 3 {
		t.Errorf("DisabledLegacyPlugins = %v, want 3 entries", result.DisabledLegacyPlugins)
	}
}

// TestInstall_FirstInstall_SkipsUninstallWhenPluginNotYetInstalled covers
// a real install.sh/install.ps1 run: on a first install, release@nerv-release
// is not in installed_plugins.json yet, so "claude plugin uninstall
// release@nerv-release" fails with wording this package must not depend
// on ("not found in installed plugins", never "not installed"). Install
// must check installed_plugins.json itself and skip the uninstall call
// entirely instead of treating that failure as fatal.
func TestInstall_FirstInstall_SkipsUninstallWhenPluginNotYetInstalled(t *testing.T) {
	deps, _ := newDeps(t)
	fake := &runnertest.FakeRunner{
		Responses: map[string]runnertest.Response{
			"claude plugin uninstall release@nerv-release": {
				Stderr:   `Plugin "release@nerv-release" not found in installed plugins`,
				ExitCode: 1,
			},
		},
	}
	deps.Runner = fake

	if _, err := install.Install(deps); err != nil {
		t.Fatalf("Install() error = %v, want nil on a first install", err)
	}

	for _, c := range fake.Calls {
		if c.Name == "claude" && len(c.Args) >= 2 && c.Args[0] == "plugin" && c.Args[1] == "uninstall" {
			t.Errorf("Install() called claude plugin uninstall on a first install, want it skipped: %v", c.Args)
		}
	}
}

func TestInstall_UninstallFailsForOtherReason_PropagatesError(t *testing.T) {
	deps, _ := newDeps(t)
	writeInstalledPlugins(t, deps.Home, map[string]string{"release@nerv-release": deps.MarketplaceDir})
	writeSettings(t, deps.SettingsPath, `{"enabledPlugins":{"release@nerv-release":true}}`)
	fake := &runnertest.FakeRunner{
		Responses: map[string]runnertest.Response{
			"claude plugin uninstall release@nerv-release": {Stderr: "network error talking to the plugin registry", ExitCode: 1},
		},
	}
	deps.Runner = fake

	if _, err := install.Install(deps); err == nil {
		t.Fatal("Install() error = nil, want an error for an unrelated uninstall failure")
	}
}

// TestInstall_OnInstallFailure_LeavesSettingsByteIdentical is the
// rollback safety net: if installing the new plugins through the real
// CLI fails, settings.json must stay exactly as it was, so the user is
// never left without a working release provider (the old nerv plugins
// stay enabled).
func TestInstall_OnInstallFailure_LeavesSettingsByteIdentical(t *testing.T) {
	deps, settingsPath := newDeps(t)
	existing := `{"enabledPlugins":{"release@nerv":true,"task@nerv":true},"extraKnownMarketplaces":{"nerv":{"source":{"source":"directory","path":"/somewhere"}}}}`
	writeSettings(t, settingsPath, existing)
	fake := &runnertest.FakeRunner{
		Responses: map[string]runnertest.Response{
			"claude plugin install release@nerv-release": {Stderr: `Plugin "release" not found in marketplace "nerv-release"`, ExitCode: 1},
		},
	}
	deps.Runner = fake

	result, err := install.Install(deps)
	if err == nil {
		t.Fatal("Install() error = nil, want an error when the CLI install fails")
	}
	if len(result.DisabledLegacyPlugins) != 0 {
		t.Errorf("DisabledLegacyPlugins = %v, want none on a failed install", result.DisabledLegacyPlugins)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	if string(after) != existing {
		t.Errorf("settings.json changed on a failed install:\nbefore: %s\nafter:  %s", existing, after)
	}
}

func TestInstall_RefreshesPluginCache(t *testing.T) {
	deps, _ := newDeps(t)
	fake := &runnertest.FakeRunner{}
	deps.Runner = fake

	if _, err := install.Install(deps); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	foundAdd := false
	foundInstall := false
	for _, c := range fake.Calls {
		if c.Name == "claude" && len(c.Args) >= 3 && c.Args[0] == "plugin" && c.Args[1] == "marketplace" && c.Args[2] == "add" {
			foundAdd = true
		}
		if c.Name == "claude" && len(c.Args) >= 2 && c.Args[0] == "plugin" && c.Args[1] == "install" {
			foundInstall = true
		}
	}
	if !foundAdd {
		t.Error("Install() never ran claude plugin marketplace add")
	}
	if !foundInstall {
		t.Error("Install() never ran claude plugin install")
	}
}
