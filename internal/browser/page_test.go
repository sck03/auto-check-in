package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/provider"
)

func TestGenericPageCheckIn(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	for _, scenario := range []struct {
		name, script string
		want         string
		clicks       int32
	}{
		{"delayed_button", `setTimeout(()=>{button.disabled=false},400);button.onclick=()=>{fetch('/submit',{method:'POST'}).then(()=>{document.querySelector('#success').hidden=false})}`, domain.Succeeded, 1},
		{"redirect_after_click", `button.disabled=false;button.onclick=()=>{fetch('/submit',{method:'POST'}).then(()=>location.href='/done')}`, domain.Succeeded, 1},
		{"already_done", `document.querySelector('#already').hidden=false`, domain.AlreadyDone, 0},
		{"click_response_lost", `button.disabled=false;button.click=()=>{const xhr=new XMLHttpRequest();xhr.open('POST','/submit',false);xhr.send();throw new Error('response lost after submit')}`, domain.Unverified, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var clicks atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/submit" {
					clicks.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				if r.URL.Path == "/done" {
					fmt.Fprint(w, `<div id="success">Success</div>`)
					return
				}
				fmt.Fprint(w, `<button id="checkin" disabled>Check in</button><div id="success" hidden>Success</div><div id="already" hidden>Already done</div><script>const button=document.querySelector('#checkin');`+scenario.script+`</script>`)
			}))
			defer server.Close()
			template, _ := domain.TemplateByID("newapi")
			site := template.Site
			site.BaseURL, site.Adapter, site.Transport, site.CheckInPath = server.URL, "browser", "browser", "/daily"
			site.Selectors.CheckIn, site.Selectors.Success, site.Selectors.Already = "#checkin", "#success", "#already"
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, _ := (&provider.Runner{Factory: factory}).Execute(ctx, site, domain.Credentials{Method: "cookie"})
			if result.Status != scenario.want || result.Retryable {
				t.Fatalf("unexpected result: %+v", result)
			}
			if clicks.Load() != scenario.clicks {
				t.Fatalf("wrong submit count: %d", clicks.Load())
			}
		})
	}
}
