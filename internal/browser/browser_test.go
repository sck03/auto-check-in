package browser

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/testsite"
	"auto-check-in/internal/transport"
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func TestRealBrowserAccountIsolationAndLogin(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	fixture := testsite.New()
	defer fixture.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	if !factory.Available() {
		t.Fatal("Chromium is required for requested browser integration")
	}
	site := domain.Templates()[2].Site
	site.BaseURL = fixture.Server.URL
	site.Transport = "browser"
	runner := provider.Runner{Factory: transport.Factory{Browser: factory}}
	for _, name := range []string{"alice", "bob"} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		result, c := runner.Execute(ctx, site, domain.Credentials{Method: "password", Username: name, Password: "test-password"})
		cancel()
		if result.Status != domain.Succeeded {
			t.Fatalf("%s: %+v", name, result)
		}
		if c == nil || c.APIUser != name || len(c.Cookies) == 0 {
			t.Fatalf("wrong session for %s: %+v", name, c)
		}
		if fixture.Count("checkin:"+name) != 1 {
			t.Fatal("wrong checkin count")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := factory.OpenInteractive(ctx, site, domain.Credentials{Method: "cookie", CookieHeader: "session=alice", APIUser: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	frame, err := session.Screenshot(ctx)
	if err != nil || !bytes.HasPrefix(frame, []byte{0xff, 0xd8, 0xff}) {
		t.Fatal("screenshot unavailable", err)
	}
	if err = session.Interact(ctx, port.Interaction{Type: "reload"}); err != nil {
		t.Fatal(err)
	}
	response, err := session.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != 200 {
		t.Fatal("interactive session lost after screenshot/reload", err, response.Status)
	}
}
