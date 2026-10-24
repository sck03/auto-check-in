package transport

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"auto-check-in/internal/domain"
)

func TestSharedPoolReusesConnectionsAndIsolatesAccountCookies(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != r.Header.Get("New-Api-User") {
			t.Error("account cookies crossed sessions", r.Header)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "rotated", Value: cookie.Value, Path: "/"})
		fmt.Fprintf(w, `{"success":true,"data":{"id":%q}}`, cookie.Value)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	factory, err := NewFactory(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = server.URL
	for _, id := range []string{"alice", "bob", "alice"} {
		session, err := factory.Open(context.Background(), site, domain.Credentials{Method: "cookie", APIUser: id, CookieHeader: "session=" + id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = session.Request(context.Background(), "GET", site.UserInfoPath, nil); err != nil {
			t.Fatal(err)
		}
		snapshot, err := session.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, cookie := range snapshot.Cookies {
			if cookie.Value != id {
				t.Fatal("snapshot crossed account boundary", snapshot)
			}
		}
		session.Close()
	}
	if connections.Load() != 1 {
		t.Fatal("sequential tasks did not reuse their connection", connections.Load())
	}
}
