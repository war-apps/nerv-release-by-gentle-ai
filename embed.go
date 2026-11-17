// Package nervrelease embeds the nerv-release plugin tree: the release
// extension and its providers, one folder each under plugins/, so the
// version engine (internal/releaseplan.Providers.FS) can load their
// manifests without a network round trip.
//
// Today this only embeds what U2 needs to compile and run `nerv-release
// version next`: the release extension's version slot and its
// conventional-commits provider. The build, host and target slots and
// their providers (docker-compose, bash, github, ssh-compose) are U4/U5
// work.
package nervrelease

import (
	"embed"
	"io/fs"
)

//go:embed all:plugins
var extensionsFS embed.FS

// ExtensionsFS returns the embedded extensions and providers rooted at
// "plugins", so callers see one folder each, such as
// "release/extension.yaml".
func ExtensionsFS() fs.FS {
	sub, err := fs.Sub(extensionsFS, "plugins")
	if err != nil {
		// Unreachable: "plugins" is a constant root name.
		panic(err)
	}
	return sub
}
