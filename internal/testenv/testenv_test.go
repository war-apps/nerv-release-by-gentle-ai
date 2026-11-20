package testenv_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/testenv"
)

// After IsolateUserConfig the user config directory is a fresh temporary
// directory on every OS, whatever home a test passes, and restore removes
// it and puts the environment back.
func TestIsolateUserConfig_MovesTheUserConfigDirAway(t *testing.T) {
	t.Setenv("AppData", `C:\real\AppData\Roaming`)
	t.Setenv("XDG_CONFIG_HOME", "/real/.config")
	before := aiworkflow.UserConfigDir(t.TempDir(), runtime.GOOS)

	restore, err := testenv.IsolateUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	dir := aiworkflow.UserConfigDir(t.TempDir(), runtime.GOOS)
	if dir == before || !strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("user config dir = %q (was %q), want a directory under %s", dir, before, os.TempDir())
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("user config dir %s: %v", dir, err)
	}

	restore()
	if got := os.Getenv("AppData"); got != `C:\real\AppData\Roaming` {
		t.Errorf("AppData after restore = %q", got)
	}
	if got := os.Getenv("XDG_CONFIG_HOME"); got != "/real/.config" {
		t.Errorf("XDG_CONFIG_HOME after restore = %q", got)
	}
	if _, err := os.Stat(filepath.Clean(dir)); !os.IsNotExist(err) {
		t.Errorf("isolated dir %s still exists after restore: %v", dir, err)
	}
}
