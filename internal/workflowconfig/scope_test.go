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

// R-S4 security fix: a project file must not be able to override a
// personal (scope: user) key by also setting it — the value must still
// come from the user file only, and a personal key the project sets but
// the user does not must stay unset (never read from the project).
func TestScalar_ProjectCannotOverridePersonalKeys(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	write(t, userPath(home), "deploy:\n  providers:\n    ssh-compose:\n      ssh_known_hosts: /home/user/.ssh/known_hosts\n")
	write(t, projectPath(repo), "deploy:\n  host: github\n  providers:\n    ssh-compose:\n      ssh_known_hosts: /tmp/attacker-known-hosts\n      infisical_domain: https://attacker.example\n")

	cfg, err := workflowconfig.Load(home, goos, repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Personal = personal(t)

	if v, found := cfg.Scalar("deploy.providers.ssh-compose.ssh_known_hosts"); !found || v != "/home/user/.ssh/known_hosts" {
		t.Errorf("ssh_known_hosts = %q, %v; want the user's value, never the project's", v, found)
	}
	if v, found := cfg.Scalar("deploy.providers.ssh-compose.infisical_domain"); found {
		t.Errorf("infisical_domain = %q from the project; want unset: a personal key the user does not set is never read from the project", v)
	}
	// A non-personal key keeps "project wins".
	if v, found := cfg.Scalar("deploy.host"); !found || v != "github" {
		t.Errorf("deploy.host = %q, %v; want github, true (project wins; not personal)", v, found)
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

// R-S4 security fix: a project file setting a personal key is reported,
// not silently honored (deployconfig/releaseconfig refuse on this).
func TestProjectPersonalKeys_ListsPersonalKeysTheProjectSets(t *testing.T) {
	doc := config.Parse("deploy:\n  host: github\n  providers:\n    ssh-compose:\n      ssh_known_hosts: /tmp/attacker-known-hosts\n      environment: staging\n")
	got, err := workflowconfig.ProjectPersonalKeys(doc, personal(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deploy.providers.ssh-compose.ssh_known_hosts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ProjectPersonalKeys = %v, want %v", got, want)
	}
}

func TestProjectPersonalKeys_NoneWithoutAPersonalKey(t *testing.T) {
	doc := config.Parse("deploy:\n  host: github\n  providers:\n    ssh-compose:\n      environment: staging\n")
	got, err := workflowconfig.ProjectPersonalKeys(doc, personal(t))
	if err != nil || len(got) != 0 {
		t.Errorf("ProjectPersonalKeys = %v, %v; want none", got, err)
	}
}

func TestPersonalKeyMessage_NamesThePathAndWhereToSetIt(t *testing.T) {
	got := workflowconfig.PersonalKeyMessage("deploy.providers.ssh-compose.ssh_known_hosts")
	want := "deploy.providers.ssh-compose.ssh_known_hosts is a personal key (scope: user): set it in your global release.yaml (nerv-release configure --set ...), not in the repository"
	if got != want {
		t.Errorf("PersonalKeyMessage = %q, want %q", got, want)
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
