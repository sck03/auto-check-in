package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"auto-check-in/internal/app"
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
	"auto-check-in/internal/transport"
)

type noBrowser struct{}

func (noBrowser) Available() bool { return false }
func (noBrowser) Open(context.Context, domain.Site, domain.Credentials) (port.Session, error) {
	return nil, port.ErrBrowserUnavailable
}
func (noBrowser) OpenInteractive(context.Context, domain.Site, domain.Credentials) (port.InteractiveSession, error) {
	return nil, port.ErrBrowserUnavailable
}

func testAPI(t *testing.T) (*httptest.Server, *app.Service) {
	t.Helper()
	vault, _ := security.NewVault(bytes.Repeat([]byte{4}, 32))
	repo, err := store.Open(filepath.Join(t.TempDir(), "checkin.db"), vault)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := app.New(ctx, repo, &provider.Runner{Factory: transport.Factory{}}, noBrowser{}, true)
	server := httptest.NewServer(New(service, "test-private-token", fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>UI</h1>")}}, Config{Version: "test", AllowedOrigins: []string{"http://wails.localhost"}}))
	t.Cleanup(func() { server.Close(); cancel(); service.CloseLogins(); repo.Close() })
	return server, service
}
func call(t *testing.T, server *httptest.Server, method, path, token, origin, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}
func TestAuthenticationAndOrigins(t *testing.T) {
	server, _ := testAPI(t)
	for _, path := range []string{"/api/v1/accounts", "/api/v1/sites", "/api/v1/settings", "/api/v1/sessions/missing", "/api/v1/accounts?token=test-private-token"} {
		status, _ := call(t, server, "GET", path, "", "", "")
		if status != 401 {
			t.Fatal(path, status)
		}
	}
	status, _ := call(t, server, "GET", "/api/v1/meta", "test-private-token", "https://evil.invalid", "")
	if status != 403 {
		t.Fatal(status)
	}
	status, _ = call(t, server, "GET", "/api/v1/meta", "test-private-token", "http://wails.localhost", "")
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = call(t, server, "GET", "/healthz", "", "", "")
	if status != 200 {
		t.Fatal(status)
	}
}
func TestAccountProjectionAndUnknownFields(t *testing.T) {
	server, service := testAPI(t)
	ctx := context.Background()
	password, method, username := "never-echo-this-password", "password", "alice"
	_, err := service.SaveAccount(ctx, "", domain.AccountInput{Name: "account", SiteID: "anyrouter", Enabled: true, Schedule: domain.DefaultSchedule(), Credentials: &domain.CredentialPatch{Password: &password, Method: &method, Username: &username}})
	if err != nil {
		t.Fatal(err)
	}
	status, data := call(t, server, "GET", "/api/v1/accounts", "test-private-token", "", "")
	if status != 200 || bytes.Contains(data, []byte(password)) {
		t.Fatal(status, string(data))
	}
	var list []domain.Account
	if err = json.Unmarshal(data, &list); err != nil || len(list) != 1 || !list[0].Auth.HasPassword {
		t.Fatal(err, string(data))
	}
	status, _ = call(t, server, "POST", "/api/v1/accounts", "test-private-token", "", `{"unknown_secret":"do-not-accept"}`)
	if status != 400 {
		t.Fatal("unknown fields accepted", status)
	}
	status, _ = call(t, server, "POST", "/api/v1/sites", "test-private-token", "", strings.Repeat("a", 70000))
	if status != 400 {
		t.Fatal("oversize accepted", status)
	}
}
func TestPublicAssetFallbackDoesNotHideMissingAPIs(t *testing.T) {
	server, _ := testAPI(t)
	status, data := call(t, server, "GET", "/accounts", "", "", "")
	if status != 200 || !bytes.Contains(data, []byte("UI")) {
		t.Fatal(status, string(data))
	}
	status, _ = call(t, server, "GET", "/missing.js", "", "", "")
	if status != 404 {
		t.Fatal(status)
	}
	status, data = call(t, server, "GET", "/api/v1/missing", "test-private-token", "", "")
	if status != 404 || !bytes.Contains(data, []byte("not_found")) {
		t.Fatal(status, string(data))
	}
}
