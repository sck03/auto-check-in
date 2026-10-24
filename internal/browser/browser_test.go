package browser

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/testsite"
	"auto-check-in/internal/transport"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

type diagnosticFactory struct {
	factory *Factory
	t       *testing.T
}

func TestDelayedPasswordFormAndSavedSession(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	fixture := testsite.New()
	defer fixture.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<button onclick="location.href='/email-login'">使用邮箱登录</button>`)
			return
		}
		if r.URL.Path != "/email-login" {
			fixture.Server.Config.Handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><script>setTimeout(()=>{
document.body.innerHTML='<input id="username"><input type="password"><button>登录</button>';
document.querySelector('button').onclick=async()=>{const r=await fetch('/api/user/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:document.querySelector('#username').value,password:document.querySelector('input[type=password]').value})});const d=await r.json();if(d.success){localStorage.setItem('user',JSON.stringify(d.data));location.href='/console'}};
},300)</script></body></html>`))
	}))
	defer server.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL, site.Transport = server.URL, "browser"
	runner := provider.Runner{Factory: factory}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, c := runner.Execute(ctx, site, domain.Credentials{Method: "password", Username: "delayed", Password: "test-password"})
	if !r.Complete() || c == nil {
		t.Fatalf("id-only delayed form failed: %+v", r)
	}
	r, _ = runner.Execute(ctx, site, *c)
	if r.Status != domain.AlreadyDone || fixture.Count("login:delayed") != 1 || fixture.Count("checkin:delayed") != 1 {
		t.Fatalf("saved session was not reused: %+v", r)
	}
}

func TestMintCapturesAndRestoresRewardToken(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/checkin/status" {
			if r.Header.Get("Authorization") != "Bearer local-reward-token" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"success":true,"can_spin":true}`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><title>Reward login</title></html>`)
	}))
	defer server.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	template, _ := domain.TemplateByID("x666")
	site := template.Site
	site.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := factory.OpenInteractive(ctx, site, domain.Credentials{Method: "cookie"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	driver := session.(*Session)
	if err := driver.run(ctx, cdpPoll("document.title==='Reward login'", nil), cdpEvaluate("localStorage.setItem('userToken','local-reward-token')", nil)); err != nil {
		t.Fatal(err)
	}
	response, err := session.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != 200 {
		t.Fatalf("reward token was not used: %+v %v", response, err)
	}
	c, err := session.Snapshot(ctx)
	if err != nil || c.Token != "local-reward-token" {
		t.Fatalf("reward token not captured: %v", err)
	}
	assertLoggedOut := func(current port.Session) {
		t.Helper()
		if err := current.(*Session).run(ctx, cdpEvaluate("localStorage.removeItem('userToken')", nil)); err != nil {
			t.Fatal(err)
		}
		response, err := current.Request(ctx, "GET", site.UserInfoPath, nil)
		if err != nil || response.Status != 401 {
			t.Fatalf("logged-out browser retained the reward token: HTTP %d, %v", response.Status, err)
		}
		saved, err := current.Snapshot(ctx)
		if err != nil || saved.Token != "" {
			t.Fatalf("logged-out snapshot retained the reward token: %v", err)
		}
	}
	assertLoggedOut(session)
	session.Close()
	restored, err := factory.Open(ctx, site, c)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	response, err = restored.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != 200 {
		t.Fatalf("reward token was not restored: %+v %v", response, err)
	}
	assertLoggedOut(restored)
	restored.Close()
	manual, err := factory.Open(ctx, site, domain.Credentials{Method: "cookie", Token: "local-reward-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer manual.Close()
	response, err = manual.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != 200 {
		t.Fatalf("explicit token should work without browser storage: HTTP %d, %v", response.Status, err)
	}
}

func (f diagnosticFactory) Open(ctx context.Context, site domain.Site, creds domain.Credentials) (port.Session, error) {
	session, err := f.factory.Open(ctx, site, creds)
	if err != nil {
		f.t.Logf("browser %s could not open session: %v", f.factory.config.Executable, err)
	}
	return session, err
}

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
	runner := provider.Runner{Factory: transport.Factory{Browser: diagnosticFactory{factory, t}}}
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
