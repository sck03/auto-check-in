package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartupReclaimsOnlyOwnedProfiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"browser-pool-123", "login-window-456", "other-directory", "browser-pool-not-owned"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "marker"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "browser-pool-789"), []byte("keep ordinary files"), 0600); err != nil {
		t.Fatal(err)
	}
	f := New(Config{TempDir: dir})
	defer f.Close()
	if err := f.ReclaimProfiles(); err != nil {
		t.Fatal(err)
	}
	stats := f.Stats().ProfileCleanup
	if stats.Removed != 2 || stats.Failed != 0 {
		t.Fatalf("unexpected cleanup: %+v", stats)
	}
	for _, name := range []string{"other-directory", "browser-pool-not-owned", "browser-pool-789"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("removed unrelated path %s: %v", name, err)
		}
	}
}

func TestProfileCleanupDoesNotFollowSymlinks(t *testing.T) {
	dir, target := t.TempDir(), t.TempDir()
	marker := filepath.Join(target, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "browser-pool-123")); err != nil {
		t.Skip("symlinks unavailable on this host")
	}
	stats, err := reclaimProfiles(dir)
	if err != nil || stats.Removed != 0 {
		t.Fatalf("followed symlink: %+v %v", stats, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("removed external target", err)
	}
}
