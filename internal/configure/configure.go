// Package configure implements `nerv-release configure`: printing and
// editing the global release.yaml (the two-section release:/deploy:
// document the engine itself reads, internal/workflowconfig) -- provider
// choice, every scope: user key with its manifest prompt and default,
// a diff and a confirmation. Mirrors nerv-task's own internal/configure,
// adapted: there is no flat Go struct for the document (it is edited in
// place, comment-preserving, through internal/config.Document), and the
// path a key lives at depends on the provider's own slot
// (internal/workflowconfig.SectionOf).
package configure

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/atomicfile"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/provider"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

// Deps bundles what Print, Set and RunWizard need: the global
// release.yaml path and the provider manifests it configures (the
// embedded tree, nervrelease.ExtensionsFS()).
type Deps struct {
	GlobalPath   string
	ExtensionsFS fs.FS
	Now          func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Providers lists, sorted by name, every provider of fsys (typically
// nervrelease.ExtensionsFS()), skipping the release extension itself
// (it has no provider.yaml of its own).
func Providers(fsys fs.FS) ([]*provider.Provider, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("listing providers: %w", err)
	}
	var out []*provider.Provider
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(fsys, e.Name()+"/"+provider.ProviderFile); err != nil {
			continue // the release extension itself: extension.yaml, not provider.yaml
		}
		p, _, err := provider.LoadProvider(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// userScopeKeys returns p's own config keys with scope: user, in their
// manifest order.
func userScopeKeys(p *provider.Provider) []provider.ConfigKey {
	var out []provider.ConfigKey
	for _, c := range p.Config {
		if c.Scope == "user" {
			out = append(out, c)
		}
	}
	return out
}

// userKeyPath is the dotted path a provider's own config key is read
// and written at, in the global release.yaml
// (<release|deploy>.providers.<provider>.<key>).
func userKeyPath(p *provider.Provider, key string) string {
	return workflowconfig.SectionOf(p.Slot) + ".providers." + p.Name + "." + key
}

// KeyValue is one scope: user key's resolved value, as Print reports it.
type KeyValue struct {
	Path    string `json:"path"`
	Value   string `json:"value"`
	Default string `json:"default"`
	Set     bool   `json:"set"`
}

// ProviderSummary is one provider's config schema, as --print reports
// it.
type ProviderSummary struct {
	Name   string               `json:"name"`
	Slot   string               `json:"slot"`
	Config []provider.ConfigKey `json:"config"`
}

// PrintOutput is the result of `nerv-release configure --print`.
type PrintOutput struct {
	ConfigPath string            `json:"config_path"`
	Values     []KeyValue        `json:"values"`
	Providers  []ProviderSummary `json:"providers"`
}

// Print reads the global release.yaml and reports its config path,
// every scope: user key's resolved value (current, or its manifest
// default when unset) and every known provider's config schema.
func Print(deps Deps) (PrintOutput, error) {
	providers, err := Providers(deps.ExtensionsFS)
	if err != nil {
		return PrintOutput{}, err
	}
	doc, _, err := loadDocument(deps.GlobalPath)
	if err != nil {
		return PrintOutput{}, err
	}

	out := PrintOutput{ConfigPath: deps.GlobalPath}
	for _, p := range providers {
		out.Providers = append(out.Providers, ProviderSummary{Name: p.Name, Slot: p.Slot, Config: p.Config})
		for _, c := range userScopeKeys(p) {
			path := userKeyPath(p, c.Key)
			value, found := doc.Scalar(path)
			if !found {
				value = c.Default
			}
			out.Values = append(out.Values, KeyValue{Path: path, Value: value, Default: c.Default, Set: found})
		}
	}
	return out, nil
}

// SetResult reports one `--set key=value` mutation.
type SetResult struct {
	Key string
	Old string
	New string
}

// Set parses "<provider>.<key>=value", validates it against the
// provider's own manifest (the key must exist and be scope: user), and
// persists it to the global release.yaml.
func Set(deps Deps, kv string) (SetResult, error) {
	key, value, ok := strings.Cut(kv, "=")
	if !ok {
		return SetResult{}, fmt.Errorf("invalid --set %q: want key=value", kv)
	}
	providerName, configKey, ok := strings.Cut(key, ".")
	if !ok {
		return SetResult{}, fmt.Errorf("invalid --set key %q: want <provider>.<key>", key)
	}

	providers, err := Providers(deps.ExtensionsFS)
	if err != nil {
		return SetResult{}, err
	}
	p := findProvider(providers, providerName)
	if p == nil {
		return SetResult{}, fmt.Errorf("provider %q is not installed", providerName)
	}
	ck, ok := p.Key(configKey)
	if !ok {
		return SetResult{}, fmt.Errorf("provider %s has no config key %q", providerName, configKey)
	}
	if ck.Scope != "user" {
		return SetResult{}, fmt.Errorf("%s.%s is a scope: repo key; it belongs in the repository's release.yaml (nerv-release init or /release:setup), not the global configuration", providerName, configKey)
	}
	if !provider.ValidValue(ck.Type, value) {
		return SetResult{}, fmt.Errorf("%s.%s: %q is not a valid %s", providerName, configKey, value, ck.Type)
	}

	doc, previous, err := loadDocument(deps.GlobalPath)
	if err != nil {
		return SetResult{}, err
	}
	path := userKeyPath(p, configKey)
	old, _ := doc.Scalar(path)
	doc.SetScalar(path, value)

	if err := saveDocument(deps.GlobalPath, doc, previous, deps.now()); err != nil {
		return SetResult{}, err
	}
	return SetResult{Key: key, Old: old, New: value}, nil
}

func findProvider(providers []*provider.Provider, name string) *provider.Provider {
	for _, p := range providers {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// loadDocument reads path as a *config.Document. A missing file is not
// an error: an empty document, with previous=nil (so saveDocument never
// backs it up).
func loadDocument(path string) (*config.Document, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Parse(""), nil, nil
		}
		return nil, nil, err
	}
	return config.Parse(string(data)), data, nil
}

// saveDocument writes doc to path when its rendered bytes differ from
// previous (a no-op, no write and no backup, when they are identical),
// backing up any existing file first.
func saveDocument(path string, doc *config.Document, previous []byte, now time.Time) error {
	newBytes := []byte(doc.String())
	if previous != nil && string(newBytes) == string(previous) {
		return nil
	}
	if previous == nil {
		// A brand-new file: nothing to back up.
		return atomicfile.Write(path, newBytes, 0o644)
	}
	_, err := atomicfile.Save(path, newBytes, atomicfile.Options{Now: now, BackupSuffix: "bak-configure-"})
	return err
}

// typedConfigValue converts value to typ's Go representation
// (provider.ValidValue already validated it), for display and for
// cfg.Providers[...] storage in the wizard.
func typedConfigValue(typ, value string) (any, error) {
	switch typ {
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid int", value)
		}
		return n, nil
	case "bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid bool", value)
		}
		return b, nil
	default:
		return value, nil
	}
}
