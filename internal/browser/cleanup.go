package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type ProfileCleanup struct {
	Removed       int `json:"removed"`
	Failed        int `json:"failed"`
	RuntimeFailed int `json:"runtime_failed"`
	Pending       int `json:"pending"`
}

func (f *Factory) cleanupProfile(profile string) {
	if err := f.removeProfile(profile); err != nil {
		slog.Warn("browser profile cleanup deferred", "profile", filepath.Base(profile), "error", err)
		f.poolMu.Lock()
		f.cleanup.RuntimeFailed++
		f.failedProfiles[profile] = struct{}{}
		if !f.closed && !f.cleanupRunning {
			f.cleanupRunning = true
			f.teardowns.Add(1)
			go func() {
				defer f.teardowns.Done()
				for range 2 {
					timer := time.NewTimer(2 * time.Second)
					select {
					case <-f.lifetime.Done():
						timer.Stop()
					case <-timer.C:
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					f.retryProfiles(ctx)
					cancel()
					f.poolMu.Lock()
					finished := len(f.failedProfiles) == 0 || f.closed
					if finished {
						f.cleanupRunning = false
					}
					f.poolMu.Unlock()
					if finished {
						return
					}
				}
				f.poolMu.Lock()
				f.cleanupRunning = false
				f.poolMu.Unlock()
			}()
		}
		f.poolMu.Unlock()
	}
}

func (f *Factory) retryProfiles(ctx context.Context) {
	f.poolMu.Lock()
	profiles := make([]string, 0, len(f.failedProfiles))
	for profile := range f.failedProfiles {
		profiles = append(profiles, profile)
	}
	f.poolMu.Unlock()
	for _, profile := range profiles {
		if ctx.Err() != nil {
			return
		}
		if err := f.removeProfile(profile); err == nil {
			f.poolMu.Lock()
			delete(f.failedProfiles, profile)
			f.poolMu.Unlock()
		}
	}
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
