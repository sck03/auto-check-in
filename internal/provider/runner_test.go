package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/transport"
)

func afterReset() time.Time { return time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC) }

func TestProviderRulesAndSessionReuse(t *testing.T) {
	for _, adapter := range []string{"anyrouter", "agentrouter", "newapi"} {
		t.Run(adapter, func(t *testing.T) {
			var mu sync.Mutex
			calls := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls[r.Method+" "+r.URL.Path]++
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/user/login":
					if _, err := r.Cookie("session"); err == nil || r.Header.Get("Authorization") != "" {
						t.Error("fresh login retained site authentication")
					}
					if _, err := r.Cookie("cf_clearance"); err != nil {
						t.Error("fresh login discarded challenge clearance")
					}
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "fresh", Path: "/"})
					fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
				case "/api/user/self":
					if _, err := r.Cookie("session"); err != nil {
						w.WriteHeader(401)
						fmt.Fprint(w, `{"success":false,"message":"session expired"}`)
						return
					}
					fmt.Fprint(w, `{"success":true,"data":{"id":1,"quota":100}}`)
				case "/api/user/checkin":
					if r.Method == "GET" {
						fmt.Fprint(w, `{"success":true,"data":{"stats":{"checked_in_today":false}}}`)
					} else {
						fmt.Fprint(w, `{"success":true,"data":{"quota_awarded":20}}`)
					}
				case "/":
					fmt.Fprint(w, "<html>home</html>")
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			site := domain.Templates()[2].Site
			site.Adapter, site.BaseURL = adapter, server.URL
			c := domain.Credentials{Method: "password", Username: "user", Password: "password", APIUser: "1", CookieHeader: "session=imported; cf_clearance=clearance", Token: "old-token"}
			r, saved := (&Runner{Factory: transport.Factory{}, Now: afterReset}).Execute(context.Background(), site, c)
			if !r.Complete() || saved == nil {
				t.Fatalf("result=%+v saved=%v", r, saved != nil)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls["GET /api/user/logout"] != 0 {
				t.Fatal("invalidated the user's imported session on the server")
			}
			if adapter == "agentrouter" {
				if calls["POST /api/user/login"] != 1 || r.Evidence != "fresh_login" {
					t.Fatalf("agent must log in exactly once: %+v %+v", calls, r)
				}
			} else if calls["POST /api/user/login"] != 0 {
				t.Fatal("valid saved session should not log in again")
			}
			if adapter == "newapi" && calls["POST /api/user/checkin"] != 1 {
				t.Fatal("ordinary New API must actually check in")
			}
			if adapter != "newapi" && calls["POST /api/user/checkin"] != 0 {
				t.Fatal("visit/login sites must not submit a check-in request")
			}
			if adapter == "anyrouter" && (calls["GET /"] != 1 || r.Evidence != "authenticated_visit") {
				t.Fatalf("AnyRouter must visit and verify: %+v %+v", calls, r)
			}
		})
	}
}

func TestResetBoundary(t *testing.T) {
	for _, adapter := range []string{"anyrouter", "agentrouter"} {
		site := domain.Templates()[0].Site
		site.Adapter = adapter
		runner := Runner{Now: func() time.Time { return afterReset().Add(-time.Hour - time.Nanosecond) }}
		r, _ := runner.Execute(context.Background(), site, domain.Credentials{Method: "password"})
		if r.Complete() || r.Evidence != "before_reset" || r.BlocksSchedule() {
			t.Fatalf("pre-reset run must not consume today's success or block its schedule: %+v", r)
		}
	}
}

func TestFailureDetailsAndRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/user/login" {
			fmt.Fprint(w, `{"success":false,"message":"密码错误 password=secret-value"}`)
			return
		}
		w.WriteHeader(401)
		fmt.Fprint(w, `{"success":false,"message":"session expired"}`)
	}))
	defer server.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = server.URL
	runner := Runner{Factory: transport.Factory{}}
	r, _ := runner.Execute(context.Background(), site, domain.Credentials{Method: "password", Username: "test", Password: "secret-value"})
	if r.Stage != "login" || r.HTTPStatus != 200 || r.Advice == "" || !strings.Contains(r.Message, "密码错误") || strings.Contains(r.Message, "secret-value") {
		t.Fatalf("missing or unsafe login details: %+v", r)
	}
	r, _ = runner.Execute(context.Background(), site, domain.Credentials{Method: "cookie"})
	if r.Stage != "session_check" || r.HTTPStatus != 401 || !strings.Contains(r.Message, "session expired") {
		t.Fatalf("missing session failure details: %+v", r)
	}
}

func TestNewAPIRechecksAmbiguousResponse(t *testing.T) {
	var mu sync.Mutex
	checked := false
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/user/self" {
			fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
		} else if r.Method == "POST" {
			checked = true
			posts++
			fmt.Fprint(w, "<html>response lost</html>")
		} else {
			fmt.Fprintf(w, `{"success":true,"data":{"stats":{"checked_in_today":%t}}}`, checked)
		}
	}))
	defer server.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = server.URL
	r, _ := (&Runner{Factory: transport.Factory{}}).Execute(context.Background(), site, domain.Credentials{Method: "cookie"})
	mu.Lock()
	defer mu.Unlock()
	if r.Evidence != "status_api" || !r.Complete() || posts != 1 {
		t.Fatalf("must confirm without repeating POST: %+v posts=%d", r, posts)
	}
}
