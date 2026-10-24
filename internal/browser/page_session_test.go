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

func TestPageSessionReusesAuthenticationAndRenewsOnlyExplicitLogin(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	var logins, clicks, selfCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("session")
		valid := cookie != nil && cookie.Value == "valid"
		switch r.URL.Path {
		case "/api/user/login":
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/", HttpOnly: true})
			fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
		case "/api/user/self":
			selfCalls.Add(1)
			fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
		case "/submit":
			if !valid {
				t.Error("unauthenticated check-in")
			}
			clicks.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case "/login":
			if valid {
				http.Redirect(w, r, "/console", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<form><input name="username"><input type="password"><button type="submit">登录</button></form>
<script>document.querySelector('form').onsubmit=async e=>{e.preventDefault();const r=await fetch('/api/user/login?fixture=true',{method:'POST',body:'{}'});const p=await r.json();localStorage.setItem('user',JSON.stringify(p.data));location.href='/console'}</script>`)
		case "/daily":
			if !valid {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<script>localStorage.setItem('user','{"id":1}')</script><button onclick="fetch('/submit',{method:'POST'}).then(()=>{this.remove();document.querySelector('#result').textContent='签到成功'})">签到</button><div id="result" role="status"></div>`)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<script>localStorage.setItem('user','{"id":1}')</script><p>Console</p>`)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, method, cookie, id string
		want                     string
		logins, clicks           int32
	}{
		{"password_valid", "password", "valid", "1", domain.Succeeded, 0, 1},
		{"oauth_valid", "github", "valid", "1", domain.Succeeded, 0, 1},
		{"password_expired", "password", "expired", "1", domain.Succeeded, 1, 1},
		{"password_expired_wrong_identity", "password", "expired", "2", domain.NeedsAttention, 1, 0},
		{"wrong_identity", "password", "valid", "2", domain.NeedsAttention, 0, 0},
		{"cookie_wrong_identity", "cookie", "valid", "2", domain.NeedsAttention, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logins.Store(0)
			clicks.Store(0)
			selfCalls.Store(0)
			site := domain.Templates()[2].Site
			site.BaseURL, site.Adapter, site.Transport, site.CheckInPath = server.URL, "browser", "browser", "/daily"
			site.LoginPath, site.LoginAPIPath = "/login?redirect=/console", "/api/user/login?fixture=true"
			creds := domain.Credentials{Method: tc.method, Username: "fixture", Password: "fixture-password", APIUser: tc.id, CookieHeader: "session=" + tc.cookie}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, updated := (&provider.Runner{Factory: factory}).Execute(ctx, site, creds)
			if result.Status != tc.want || logins.Load() != tc.logins || clicks.Load() != tc.clicks {
				t.Fatalf("result=%+v logins=%d clicks=%d", result, logins.Load(), clicks.Load())
			}
			if result.Complete() {
				if updated == nil || updated.APIUser != "1" {
					t.Fatal("session was not saved", updated)
				}
				// The next run restores a new browser context, as it would next day.
				result, _ = (&provider.Runner{Factory: factory}).Execute(ctx, site, *updated)
				if !result.Complete() || logins.Load() != tc.logins || clicks.Load() != tc.clicks+1 {
					t.Fatal("restored session logged in again", result, logins.Load(), clicks.Load())
				}
			} else if updated != nil {
				t.Fatal("rejected identity was persisted")
			}
			if selfCalls.Load() != 0 {
				t.Fatal("login API query response was not captured", selfCalls.Load())
			}
		})
	}
}
