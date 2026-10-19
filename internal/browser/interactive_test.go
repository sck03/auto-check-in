package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

func TestInteractiveFollowsOAuthPopup(t *testing.T) {
	for _, method := range []string{"github", "linuxdo"} {
		t.Run(method, func(t *testing.T) { testInteractiveOAuth(t, false, method, true) })
	}
}

func TestInteractiveOAuthSameTab(t *testing.T) {
	for _, method := range []string{"github", "linuxdo"} {
		t.Run(method, func(t *testing.T) { testInteractiveOAuth(t, false, method, false) })
	}
}

func TestNativeInteractiveOAuth(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("CHECKIN_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("native desktop login requires a Windows desktop")
	}
	for _, method := range []string{"github", "linuxdo"} {
		t.Run(method, func(t *testing.T) { testInteractiveOAuth(t, true, method, true) })
	}
}

func testInteractiveOAuth(t *testing.T, visible bool, method string, popup bool) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<title>OAuth sign in</title><form action=%s><input id=user name=user autofocus><button>Continue</button></form>",
			jsonJS(r.URL.Query().Get("return")))
	}))
	defer oauth.Close()
	siteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/self" {
			w.Header().Set("Content-Type", "application/json")
			if cookie, err := r.Cookie("session"); err == nil && cookie.Value == "example-user" && r.Header.Get("New-Api-User") == "example-user" {
				fmt.Fprint(w, `{"success":true,"data":{"id":"example-user"}}`)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"success":false}`)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/callback" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "example-user", Path: "/", HttpOnly: true})
			fmt.Fprint(w, `<title>Callback</title><script>localStorage.setItem('user','{"id":"example-user"}')</script>`)
			if popup {
				fmt.Fprint(w, "<script>setTimeout(()=>window.close(),200)</script>")
			}
			return
		}
		destination := "_self"
		if popup {
			destination = "_blank"
		}
		fmt.Fprintf(w, "<title>Original login</title><button disabled onclick=\"window.open('%s?return='+encodeURIComponent(location.origin+'/callback'),'%s')\">使用 %s 继续</button><script>setTimeout(()=>document.querySelector('button').disabled=false,300)</script>", oauth.URL, destination, method)
	}))
	defer siteServer.Close()
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true", InteractiveVisible: visible})
	defer factory.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = siteServer.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := factory.OpenInteractive(ctx, site, domain.Credentials{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.NativeWindow() != visible {
		t.Fatal("interactive presentation does not match the selected host")
	}
	other, err := factory.OpenInteractive(ctx, site, domain.Credentials{Method: "cookie"})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	driver := session.(*Session)
	if visible {
		var webdriver bool
		if err := driver.run(ctx, cdpEvaluate("navigator.webdriver", &webdriver)); err != nil || webdriver {
			t.Fatalf("human login window still uses automation mode: webdriver=%v err=%v", webdriver, err)
		}
	}
	if err := session.Interact(ctx, port.Interaction{Type: "focus"}); err != nil {
		t.Fatal(err)
	}
	title := ""
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if err := driver.run(ctx, cdpEvaluate("document.title", &title)); err == nil && title == "OAuth sign in" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if title != "OAuth sign in" {
		t.Fatalf("preview stayed on %q instead of following the OAuth popup", title)
	}
	var otherTitle string
	if err := other.(*Session).run(ctx, cdpEvaluate("document.title", &otherTitle)); err != nil || otherTitle != "Original login" {
		t.Fatalf("another account was affected by the popup: %q, %v", otherTitle, err)
	}
	// Opening the second native window can take focus after the first window
	// has already selected its provider. Model the user's click into the field.
	if err := session.Interact(ctx, port.Interaction{Type: "focus"}); err != nil {
		t.Fatal(err)
	}
	if err := session.Interact(ctx, port.Interaction{Type: "click", X: 80, Y: 20}); err != nil {
		t.Fatal(err)
	}
	if err := session.Interact(ctx, port.Interaction{Type: "text", Text: "example-user"}); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := driver.run(ctx, cdpEvaluate("document.querySelector('#user').value", &value)); err != nil || value != "example-user" {
		t.Fatalf("text did not reach OAuth page: value=%q, error=%v", value, err)
	}
	if err := session.Interact(ctx, port.Interaction{Type: "key", Key: "Enter"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(4 * time.Second)
	expectedTitle := "Callback"
	if popup {
		expectedTitle = "Original login"
	}
	for time.Now().Before(deadline) {
		if err := driver.run(ctx, cdpEvaluate("document.title", &title)); err == nil && title == expectedTitle {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if title != expectedTitle {
		t.Fatalf("preview did not return after closing OAuth popup: %q", title)
	}
	response, err := session.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil || response.Status != http.StatusOK {
		t.Fatalf("OAuth session verification failed: status=%d, error=%v", response.Status, err)
	}
	saved, err := session.Snapshot(ctx)
	if err != nil || saved.APIUser != "example-user" || !strings.Contains(saved.Storage[site.BaseURL]["user"], "example-user") {
		t.Fatalf("OAuth storage was not captured: id=%q, error=%v", saved.APIUser, err)
	}
	unrelated, err := other.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range unrelated.Cookies {
		if cookie.Name == "session" {
			t.Fatal("OAuth cookies leaked into another account")
		}
	}
}
