//go:build windows

package desktop

import (
	"auto-check-in/internal/domain"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupRegistrationUsesCurrentUserAndExactCommand(t *testing.T) {
	// This test uses an isolated test key, never the real Windows Run key.
	path := `Software\AutoCheckIn\Tests\` + domain.NewID()
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	startup := &registryStartup{keyPath: path, command: `"C:\Test App\AutoCheckIn.exe" -background -data "C:\Test Data"`}
	if startup.Enabled() {
		t.Fatal("new key enabled")
	}
	if err := startup.Set(true); err != nil {
		t.Fatal(err)
	}
	if !startup.Enabled() {
		t.Fatal("startup command not stored")
	}
	if err := startup.Set(false); err != nil {
		t.Fatal(err)
	}
	if startup.Enabled() {
		t.Fatal("startup command not removed")
	}
}
func TestDesktopPreferencesRoundTripAndConflict(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHECKIN_TEST_STARTUP_FILE", filepath.Join(dir, "fake-startup"))
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cancel()
	p := m.Status().Preferences
	p.AutoStart = true
	p.MinimizeToTray = false
	saved, err := m.Configure(p)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Preferences.AutoStart || saved.Preferences.MinimizeToTray {
		t.Fatal(saved)
	}
	if _, err = m.Configure(p); err == nil {
		t.Fatal("stale preferences accepted")
	}
	reloaded, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.cancel()
	if reloaded.Status().Preferences != saved.Preferences {
		t.Fatal("preferences lost on restart")
	}
	if len(saved.Menu) != 8 {
		t.Fatal("tray menu missing actions")
	}
}
func TestPreferenceWriteFailureRestoresStartup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHECKIN_TEST_STARTUP_FILE", filepath.Join(dir, "fake-startup"))
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cancel()
	if err = os.Mkdir(filepath.Join(dir, "desktop.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	p := m.Status().Preferences
	p.AutoStart = true
	if _, err = m.Configure(p); err == nil {
		t.Fatal("expected write failure")
	}
	if m.startup.Enabled() {
		t.Fatal("startup changed despite failed save")
	}
}
