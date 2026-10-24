package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

type ProfileCleanup struct {
	Removed int `json:"removed"`
	Failed  int `json:"failed"`
}

var ownedProfileName = regexp.MustCompile(`^(browser-pool-|login-window-)[0-9]+$`)

// ReclaimProfiles runs only during bootstrap, while the application owns the
// data-directory lock and before any new sessions exist. It neither follows
// links nor removes unknown files or directories in the temporary root.
func (f *Factory) ReclaimProfiles() error {
	stats, err := reclaimProfiles(f.config.TempDir)
	f.poolMu.Lock()
	f.cleanup = stats
	f.poolMu.Unlock()
	return err
}

func reclaimProfiles(dir string) (ProfileCleanup, error) {
	var stats ProfileCleanup
	root, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return stats, fmt.Errorf("浏览器临时目录必须是普通目录")
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !ownedProfileName.MatchString(entry.Name()) {
			continue
		}
		profile := filepath.Join(dir, entry.Name())
		if err := RemoveProfile(profile); err != nil {
			stats.Failed++
			failures = append(failures, fmt.Errorf("清理浏览器临时目录 %s：%w", entry.Name(), err))
		} else {
			stats.Removed++
		}
	}
	return stats, errors.Join(failures...)
}
