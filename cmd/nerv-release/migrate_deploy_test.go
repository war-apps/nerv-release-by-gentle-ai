package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployprovider"
)

// S3 (6.6 TestDeploy_PipelineBecomesTargetsAndHost): pipeline: becomes
// one release-ssh-compose target per environment plus the github host,
// loadable by deployconfig and the provider manifest; what has no
// equivalent is reported; the personal settings are dropped and
// reported with the command that sets them.
func TestMigrate_PipelineBecomesTargetsAndHost(t *testing.T) {
	w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
	stdout := w.mustMigrate()
	d, err := deployconfig.Load(w.root)
	if err != nil {
		t.Fatalf("deployconfig.Load: %v", err)
	}
	if d.Host != "github" {
		t.Errorf("host = %q, want github", d.Host)
	}
	m, err := deployprovider.Load(nervrelease.ExtensionsFS(), "release-ssh-compose")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tg := range d.Targets {
		names = append(names, tg.Name)
		if tg.Provider != "ssh-compose" {
			t.Errorf("%s provider = %q", tg.Name, tg.Provider)
		}
		for k, v := range tg.Config {
			c, ok := m.Key(k)
			if !ok || c.Scope == "user" || !deployprovider.ValidValue(c.Type, v) {
				t.Errorf("%s: config.%s = %q is not a valid repository key of ssh-compose", tg.Name, k, v)
			}
		}
		want := map[string]string{"environment": tg.Name, "source": "infisical", "infisical_project_id": "p-1", "infisical_app_path": "/app", "ssh_port": "2222", "verify_timeout": "300s"}
		if !reflect.DeepEqual(tg.Config, want) {
			t.Errorf("%s: config = %v, want %v", tg.Name, tg.Config, want)
		}
	}
	if !slices.Equal(names, []string{"staging", "production"}) {
		t.Errorf("targets = %q, want staging, production in order", names)
	}
	containsAll(t, "stdout", stdout,
		"pipeline.deploy: one release-ssh-compose target per environment (staging, production)",
		"pipeline: removed (nerv pipeline was removed)",
		"selection: branches has no equivalent", "choose them with nerv-release deploy --target",
		"pipeline.sync", "pipeline.registry", "pipeline.dotnet", "pipeline.images")
	containsAll(t, "stdout", stdout, personalKeyWarnings...)
	text := w.read(releaseYAMLRel)
	containsNone(t, "release.yaml", text, "pipeline:", "ssh_auth", "infisical_domain", "ssh_key_file")
	if w.exists(w.globalPath()) {
		t.Error("the global release.yaml was written without --user")
	}
	w.assertNoop()
}

// personalKeyWarnings are the warnings of legacy-full's personal
// release-ssh-compose settings: each one dropped, never moved, with the
// command that sets it.
var personalKeyWarnings = []string{
	"pipeline.deploy: infisical_domain (https://infisical.acme) is a personal setting (scope user) and was dropped: a repository never sets your personal settings; if you trust this repository, set it yourself: nerv-release configure --set ssh-compose.infisical_domain=https://infisical.acme",
	"pipeline.deploy: ssh_auth (key) is a personal setting (scope user) and was dropped: a repository never sets your personal settings; if you trust this repository, set it yourself: nerv-release configure --set ssh-compose.ssh_auth=key",
	"pipeline.deploy: ssh_key_file (~/.ssh/id_deploy) is a personal setting (scope user) and was dropped: a repository never sets your personal settings; if you trust this repository, set it yourself: nerv-release configure --set 'ssh-compose.ssh_key_file=~/.ssh/id_deploy'",
}

// S3 (security; nerv-clean S2): with --user too, a repository's personal
// settings never reach the global release.yaml — a cloned repository
// must not set a known_hosts file, key file or Infisical domain for every
// repository of the user: they are dropped and reported, and the global
// file is left as it was, with no backup.
func TestMigrate_PipelinePersonalKeysNeverReachTheGlobalFile(t *testing.T) {
	for _, existing := range []string{"", "# mine\ndeploy:\n  providers:\n    ssh-compose:\n      ssh_auth: agent\n"} {
		w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
		if existing != "" {
			w.write(w.globalPath(), existing)
		}
		stdout := w.mustMigrate("--user")
		containsAll(t, "stdout", stdout, personalKeyWarnings...)
		containsNone(t, "stdout", stdout, "moved to the global release.yaml", "migrate --user here", w.globalPath())
		switch {
		case existing == "" && w.exists(w.globalPath()):
			t.Errorf("the global release.yaml was created:\n%s", w.read(w.globalPath()))
		case existing != "" && w.read(w.globalPath()) != existing:
			t.Errorf("the global release.yaml changed:\n%s", w.read(w.globalPath()))
		}
		if bak := backups(t, w.globalPath()); len(bak) != 0 {
			t.Errorf("global backups = %v, want none", bak)
		}
		containsNone(t, "release.yaml", w.read(releaseYAMLRel), "ssh_auth", "infisical_domain", "ssh_key_file")
		if _, err := deployconfig.Load(w.root); err != nil {
			t.Fatalf("deployconfig.Load: %v", err)
		}
		w.assertNoop("--user")
	}
}

// S3: a pipeline: next to a deploy: section already in today's shape
// keeps that section and reports the targets it did not write; the
// pipeline: section goes.
func TestMigrate_PipelineKeepsAnExistingDeploySection(t *testing.T) {
	text := "pipeline:\n  deploy:\n    environments: [staging]\ndeploy:\n  targets:\n    - { name: lab, provider: script, config: { script: deploy.sh } }\n"
	w := newMigrateWorld(t, migrateGitHubOrigin, map[string]string{releaseYAMLRel: text})
	stdout := w.mustMigrate()
	want := "deploy:\n  targets:\n    - { name: lab, provider: script, config: { script: deploy.sh } }\n"
	if got := w.read(releaseYAMLRel); got != want {
		t.Errorf("release.yaml =\n%s\nwant\n%s", got, want)
	}
	containsAll(t, "stdout", stdout, "deploy: already set, kept: the release-ssh-compose targets translated from pipeline: (staging) were not written; add them by hand")
	w.assertNoop()
}

// S3: a pipeline: section alone (no release:) becomes the deploy: section,
// its targets in 4.2.0's default environments when it lists none.
func TestMigrate_PipelineAloneUsesTheDefaultEnvironments(t *testing.T) {
	w := newMigrateWorld(t, "", map[string]string{releaseYAMLRel: "pipeline:\n  deploy:\n    source: infisical\n"})
	w.mustMigrate()
	d, err := deployconfig.Load(w.root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tg := range d.Targets {
		names = append(names, tg.Name)
	}
	if want := []string{"production-green", "production-blue", "staging", "development"}; !slices.Equal(names, want) || d.Host != "" {
		t.Errorf("targets %q host %q, want %q and no host", names, d.Host, want)
	}
	if !strings.HasPrefix(w.read(releaseYAMLRel), "deploy:\n") {
		t.Errorf("release.yaml =\n%s", w.read(releaseYAMLRel))
	}
}
