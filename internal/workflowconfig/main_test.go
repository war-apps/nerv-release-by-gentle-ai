package workflowconfig_test

import (
	"os"
	"testing"
)

// TestMain clears XDG_CONFIG_HOME so the user config path is built from
// each test's fake home, not from the machine running the tests (as
// nerv-task's internal/config does; aiworkflow.UserConfigDir reads
// XDG_CONFIG_HOME/%AppData% even for a fake home).
func TestMain(m *testing.M) {
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Exit(m.Run())
}
