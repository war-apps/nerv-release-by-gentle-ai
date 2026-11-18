package configure_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/configure"
)

func newDeps(t *testing.T) configure.Deps {
	t.Helper()
	return configure.Deps{
		GlobalPath:   filepath.Join(t.TempDir(), "release.yaml"),
		ExtensionsFS: nervrelease.ExtensionsFS(),
		Now:          func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

func TestProviders_SkipsReleaseItself_ListsEverySiblingSorted(t *testing.T) {
	providers, err := configure.Providers(nervrelease.ExtensionsFS())
	if err != nil {
		t.Fatalf("Providers() error = %v", err)
	}
	var names []string
	for _, p := range providers {
		names = append(names, p.Name)
	}
	want := []string{"bash", "conventional-commits", "docker-compose", "github", "script", "ssh-compose"}
	if len(names) != len(want) {
		t.Fatalf("Providers() names = %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("Providers()[%d].Name = %q, want %q", i, names[i], w)
		}
	}
}

func TestPrint_ReportsConfigPathAndProviders(t *testing.T) {
	deps := newDeps(t)

	out, err := configure.Print(deps)
	if err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	if out.ConfigPath != deps.GlobalPath {
		t.Errorf("ConfigPath = %q, want %q", out.ConfigPath, deps.GlobalPath)
	}
	if len(out.Providers) != 6 {
		t.Errorf("Providers = %v, want 6", out.Providers)
	}
	found := false
	for _, kv := range out.Values {
		if kv.Path == "deploy.providers.ssh-compose.ssh_key_file" {
			found = true
			if kv.Set {
				t.Error("ssh_key_file should not be reported as set when the global file does not exist yet")
			}
		}
	}
	if !found {
		t.Error("Values does not list deploy.providers.ssh-compose.ssh_key_file")
	}
}

func TestSet_PersonalKey_WritesAndReturnsOldNew(t *testing.T) {
	deps := newDeps(t)

	result, err := configure.Set(deps, "ssh-compose.ssh_key_file=/etc/nerv/id_ed25519")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if result.New != "/etc/nerv/id_ed25519" {
		t.Errorf("New = %q, want the set value", result.New)
	}

	data, err := os.ReadFile(deps.GlobalPath)
	if err != nil {
		t.Fatalf("reading %s: %v", deps.GlobalPath, err)
	}
	if !strings.Contains(string(data), "ssh_key_file: /etc/nerv/id_ed25519") {
		t.Errorf("%s = %q, want it to carry the new value", deps.GlobalPath, data)
	}
	if !strings.Contains(string(data), "deploy:") {
		t.Errorf("%s = %q, want a deploy: section (ssh-compose is a target provider)", deps.GlobalPath, data)
	}

	result2, err := configure.Set(deps, "ssh-compose.ssh_key_file=/etc/nerv/id_ed25519_2")
	if err != nil {
		t.Fatalf("Set() second call error = %v", err)
	}
	if result2.Old != "/etc/nerv/id_ed25519" {
		t.Errorf("second Set() Old = %q, want the previous value", result2.Old)
	}
}

func TestSet_RepoScopeKey_Refused(t *testing.T) {
	deps := newDeps(t)

	// ssh-compose.environment is scope: repo: it belongs in the
	// repository's release.yaml (init / /release:setup), never in the
	// global file.
	if _, err := configure.Set(deps, "ssh-compose.environment=prod"); err == nil {
		t.Fatal("Set() error = nil, want a refusal for a scope: repo key")
	}
}

func TestSet_UnknownProvider_Errors(t *testing.T) {
	deps := newDeps(t)
	if _, err := configure.Set(deps, "no-such-provider.key=value"); err == nil {
		t.Fatal("Set() error = nil, want an error for an unknown provider")
	}
}

func TestSet_UnknownKey_Errors(t *testing.T) {
	deps := newDeps(t)
	if _, err := configure.Set(deps, "ssh-compose.no_such_key=value"); err == nil {
		t.Fatal("Set() error = nil, want an error for an unknown config key")
	}
}

func TestSet_InvalidKV_Errors(t *testing.T) {
	deps := newDeps(t)
	if _, err := configure.Set(deps, "not-a-key-value-pair"); err == nil {
		t.Fatal("Set() error = nil, want an error for a malformed key=value")
	}
}

func TestRunWizard_SkipsProvidersWithNoUserScopeKeys(t *testing.T) {
	deps := newDeps(t)
	var out strings.Builder
	// "Configure ssh-compose?" -> y, then enough blanks to keep every
	// prompted value, then "Write...?" -> y.
	in := strings.NewReader("y\n" + strings.Repeat("\n", 10) + "y\n")

	if _, err := configure.RunWizard(deps, in, &out); err != nil {
		t.Fatalf("RunWizard() error = %v", err)
	}
	if strings.Contains(out.String(), "Configure bash?") {
		t.Error("the wizard must never ask to configure a provider with no scope: user keys")
	}
	if !strings.Contains(out.String(), "Configure ssh-compose?") {
		t.Errorf("stdout = %q, want it to ask about ssh-compose (it has scope: user keys)", out.String())
	}
}

func TestRunWizard_WritesOnConfirmation(t *testing.T) {
	deps := newDeps(t)
	var out strings.Builder
	in := strings.NewReader("y\n" + "/etc/nerv/key\n" + strings.Repeat("\n", 10) + "y\n")

	result, err := configure.RunWizard(deps, in, &out)
	if err != nil {
		t.Fatalf("RunWizard() error = %v", err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true after confirming a real change")
	}
	data, err := os.ReadFile(deps.GlobalPath)
	if err != nil {
		t.Fatalf("reading %s: %v", deps.GlobalPath, err)
	}
	if !strings.Contains(string(data), "/etc/nerv/key") {
		t.Errorf("%s = %q, want the answered value written", deps.GlobalPath, data)
	}
}

func TestRunWizard_DeclinedWrite_NothingWritten(t *testing.T) {
	deps := newDeps(t)
	var out strings.Builder
	// 1 "configure?" + 1 real answer (infisical_domain) + 4 blanks (the
	// remaining ssh-compose user keys) + 1 explicit decline of the
	// write confirmation.
	in := strings.NewReader("y\n" + "/etc/nerv/key\n" + strings.Repeat("\n", 4) + "n\n")

	if _, err := configure.RunWizard(deps, in, &out); err != nil {
		t.Fatalf("RunWizard() error = %v", err)
	}
	if _, err := os.Stat(deps.GlobalPath); err == nil {
		t.Error("the global file was written despite declining the confirmation")
	}
}

func TestRunWizard_NoChanges_ReportsNoneAndSkipsWrite(t *testing.T) {
	deps := newDeps(t)
	var out strings.Builder
	in := strings.NewReader("n\n")

	result, err := configure.RunWizard(deps, in, &out)
	if err != nil {
		t.Fatalf("RunWizard() error = %v", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false when every provider was declined")
	}
}

func TestRunWizard_InputExhausted_ReturnsErrInputClosed(t *testing.T) {
	deps := newDeps(t)
	var out strings.Builder
	in := strings.NewReader("")

	if _, err := configure.RunWizard(deps, in, &out); err == nil {
		t.Fatal("RunWizard() error = nil, want ErrInputClosed for an exhausted reader")
	}
}
