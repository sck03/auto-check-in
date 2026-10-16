package browser

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/testsite"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestClosedSessionStopsPageAndPopupTraffic(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("real browser integration disabled")
	}
	for _, visible := range []bool{false, true} {
		t.Run(fmt.Sprintf("visible=%v", visible), func(t *testing.T) {
			if visible && os.Getenv("CHECKIN_NATIVE_BROWSER_TEST") != "1" {
				t.Skip("native desktop login disabled")
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ping" {
					requests.Add(1)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<button style="position:absolute;left:0;top:0;width:200px;height:100px" onclick="window.open('/popup','_blank')">Open</button><script>setInterval(()=>fetch('/ping'),40)</script>`)
			}))
			defer server.Close()
			factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), IdleTimeout: time.Minute,
				InteractiveVisible: visible, NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
			defer factory.Close()
			site := domain.Templates()[0].Site
			site.BaseURL = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			session, err := factory.OpenInteractive(ctx, site, domain.Credentials{Method: "cookie"})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			driver := session.(*Session)
			if err := driver.run(ctx, cdpPoll("!!document.querySelector('button')", nil)); err != nil {
				t.Fatal(err)
			}
			if err := session.Interact(ctx, port.Interaction{Type: "focus"}); err != nil {
				t.Fatal(err)
			}
			// A newly shown native window needs to paint before pointer hit testing.
			if err := driver.run(ctx, cdpEvaluate("new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve(true))))", nil, chromedp.EvalAwaitPromise)); err != nil {
				t.Fatal(err)
			}
			if err := session.Interact(ctx, port.Interaction{Type: "click", X: 80, Y: 40}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				driver.tabMu.RLock()
				ready = len(driver.tabOrder) >= 2 && requests.Load() >= 4
				driver.tabMu.RUnlock()
				if ready {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ready {
				driver.tabMu.RLock()
				tabs := len(driver.tabOrder)
				driver.tabMu.RUnlock()
				t.Fatalf("fixture page and popup did not start polling: tabs=%d requests=%d", tabs, requests.Load())
			}
			session.Close()
			if stats := factory.Stats(); stats.Active != 0 || stats.NativeWindows != 0 || stats.Running == visible {
				t.Fatalf("wrong state after session close: %+v", stats)
			}
			// Allow already-delivered requests to drain, then keep the empty pool
			// alive to prove that closing the application is not needed to stop traffic.
			time.Sleep(100 * time.Millisecond)
			before := requests.Load()
			time.Sleep(250 * time.Millisecond)
			if after := requests.Load(); after != before {
				t.Fatalf("closed account is still accessing the site: %d -> %d", before, after)
			}
		})
	}
}

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
