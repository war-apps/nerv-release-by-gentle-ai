package main

import (
	"encoding/json"
	"fmt"
	"io"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/version"
)

const versionUsage = `Usage: nerv-release version [--json]

Prints the binary and release plugin versions, following the NERV
ecosystem version contract so "nerv" can report nerv-release as
installed or outdated.

--json   Print the versions as JSON ({"binary":..., "plugin":...}).
`

// runVersion is `nerv-release version` (R-S2, U6): {"binary","plugin"},
// plugin being the core release plugin.json version.
func runVersion(args []string, stdout, stderr io.Writer) int {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			fmt.Fprint(stderr, versionUsage)
			return 2
		}
	}

	pluginVersion, err := version.PluginVersion(nervrelease.CoreFS())
	if err != nil {
		fmt.Fprintf(stderr, "nerv-release: %v\n", err)
		return 2
	}

	if jsonOutput {
		info := version.Info{Binary: version.Binary, Plugin: pluginVersion}
		encoded, err := json.Marshal(info)
		if err != nil {
			fmt.Fprintf(stderr, "nerv-release: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}

	fmt.Fprintf(stdout, "nerv-release %s (plugin %s)\n", version.Binary, pluginVersion)
	return 0
}
