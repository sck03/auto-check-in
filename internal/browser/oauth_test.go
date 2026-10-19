package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/transport"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

func TestOAuthConsentContinuesToCallbackOnce(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	for _, method := range []string{"github", "linuxdo"} {
		for _, interactive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/interactive=%v", method, interactive), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "<title>Local fixture</title>")
				}))
				defer server.Close()
				factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
				defer factory.Close()
				template, _ := domain.TemplateByID("agentrouter")
				site := template.Site
				site.BaseURL = server.URL
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				session, err := factory.Open(ctx, site, domain.Credentials{Method: method})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				s := session.(*Session)
				authorize, label := "https://github.com/login/oauth/authorize", "Authorize AgentRouter"
				if method == "linuxdo" {
					authorize, label = "https://connect.linux.do/oauth2/authorize", "允许"
				}
				authorize += "?redirect_uri=" + url.QueryEscape(site.BaseURL+"/callback")
				if method == "linuxdo" {
					authorize = "https://connect.linux.do/oauth2/authorize?response_type=code&client_id=fixture&state=fixture"
				}
				approvalLink := "https://connect.linux.do/oauth2/approve/fixture"
				var approvals, callbacks atomic.Int32
				// Fulfill every request in CDP: real provider domains exercise the URL
				// guards without contacting GitHub/LinuxDO or using any real account.
				events := chromedp.Events(s.ctx, fetch.RequestPaused)
				go func() {
					for event, err := range events {
						if err != nil {
							return
						}
						body, contentType := "", "text/html; charset=utf-8"
						headers := []*fetch.HeaderEntry{}
						switch event.Request.URL {
						case site.BaseURL + "/login":
							body = fmt.Sprintf(`<script>localStorage.setItem('status','{"github_client_id":"fixture","linuxdo_client_id":"fixture"}')</script><button onclick="location.href=%s">使用 %s 继续</button>`, strings.ReplaceAll(jsonJS(authorize), `"`, "&quot;"), method)
						case authorize:
							body = fmt.Sprintf(`<form method="post"><button id="js-oauth-authorize-btn" name="authorize" value="1">%s</button></form>`, label)
							if method == "linuxdo" {
								body = fmt.Sprintf(`<div class="oauth-panel"><div class="oauth-app-info"><a href="%s/">Application</a></div><div class="oauth-actions"><a href="%s" class="btn-pill btn-pill-primary">允许</a><a href="/oauth2/decline/fixture">拒绝</a></div></div>`, site.BaseURL, approvalLink)
							}
							if event.Request.Method == "POST" {
								approvals.Add(1)
								// Leave the same consent button visible for several polling
								// cycles while the callback is delayed: never submit twice.
								body += fmt.Sprintf(`<script>setTimeout(()=>location.href=%s,1800)</script>`, jsonJS(site.BaseURL+"/callback"))
							}
						case approvalLink:
							approvals.Add(1)
							body = fmt.Sprintf(`<script>setTimeout(()=>location.href=%s,1800)</script>`, jsonJS(site.BaseURL+"/callback"))
						case site.BaseURL + "/callback":
							body = fmt.Sprintf(`<script>fetch('/api/oauth/%s').then(r=>r.json()).then(d=>{localStorage.setItem('user',JSON.stringify(d.data));history.replaceState(null,'','/console/personal')})</script>`, method)
						case site.BaseURL + "/api/oauth/" + method:
							callbacks.Add(1)
							body, contentType = `{"success":true,"data":{"id":7}}`, "application/json"
							headers = append(headers, &fetch.HeaderEntry{Name: "Set-Cookie", Value: "session=fresh; Path=/; HttpOnly"})
						case site.BaseURL + "/api/user/self":
							body = `<!doctype html><meta name="aliyun_waf_aa" content="fixture">`
						}
						_, _ = cdp.Call(s.ctx, chromedp.FromContext(s.ctx).Target, fetch.FulfillRequest, fetch.FulfillRequestParams{
							RequestID: event.RequestID, ResponseCode: 200, Body: []byte(body),
							ResponseHeaders: append(headers, &fetch.HeaderEntry{Name: "Content-Type", Value: contentType}),
						})
					}
				}()
				if _, err := cdp.Call(s.ctx, chromedp.FromContext(s.ctx).Target, fetch.Enable, fetch.EnableParams{}); err != nil {
					t.Fatal(err)
				}
				if s.ConfirmedLogin(ctx) {
					t.Fatal("unobserved login must not count as confirmed")
				}
				var response port.Response
				if interactive {
					if err = s.run(ctx, cdpNavigateDOMReady(site.BaseURL+site.LoginPath)); err != nil {
						t.Fatal(err)
					}
					go s.assistOAuth(ctx, method)
					select {
					case response = <-s.loginResponses:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				} else {
					response, err = session.Login(ctx)
				}
				if err != nil || transport.UserID(response.Body) != "7" || approvals.Load() != 1 || callbacks.Load() != 1 {
					t.Fatalf("consent did not complete once: approvals=%d callbacks=%d response=%s err=%v", approvals.Load(), callbacks.Load(), response.Body, err)
				}
				if err := s.pollPage(ctx, "location.pathname==='/console/personal' && localStorage.getItem('user')!==null", nil, 3*time.Second); err != nil || !s.ConfirmedLogin(ctx) {
					t.Fatalf("successful callback was not retained as login proof: %v", err)
				}
				for _, user := range []string{`{}`, `{"id":8}`} {
					if err := s.run(ctx, cdpEvaluate("localStorage.setItem('user',"+jsonJS(user)+")", nil)); err != nil {
						t.Fatal(err)
					}
					if s.ConfirmedLogin(ctx) {
						t.Fatal("logout or a different user reused previous login proof")
					}
				}
			})
		}
	}
}

