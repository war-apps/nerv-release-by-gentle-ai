package version_test

import (
	"testing"
	"testing/fstest"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/version"
)

func TestPluginVersion_ReadsPluginJSON(t *testing.T) {
	fsys := fstest.MapFS{
		".claude-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"release","version":"1.2.3"}`)},
	}
	got, err := version.PluginVersion(fsys)
	if err != nil {
		t.Fatalf("PluginVersion() error = %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("PluginVersion() = %q, want %q", got, "1.2.3")
	}
}

func TestPluginVersion_MissingManifest_Errors(t *testing.T) {
	fsys := fstest.MapFS{}
	if _, err := version.PluginVersion(fsys); err == nil {
		t.Fatal("PluginVersion() error = nil, want an error for a missing manifest")
	}
}

func TestBinary_DefaultsToDev(t *testing.T) {
	if version.Binary != "dev" {
		t.Errorf("Binary = %q, want %q (overridden only by goreleaser ldflags)", version.Binary, "dev")
	}
}
