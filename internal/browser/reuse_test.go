package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/provider"
)

func TestSavedOAuthSessionReuseAndExpiry(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("real browser integration disabled")
	}
	for _, adapter := range []string{"anyrouter", "newapi"} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expired=%v", adapter, expired), func(t *testing.T) {
				var logins atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/login":
						w.Header().Set("Content-Type", "text/html")
						fmt.Fprint(w, `<script>if(localStorage.getItem('user'))location.replace('/console')</script><button onclick="fetch('/api/oauth/linuxdo').then(r=>r.json()).then(d=>{localStorage.setItem('user',JSON.stringify(d.data));history.replaceState(null,'','/console')})">使用 LinuxDO 继续</button>`)
					case "/console":
						fmt.Fprint(w, `<title>Console</title>`)
					case "/api/user/self":
						w.Header().Set("Content-Type", "application/json")
						c, _ := r.Cookie("session")
						if c == nil || (expired && c.Value != "fresh") {
							w.WriteHeader(http.StatusUnauthorized)
							fmt.Fprint(w, `{"success":false,"message":"未登录"}`)
						} else {
							fmt.Fprint(w, `{"success":true,"data":{"id":7}}`)
						}
					case "/api/oauth/linuxdo":
						logins.Add(1)
						http.SetCookie(w, &http.Cookie{Name: "session", Value: "fresh", Path: "/"})
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"success":true,"data":{"id":7}}`)
					case "/api/user/checkin":
						if r.Method == http.MethodGet {
							fmt.Fprint(w, `{"success":true,"data":{"checked_in":false}}`)
						} else {
							fmt.Fprint(w, `{"success":true}`)
						}
					default:
						fmt.Fprint(w, `{"success":true}`)
					}
				}))
				defer server.Close()
				factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
				defer factory.Close()
				template, _ := domain.TemplateByID(adapter)
				site := template.Site
				site.BaseURL, site.Transport = server.URL, "browser"
				runner := provider.Runner{Factory: factory, Now: func() time.Time { return time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC) }}
				creds := domain.Credentials{Method: "linuxdo", APIUser: "7", CookieHeader: "session=saved", Storage: map[string]map[string]string{site.BaseURL: {"user": `{"id":7}`}}}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				result, saved := runner.Execute(ctx, site, creds)
				want := int32(0)
				if expired {
					want = 1
				}
				if !result.Complete() || saved == nil || logins.Load() != want {
					t.Fatalf("saved session handling: result=%+v logins=%d want=%d", result, logins.Load(), want)
				}
			})
		}
	}
}
