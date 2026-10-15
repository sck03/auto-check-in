package browser

import (
	"context"
	"os"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/testsite"
)

func TestRestoredStorageDoesNotOverwriteSiteChanges(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("real browser integration disabled")
	}
	fixture := testsite.New()
	defer fixture.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = fixture.Server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := factory.OpenInteractive(ctx, site, domain.Credentials{
		Method: "cookie", Storage: map[string]map[string]string{site.BaseURL: {"user": `{"id":"old-user"}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	driver := session.(*Session)
	if err := driver.run(ctx, cdpPoll("location.pathname === '/login' && document.readyState === 'complete'", nil)); err != nil {
		t.Fatal(err)
	}
	readUser := func(want string) {
		t.Helper()
		var got string
		if err := driver.run(ctx, cdpEvaluate("localStorage.getItem('user') || ''", &got)); err != nil || got != want {
			t.Fatalf("stored user = %q, want %q; error = %v", got, want, err)
		}
	}
	readUser(`{"id":"old-user"}`)
	if err := driver.run(ctx, cdpEvaluate(`localStorage.setItem('user', '{"id":"new-user"}')`, nil), cdpNavigate(site.BaseURL+"/console")); err != nil {
		t.Fatal(err)
	}
	readUser(`{"id":"new-user"}`)
	if err := driver.run(ctx, cdpEvaluate("localStorage.removeItem('user')", nil), cdpNavigate(site.BaseURL+site.LoginPath)); err != nil {
		t.Fatal(err)
	}
	readUser("")
}
