package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/domain"
)

func TestMain(m *testing.M) {
	if ready := os.Getenv("CHECKIN_BROWSER_HELPER_READY"); ready != "" {
		// This child behaves like a browser process that never answers CDP.
		_ = os.WriteFile(ready, nil, 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestStalledBrowserStartupStaysCancellable(t *testing.T) {
	for _, stopFactory := range []bool{false, true} {
		name := "request"
		if stopFactory {
			name = "factory"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			ready := filepath.Join(directory, "started")
			t.Setenv("CHECKIN_BROWSER_HELPER_READY", ready)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			factory := New(Config{Executable: executable, TempDir: directory})
			defer factory.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			site := domain.Templates()[2].Site
			go func() { _, err := factory.Open(ctx, site, domain.Credentials{}); result <- err }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("startup helper did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			stats := make(chan PoolStats, 1)
			go func() { stats <- factory.Stats() }()
			select {
			case <-stats:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("stalled startup blocked diagnostics")
			}
			waiting, stopWaiting := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer stopWaiting()
			if _, err := factory.Open(waiting, site, domain.Credentials{}); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("waiting account could not cancel: %v", err)
			}
			if stopFactory {
				factory.Close()
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("stalled browser unexpectedly opened")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("startup ignored cancellation")
			}
			if factory.Stats().Active != 0 || len(factory.slots) != 0 {
				t.Fatal("cancelled startup retained a browser slot")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != "started" {
				t.Fatalf("temporary browser profile was not removed: %v, %v", entries, err)
			}
		})
	}
}
