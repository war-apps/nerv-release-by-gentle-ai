package workflowconfig_test

import (
	"reflect"
	"strings"
	"testing"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

func personal(t *testing.T) workflowconfig.Personal {
	t.Helper()
	p, err := workflowconfig.LoadPersonal(nervrelease.ExtensionsFS())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// LoadPersonal finds every provider config key a manifest declares
// scope: user (release-ssh-compose's: ssh_auth, ssh_key_file,
// infisical_domain, ssh_known_hosts, ssh_connect_timeout), at
// deploy.providers.<provider>.<key> (deploy: target slot), and no other.
func TestLoadPersonal_FindsTheShippedScopeUserKeys(t *testing.T) {
	p := personal(t)
	for path, want := range map[string]bool{
		"deploy.providers.ssh-compose.ssh_auth":             true,
		"deploy.providers.ssh-compose.ssh_key_file":         true,
		"deploy.providers.ssh-compose.infisical_domain":     true,
		"deploy.providers.ssh-compose.ssh_known_hosts":      true,
		"deploy.providers.ssh-compose.ssh_connect_timeout":  true,
		"deploy.providers.ssh-compose.environment":          false,
		"deploy.providers.ssh-compose.ssh_port":             false,
		"deploy.providers.ssh-compose.update_timeout":       false,
		"release.providers.conventional-commits.tag_format": false,
	} {
		if got := p.Has(path); got != want {
			t.Errorf("Has(%s) = %v, want %v", path, got, want)
		}
	}
}

// Has also reports a child path of a personal key as personal.
func TestPersonal_HasChildPath(t *testing.T) {
	p := personal(t)
	if !p.Has("deploy.providers.ssh-compose.ssh_auth.sub") {
		t.Error("a child of a personal key should be personal too")
	}
}

func TestScalar_PersonalDeployKeysComeFromTheUserFile(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, userPath(home), "deploy:\n  providers:\n    ssh-compose:\n      ssh_auth: key\n")
	write(t, projectPath(repo), "deploy:\n  host: github\n")

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Personal = personal(t)
	if v, found := cfg.Scalar("deploy.providers.ssh-compose.ssh_auth"); !found || v != "key" {
		t.Errorf("deploy.providers.ssh-compose.ssh_auth = %q, %v; want key, true", v, found)
	}
	if v, found := cfg.Scalar("deploy.host"); !found || v != "github" {
		t.Errorf("deploy.host = %q, %v; want github, true (project only)", v, found)
	}
	// A non-personal provider key is still project-only.
	if v, found := cfg.Scalar("deploy.providers.ssh-compose.environment"); found {
		t.Errorf("deploy.providers.ssh-compose.environment = %q from the user file; want unset (not personal)", v)
	}
}

func TestScalar_DefaultsNeverApplyAtRunTime(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, userPath(home), "defaults:\n  release:\n    version: conventional-commits\n  deploy:\n    host: github\n")
	write(t, projectPath(repo), "enabled: true\n")

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Personal = personal(t)
	for _, key := range []string{"release.version", "deploy.host"} {
		if v, found := cfg.Scalar(key); found {
			t.Errorf("%s = %q; a default must never apply at run time", key, v)
		}
	}
}

func TestRunTimeUserKeys_ListsWhatNoLongerApplies(t *testing.T) {
	doc := config.Parse("release:\n  build: bash\ndeploy:\n  providers:\n    ssh-compose:\n      ssh_auth: key\n      ssh_port: 2222\ndefaults:\n  release:\n    version: x\n")
	got, err := workflowconfig.RunTimeUserKeys(doc, personal(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"release.build", "deploy.providers.ssh-compose.ssh_port"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RunTimeUserKeys = %v, want %v", got, want)
	}
}

func TestRunTimeUserKeys_NoneForPersonalKeysOnly(t *testing.T) {
	doc := config.Parse("deploy:\n  providers:\n    ssh-compose:\n      ssh_auth: key\n")
	got, err := workflowconfig.RunTimeUserKeys(doc, personal(t))
	if err != nil || len(got) != 0 {
		t.Errorf("RunTimeUserKeys = %v, %v; want none", got, err)
	}
}

func TestSeed_IsTheDefaultsSectionWithoutPersonalKeys(t *testing.T) {
	user := config.Parse("defaults:\n  deploy:\n    host: github\n    providers:\n      ssh-compose:\n        ssh_auth: key\n        environment: staging\n")
	block, ok := workflowconfig.Seed(user, "deploy", personal(t))
	if !ok {
		t.Fatal("no deploy seed")
	}
	want := "deploy:\n  host: github\n  providers:\n    ssh-compose:\n      environment: staging"
	if strings.TrimRight(block, "\n") != want {
		t.Errorf("seed =\n%s\nwant\n%s", block, want)
	}
	if _, ok := workflowconfig.Seed(user, "release", personal(t)); ok {
		t.Error("a release seed with no defaults.release")
	}
	if _, ok := workflowconfig.Seed(config.Parse("deploy:\n  host: x\n"), "deploy", personal(t)); ok {
		t.Error("a seed from a run-time deploy: section; only defaults.deploy seeds")
	}
}
