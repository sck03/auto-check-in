package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
)

func TestWrongIdentityCannotReplaceSavedSessionOrSubmitCheckIn(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(fmt.Sprint("fresh=", fresh), func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" && r.URL.Path != "/api/user/login" {
					posts++
				}
				fmt.Fprint(w, `{"success":true,"data":{"id":2,"checked_in":true}}`)
			}))
			defer server.Close()
			site := domain.Templates()[2].Site
			site.BaseURL = server.URL
			c := domain.Credentials{Method: "cookie", APIUser: "1", CookieHeader: "session=original"}
			if fresh {
				site.Adapter = "agentrouter"
				c.Method, c.Username, c.Password = "password", "one", "fixture-password"
			}
			runner := Runner{Factory: transport.Factory{}, Now: afterReset}
			for range 2 {
				result, updated := runner.Execute(context.Background(), site, c)
				if result.Status != domain.NeedsAttention || updated != nil || posts != 0 {
					t.Fatalf("identity guard failed: status=%s updated=%v check-in posts=%d", result.Status, updated != nil, posts)
				}
			}
		})
	}
}

type identityChangingSession struct{ port.Session }

func (*identityChangingSession) Close() {}
func (*identityChangingSession) Request(context.Context, string, string, any) (port.Response, error) {
	return port.Response{Status: 200, Body: []byte(`{"success":true,"data":{"id":1,"checked_in":true}}`)}, nil
}
func (*identityChangingSession) Snapshot(context.Context) (domain.Credentials, error) {
	return domain.Credentials{Method: "cookie", APIUser: "2"}, nil
}

type identityChangingFactory struct{}

func (identityChangingFactory) Open(context.Context, domain.Site, domain.Credentials) (port.Session, error) {
	return &identityChangingSession{}, nil
}

func TestIdentityChangeDuringExecutionDoesNotConfirmOldAccount(t *testing.T) {
	runner := Runner{Factory: identityChangingFactory{}}
	result, updated := runner.Execute(context.Background(), domain.Templates()[2].Site, domain.Credentials{Method: "cookie", APIUser: "1"})
	if result.Status != domain.Unverified || updated != nil {
		t.Fatalf("identity change confirmed old account: %+v saved=%v", result, updated != nil)
	}
}
