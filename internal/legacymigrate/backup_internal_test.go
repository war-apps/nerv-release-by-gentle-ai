package legacymigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Two backups of the same file in the same second (nerv migrate backs
// release.yaml up under .nerv/backups/migrate-<time>/ just before running
// nerv-release migrate) never collide: nerv-release's own folder is
// release-migrate-<time>, and a taken one gets a numbered sibling.
func TestBackupRepoFile_NeverCollides(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 25, 10, 54, 53, 0, time.UTC)
	rel := filepath.Join(".ai-workflow", "release.yaml")
	nervOwn := filepath.Join(root, ".nerv", "backups", "migrate-"+now.Format(backupTimeFormat), rel)
	if err := os.MkdirAll(filepath.Dir(nervOwn), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nervOwn, []byte("nerv's"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := backupRepoFile(root, rel, []byte("one"), now)
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}
	second, err := backupRepoFile(root, rel, []byte("two"), now)
	if err != nil {
		t.Fatalf("second backup in the same second: %v", err)
	}
	if first == second || first == nervOwn {
		t.Fatalf("backups collide: %s, %s (nerv's %s)", first, second, nervOwn)
	}
	for _, p := range []string{first, second} {
		if !strings.Contains(filepath.ToSlash(p), "/.nerv/backups/release-migrate-") {
			t.Errorf("backup %s is not under .nerv/backups/release-migrate-*", p)
		}
	}
	if b, _ := os.ReadFile(nervOwn); string(b) != "nerv's" {
		t.Errorf("nerv's own backup changed: %q", b)
	}
}
