// Package install materializes and registers the nerv-release plugin
// marketplace in Claude Code, mirroring nerv-task's own install flow
// (kit claudeplugin) but scoped to nerv-release's own marketplace and
// plugins only (R-S6).
package install

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/claudeplugin"
	"github.com/war-apps/nerv-kit-by-gentle-ai/runner"
)

// MarketplaceName is the directory marketplace nerv-release registers
// itself under.
const MarketplaceName = "nerv-release"

// CorePluginID is the core release plugin's id as Claude Code's
// settings.json enabledPlugins keys it.
const CorePluginID = "release@" + MarketplaceName

// OwnerName is the marketplace owner nerv-release registers as.
const OwnerName = "Walter Rodriguez"

// legacyMarketplace is nerv's own marketplace name: before nerv-release
// existed, the release extension and its 6 providers shipped there as
// release@nerv and <name>@nerv.
const legacyMarketplace = "nerv"

// Deps bundles every external effect Install needs, so it can be tested
// against a temporary HOME, a temporary settings.json and a fake
// runner, with no real network and no real claude binary.
type Deps struct {
	Home           string
	SettingsPath   string
	MarketplaceDir string
	// CoreFS is the release plugin's own tree (nervrelease.CoreFS()).
	CoreFS fs.FS
	// ExtensionsFS is the 6 provider plugins alone, with no "release"
	// folder of its own (nervrelease.ProvidersFS()): release's
	// extension.yaml declares providers: bundled, so every provider
	// this tree ships is a candidate for the fresh-machine fallback
	// (Result.BundledProviders).
	ExtensionsFS fs.FS
	Runner       runner.Runner
	Now          time.Time
	Stdout       io.Writer
}

// Result reports what Install did.
type Result struct {
	Materialized          claudeplugin.Result
	DisabledLegacyPlugins []string
	SettingsWritten       bool
	// CarriedOverProviders lists, by its short name (e.g. "release-github"),
	// every provider that was enabled as <name>@nerv before install and
	// is now installed as <name>@nerv-release.
	CarriedOverProviders []string
	// BundledProviders lists, by its short name, every provider
	// installed because none was enabled anywhere (neither @nerv nor
	// already @nerv-release): the fresh-machine fallback to everything
	// release bundles today (R-S6).
	BundledProviders []string
}

// Install materializes the nerv-release plugin bundle to
// deps.MarketplaceDir, registers it with the real claude CLI, installs
// release@nerv-release plus:
//
//   - every provider that was enabled as <name>@nerv (carried over), and
//   - every provider already enabled as <name>@nerv-release (refreshed),
//
// or, when neither carries over anything at all (a fresh machine with no
// release provider enabled anywhere), every provider ExtensionsFS ships
// (release's extension.yaml declares providers: bundled: every provider
// is installed with release, never asked). Only after a successful
// install does it disable release@nerv and its 6 provider plugins, and
// only then does it write Claude Code's settings.json: on any failure
// the file is left byte-identical, and no other nerv plugin (nerv@nerv,
// task@nerv, ...) is ever touched.
func Install(deps Deps) (Result, error) {
	var result Result

	materialized, err := claudeplugin.MaterializeBundle(deps.CoreFS, deps.ExtensionsFS, deps.MarketplaceDir, claudeplugin.Marketplace{
		Name:      MarketplaceName,
		OwnerName: OwnerName,
	})
	if err != nil {
		return result, err
	}
	result.Materialized = materialized

	providers, err := providerNames(deps.ExtensionsFS)
	if err != nil {
		return result, err
	}

	previous, settings, err := loadOrCreateSettings(deps.SettingsPath)
	if err != nil {
		return result, err
	}

	ids := []string{CorePluginID}

	for _, name := range providers {
		if settings.PluginEnabled(name + "@" + legacyMarketplace) {
			id := name + "@" + MarketplaceName
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
				result.CarriedOverProviders = append(result.CarriedOverProviders, name)
			}
		}
	}

	// Every nerv-release plugin the caller already had enabled is
	// refreshed too: this is what keeps `nerv-release upgrade` (which
	// re-runs install with no flags) from silently leaving an enabled
	// provider behind at its old installed version.
	for _, id := range settings.MarketplacePlugins(MarketplaceName) {
		if settings.PluginEnabled(id) && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	if len(ids) == 1 {
		// Fresh machine: no provider enabled anywhere. Fall back to
		// every provider release bundles today.
		for _, name := range providers {
			ids = append(ids, name+"@"+MarketplaceName)
			result.BundledProviders = append(result.BundledProviders, name)
		}
	}

	if err := refreshCache(deps, ids); err != nil {
		return result, err
	}

	settings.RegisterMarketplace(MarketplaceName, deps.MarketplaceDir)
	for _, id := range ids {
		settings.EnablePlugin(id)
	}

	for _, legacy := range legacyPluginIDs(providers) {
		if settings.PluginEnabled(legacy) {
			settings.RemovePlugin(legacy)
			result.DisabledLegacyPlugins = append(result.DisabledLegacyPlugins, legacy)
		}
	}

	written, _, err := claudeplugin.SaveSettings(deps.SettingsPath, settings, previous, deps.Now, "bak-install-")
	if err != nil {
		return result, err
	}
	result.SettingsWritten = written

	return result, nil
}

// legacyPluginIDs is release@nerv and every <name>@nerv of providers,
// the only plugins nerv's own marketplace ships for release tracking:
// installing nerv-release disables exactly these, never any other nerv
// plugin.
func legacyPluginIDs(providers []string) []string {
	ids := []string{"release@" + legacyMarketplace}
	for _, name := range providers {
		ids = append(ids, name+"@"+legacyMarketplace)
	}
	return ids
}

// providerNames lists, sorted, every top-level provider plugin folder of
// fsys (a nil fsys has none).
func providerNames(fsys fs.FS) ([]string, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("listing providers: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func loadOrCreateSettings(path string) ([]byte, *claudeplugin.Settings, error) {
	previous, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, claudeplugin.NewSettings(), nil
		}
		return nil, nil, err
	}
	settings, err := claudeplugin.LoadSettings(path)
	if err != nil {
		return nil, nil, err
	}
	return previous, settings, nil
}

// refreshCache registers deps.MarketplaceDir with the real claude CLI
// and installs every id in ids, uninstalling first when the cache
// already carries an older copy (so an upgraded bundle is actually
// picked up).
//
// On a first install nothing is installed yet, and asking the CLI to
// uninstall a plugin it never installed fails ("Plugin ... not found in
// installed plugins", or any other wording Claude Code chooses) in a
// way this package must not depend on to tell apart from a real
// failure. installed_plugins.json is checked first instead (kit
// claudeplugin.InstalledPlugin), and the uninstall call is skipped
// entirely when the plugin is not there -- any other uninstall failure,
// for a plugin that IS recorded as installed, still fails Install.
func refreshCache(deps Deps, ids []string) error {
	cli := claudeplugin.PluginCLI{Runner: deps.Runner}
	ctx := context.Background()

	if _, err := cli.AddMarketplace(ctx, deps.MarketplaceDir); err != nil {
		return err
	}

	installedPath := filepath.Join(deps.Home, ".claude", "plugins", "installed_plugins.json")

	for _, id := range ids {
		_, installed, err := claudeplugin.InstalledPlugin(installedPath, id)
		if err != nil {
			return err
		}
		if installed {
			if _, err := cli.Uninstall(ctx, id); err != nil {
				return err
			}
		}
		if _, err := cli.Install(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
