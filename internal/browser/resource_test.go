package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

func TestDeferredProfileCleanupRetriesOnlyWhilePending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		factory := New(Config{TempDir: t.TempDir()})
		defer factory.Close()
		var attempts atomic.Int32
		factory.removeProfile = func(string) error {
			if attempts.Add(1) == 1 {
				return errors.New("fixture file lock")
			}
			return nil
		}
		factory.cleanupProfile(filepath.Join(factory.config.TempDir, "browser-pool-123"))
		if factory.Stats().ProfileCleanup.Pending != 1 {
			t.Fatal("failure was lost")
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if factory.Stats().ProfileCleanup.Pending != 0 || attempts.Load() != 2 {
			t.Fatal("cleanup did not recover")
		}
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		if attempts.Load() != 2 {
			t.Fatal("idle cleanup kept polling")
		}
	})
}

func TestCapacitySnapshotBroadcastsSlotRelease(t *testing.T) {
	f := New(Config{})
	defer f.Close()
	for range 2 {
		if err := f.reserveSlot(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	capacity := f.Capacity()
	if len(capacity.BlockedTransports) != 1 {
		t.Fatal("full pool was not reported")
	}
	f.releaseSlot()
	select {
	case <-capacity.Changed:
	default:
		t.Fatal("slot release did not notify workers")
	}
	if len(f.Capacity().BlockedTransports) != 0 {
		t.Fatal("released pool remains blocked")
	}
	f.releaseSlot()
}

func TestBrowserResponseAndStorageBudgets(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("real Chromium integration disabled")
	}
	now := time.Now()
	unicodeBody := strings.Repeat("界", 10000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			fmt.Fprint(w, strings.Repeat("x", 2<<20))
		case "/retry":
			w.Header().Set("Retry-After", now.Add(10*time.Minute).UTC().Format(http.TimeFormat))
			w.WriteHeader(429)
		case "/unicode":
			fmt.Fprint(w, unicodeBody)
		default:
			fmt.Fprint(w, "<title>Local fixture</title>")
		}
	}))
	defer server.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := factory.Open(ctx, site, domain.Credentials{Method: "cookie"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	response, err := session.Request(ctx, "GET", "/retry", nil)
	if err != nil || response.RetryAfterSeconds < 570 || response.RetryAfterSeconds > 600 {
		t.Fatalf("retry date lost: %+v %v", response, err)
	}
	response, err = session.Request(ctx, "GET", "/unicode", nil)
	if err != nil || string(response.Body) != unicodeBody {
		t.Fatal("streaming decoder corrupted UTF-8", err)
	}
	if _, err = session.Request(ctx, "GET", "/large", nil); err == nil {
		t.Fatal("oversized GET accepted")
	}
	if _, err = session.Request(ctx, "POST", "/large", nil); !errors.Is(err, port.ErrUncertain) {
		t.Fatal("oversized POST lost uncertainty", err)
	}
	driver := session.(*Session)
	if err := driver.run(ctx, cdpEvaluate("localStorage.setItem('fixture-cache','x'.repeat(2097153))", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Snapshot(ctx); !errors.Is(err, domain.ErrSessionLimit) {
		t.Fatal("oversized localStorage did not return actionable limit error", err)
	}
}