func TestAgentRouterOAuthFreshLogin(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	for _, method := range []string{"github", "linuxdo"} {
		for _, popup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/popup=%v", method, popup), func(t *testing.T) {
				visible := os.Getenv("CHECKIN_NATIVE_BROWSER_TEST") == "1"
				var logins, unexpected atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/user/self":
						// Fresh callback capture must work even when profile queries are
						// intercepted, as on the real AgentRouter site.
						if !visible {
							w.Header().Set("Content-Type", "application/json")
							if c, err := r.Cookie("session"); err == nil && c.Value == "fresh" {
								fmt.Fprint(w, `{"success":true,"data":{"id":7,"quota":100}}`)
							} else {
								w.WriteHeader(http.StatusUnauthorized)
								fmt.Fprint(w, `{"success":false}`)
							}
							return
						}
						fmt.Fprint(w, `<!doctype html><meta name="aliyun_waf_aa" content="fixture">`)
					case "/api/oauth/" + method:
						logins.Add(1)
						if _, err := r.Cookie("session"); err == nil {
							t.Error("AgentRouter did not discard its previous session")
						}
						http.SetCookie(w, &http.Cookie{Name: "session", Value: "fresh", Path: "/"})
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"success":true,"data":{"id":7}}`)
					case "/callback":
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						finish := map[bool]string{true: "window.close()", false: "location.href='/console'"}[popup]
						if visible {
							// AgentRouter returns within its SPA and leaves the OAuth tab open.
							finish = "history.replaceState(null,'','/console')"
						}
						fmt.Fprintf(w, `<script>fetch('/api/oauth/%s').then(r=>r.json()).then(d=>{localStorage.setItem('user',JSON.stringify(d.data));%s})</script>`, method, finish)
					case "/console":
						fmt.Fprint(w, "<title>Logged in</title>")
					case "/login":
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						other := "github"
						if method == "github" {
							other = "linuxdo"
						}
						fmt.Fprintf(w, `<button onclick="fetch('/wrong')">使用 %s 继续</button><button id="chosen" onclick="window.open(localStorage.getItem('status')?'/callback':'/wrong','%s')">使用 %s 继续</button><script>setTimeout(()=>localStorage.setItem('status','{"github_client_id":"fixture","linuxdo_client_id":"fixture"}'),500)</script>`, other, map[bool]string{true: "_blank", false: "_self"}[popup], method)
					default:
						if r.URL.Path != "/favicon.ico" {
							unexpected.Add(1)
						}
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer server.Close()
				factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true", InteractiveVisible: visible})
				defer factory.Close()
				template, _ := domain.TemplateByID("agentrouter")
				site := template.Site
				site.BaseURL = server.URL
				runner := provider.Runner{Factory: factory, Now: func() time.Time { return time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC) }}
				creds := domain.Credentials{Method: method, CookieHeader: "session=old", APIUser: "7", Storage: map[string]map[string]string{site.BaseURL: {"user": `{"id":7}`}}}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				for i := 0; i < 2; i++ {
					result, saved := runner.Execute(ctx, site, creds)
					if !result.Complete() || result.Evidence != "fresh_login" || saved == nil || saved.APIUser != "7" {
						t.Fatalf("OAuth fresh login %d: %+v", i, result)
					}
					if !strings.Contains(saved.Storage[site.BaseURL]["user"], "7") {
						t.Fatal("fresh session storage was not captured")
					}
					creds = *saved
				}
				if logins.Load() != 2 || unexpected.Load() != 0 {
					t.Fatalf("wrong provider, duplicate login, logout or check-in request: logins=%d unexpected=%d", logins.Load(), unexpected.Load())
				}
				if visible && factory.starts != 2 {
					t.Fatal("desktop OAuth runs must each use an ordinary isolated browser window")
				}
			})
		}
	}
}
