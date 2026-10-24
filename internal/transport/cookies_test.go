package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auto-check-in/internal/domain"
)

func TestCookieRotationDeletionAndReopenUseCanonicalKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rotate":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "new", Domain: ".localhost", Path: "/"})
		case "/delete":
			http.SetCookie(w, &http.Cookie{Name: "session", Domain: "localhost", Path: "/", MaxAge: -1})
		default:
			fmt.Fprint(w, r.Header.Get("Cookie"))
		}
	}))
	defer server.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	ctx := context.Background()
	session, err := OpenHTTP(site, domain.Credentials{Method: "cookie", CookieHeader: "session=old"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Request(ctx, "GET", "/rotate", nil); err != nil {
		t.Fatal(err)
	}
	creds, _ := session.Snapshot(ctx)
	if len(creds.Cookies) != 1 || creds.Cookies[0].Value != "new" {
		t.Fatalf("stale cookie retained: %+v", creds.Cookies)
	}
	reopened, err := OpenHTTP(site, creds, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	response, err := reopened.Request(ctx, "GET", "/read", nil)
	if err != nil || string(response.Body) != "session=new" {
		t.Fatalf("wrong restored cookie: %s %v", response.Body, err)
	}
	if _, err := reopened.Request(ctx, "GET", "/delete", nil); err != nil {
		t.Fatal(err)
	}
	cleared, _ := reopened.Snapshot(ctx)
	if len(cleared.Cookies) != 0 {
		t.Fatalf("deleted cookie resurrected: %+v", cleared.Cookies)
	}
}

func TestCookieKeysIgnoreDomainCaseAndDefaultPath(t *testing.T) {
	if cookieKey(domain.Cookie{Name: "session", Domain: ".Example.COM"}) != cookieKey(domain.Cookie{Name: "session", Domain: "example.com", Path: "/"}) {
		t.Fatal("equivalent domain/path spellings have different keys")
	}
}
