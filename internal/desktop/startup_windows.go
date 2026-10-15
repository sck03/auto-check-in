//go:build windows

package desktop

import (
	"errors"
	"golang.org/x/sys/windows/registry"
	"os"
	"syscall"
)

type Startup interface {
	Enabled() bool
	Set(bool) error
}
type registryStartup struct{ keyPath, command string }

const startupValue = "AutoCheckIn"
const startupKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func NewStartup(executable, dataDir string) Startup {
	if file := os.Getenv("CHECKIN_TEST_STARTUP_FILE"); file != "" {
		return &fileStartup{path: file}
	}
	command := syscall.EscapeArg(executable) + " -background -data " + syscall.EscapeArg(dataDir)
	return &registryStartup{keyPath: startupKey, command: command}
}
func (s *registryStartup) Enabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, s.keyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(startupValue)
	return err == nil && value == s.command
}
func (s *registryStartup) Set(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, s.keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if enabled {
		return key.SetStringValue(startupValue, s.command)
	}
	err = key.DeleteValue(startupValue)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

// A test-only backend lets UI tests exercise the switch without changing the
// user's actual startup registration.
type fileStartup struct{ path string }

func (s *fileStartup) Enabled() bool {
	b, e := os.ReadFile(s.path)
	return e == nil && string(b) == "enabled"
}
func (s *fileStartup) Set(on bool) error {
	value := "disabled"
	if on {
		value = "enabled"
	}
	return os.WriteFile(s.path, []byte(value), 0600)
}
