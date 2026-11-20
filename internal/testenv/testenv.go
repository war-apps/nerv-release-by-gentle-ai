// Package testenv isolates a test binary from the developer's machine.
package testenv

import (
	"fmt"
	"os"
	"testing"
)

// Run runs the tests of m with the user config isolated, for TestMain:
// os.Exit(testenv.Run(m)).
func Run(m *testing.M) int {
	restore, err := IsolateUserConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolating the user config:", err)
		return 1
	}
	defer restore()
	return m.Run()
}

// userConfigVars are the variables nerv-kit's aiworkflow.UserConfigDir
// prefers over the home directory: AppData on Windows, XDG_CONFIG_HOME
// elsewhere. A test that only fakes the home still reaches the real user
// config through them.
var userConfigVars = []string{"AppData", "XDG_CONFIG_HOME"}

// IsolateUserConfig points the user config directory at a new temporary
// directory, so no test of the binary can read or write the developer's
// real ai-workflow configuration. Call it from TestMain before m.Run;
// restore removes the directory and puts the variables back.
func IsolateUserConfig() (restore func(), err error) {
	dir, err := os.MkdirTemp("", "nerv-release-userconfig-")
	if err != nil {
		return nil, err
	}
	type saved struct {
		value string
		set   bool
	}
	old := map[string]saved{}
	for _, name := range userConfigVars {
		value, set := os.LookupEnv(name)
		old[name] = saved{value, set}
		if err := os.Setenv(name, dir); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	return func() {
		for name, s := range old {
			if s.set {
				os.Setenv(name, s.value)
			} else {
				os.Unsetenv(name)
			}
		}
		os.RemoveAll(dir)
	}, nil
}
