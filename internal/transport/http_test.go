package transport

import (
	"auto-check-in/internal/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPNeverForwardsCredentialsToRedirect(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer origin.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = origin.URL
	session, err := OpenHTTP(site, domain.Credentials{Method: "cookie", APIUser: "alice", Token: "private", CookieHeader: "session=private"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	resp, err := session.Request(context.Background(), "GET", "/api/user/self", nil)
	if err != nil || resp.Status != 302 || hits.Load() != 0 {
		t.Fatal(resp.Status, hits.Load(), err)
	}
}
