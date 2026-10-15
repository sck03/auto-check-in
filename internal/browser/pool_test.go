package browser

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/testsite"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPoolReuseContextIsolationCancellationAndIdleRelease(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("real browser integration disabled")
	}
	fixture := testsite.New()
	defer fixture.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), IdleTimeout: 100 * time.Millisecond, NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = fixture.Server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a, err := factory.Open(ctx, site, domain.Credentials{Method: "cookie", APIUser: "alice", CookieHeader: "session=alice"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := factory.Open(ctx, site, domain.Credentials{Method: "cookie", APIUser: "bob", CookieHeader: "session=bob"})
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	if stats := factory.Stats(); stats.Starts != 1 || stats.Active != 2 {
		t.Fatal(stats)
	}
	if _, err = factory.OpenInteractive(ctx, site, domain.Credentials{}); !errors.Is(err, port.ErrBrowserBusy) {
		t.Fatal("interactive capacity not enforced", err)
	}
	for _, tc := range []struct {
		s    port.Session
		name string
	}{{a, "alice"}, {b, "bob"}} {
		c, err := tc.s.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, cookie := range c.Cookies {
			if cookie.Name == "session" && cookie.Value != tc.name {
				t.Fatalf("cross-account cookie: %s", cookie.Value)
			}
		}
	}
	a.Close()
	response, err := b.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != 200 {
		t.Fatal("closing one account killed another", err)
	}
	b.Close()
	deadline := time.Now().Add(5 * time.Second)
	for factory.Stats().Running && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if factory.Stats().Running {
		t.Fatal("idle browser process not released")
	}
}
