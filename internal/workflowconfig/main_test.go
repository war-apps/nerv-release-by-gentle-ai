package workflowconfig_test

import (
	"os"
	"testing"
)

// TestMain clears XDG_CONFIG_HOME and AppData so the user config path is
// built from each test's fake home on every OS, never from the machine
// running the tests (aiworkflow.UserConfigDir prefers them to a fake home:
// XDG_CONFIG_HOME on Linux, %AppData% on Windows).
func TestMain(m *testing.M) {
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("AppData")
	os.Exit(m.Run())
}
