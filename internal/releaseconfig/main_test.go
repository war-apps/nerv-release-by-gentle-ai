package releaseconfig_test

import (
	"os"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/testenv"
)

// TestMain keeps every test away from the developer's real user config.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
