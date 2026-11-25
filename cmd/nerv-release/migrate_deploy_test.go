package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployprovider"
)

// S3 (6.6 TestDeploy_PipelineBecomesTargetsAndHost): pipeline: becomes
// one release-ssh-compose target per environment plus the github host,
// loadable by deployconfig and the provider manifest; what has no
// equivalent is reported; without --user the personal settings are
// reported with the --user hint and left out.
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
		"pipeline.sync", "pipeline.registry", "pipeline.dotnet", "pipeline.images",
		"deploy.targets[*].config.ssh_auth: a personal setting (scope user); dropped from the migrated release.yaml (run nerv-release migrate --user here to move it to the global release.yaml instead)",
		"deploy.targets[*].config.infisical_domain: a personal setting",
		"deploy.targets[*].config.ssh_key_file: a personal setting")
	text := w.read(releaseYAMLRel)
	containsNone(t, "release.yaml", text, "pipeline:", "ssh_auth", "infisical_domain", "ssh_key_file")
	if w.exists(w.globalPath()) {
		t.Error("the global release.yaml was written without --user")
	}
	w.assertNoop()
}

// S3/S5: with --user the personal settings move to the global
// release.yaml, at deploy.providers.ssh-compose.<key>; a second run
// changes nothing.
func TestMigrate_PipelinePersonalKeysMoveWithUser(t *testing.T) {
	w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
	stdout := w.mustMigrate("--user")
	want := map[string]any{"deploy": map[string]any{"providers": map[string]any{"ssh-compose": map[string]any{
		"infisical_domain": "https://infisical.acme", "ssh_auth": "key", "ssh_key_file": "~/.ssh/id_deploy",
	}}}}
	if got := yamlMap(t, w.read(w.globalPath())); !reflect.DeepEqual(got, want) {
		t.Errorf("global release.yaml = %v, want %v", got, want)
	}
	containsAll(t, "stdout", stdout,
		"Create "+w.globalPath()+": nerv-release's global configuration, converted",
		"pipeline.deploy: ssh_auth moved to the global release.yaml as deploy.providers.ssh-compose.ssh_auth")
	containsNone(t, "release.yaml", w.read(releaseYAMLRel), "ssh_auth", "infisical_domain", "ssh_key_file")
	if _, err := deployconfig.Load(w.root); err != nil {
		t.Fatalf("deployconfig.Load: %v", err)
	}
	w.assertNoop("--user")
}

// S3: a personal setting the global release.yaml already sets is kept
// there, never overwritten, and reported.
func TestMigrate_PipelinePersonalKeyAlreadySetIsKept(t *testing.T) {
	w := fixtureWorld(t, legacyFull, migrateGitHubOrigin)
	existing := "# mine\ndeploy:\n  providers:\n    ssh-compose:\n      ssh_auth: agent\n"
	w.write(w.globalPath(), existing)
	stdout := w.mustMigrate("--user")
	global := config.Parse(w.read(w.globalPath()))
	if got, _ := global.Scalar("deploy.providers.ssh-compose.ssh_auth"); got != "agent" {
		t.Errorf("ssh_auth = %q, want the global file's own agent", got)
	}
	if got, _ := global.Scalar("deploy.providers.ssh-compose.ssh_key_file"); got != "~/.ssh/id_deploy" {
		t.Errorf("ssh_key_file = %q, want moved", got)
	}
	containsAll(t, "stdout", stdout, "deploy.targets[*].config.ssh_auth: dropped from the migrated release.yaml; the global release.yaml already sets deploy.providers.ssh-compose.ssh_auth to agent")
	if bak := backups(t, w.globalPath()); len(bak) != 1 || mustReadFile(t, bak[0]) != existing {
		t.Errorf("global backups = %v, want one holding the old file", bak)
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
