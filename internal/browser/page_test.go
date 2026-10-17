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
		automatic    bool
	}{
		{"delayed_button", `setTimeout(()=>{button.disabled=false},400);button.onclick=()=>{fetch('/submit',{method:'POST'}).then(()=>{document.querySelector('#success').hidden=false})}`, domain.Succeeded, 1, false},
		{"redirect_after_click", `button.disabled=false;button.onclick=()=>{fetch('/submit',{method:'POST'}).then(()=>location.href='/done')}`, domain.Succeeded, 1, false},
		{"already_done", `document.querySelector('#already').hidden=false`, domain.AlreadyDone, 0, false},
		{"click_response_lost", `button.disabled=false;button.click=()=>{const xhr=new XMLHttpRequest();xhr.open('POST','/submit',false);xhr.send();throw new Error('response lost after submit')}`, domain.Unverified, 1, false},
		{"automatic_chinese", `button.textContent='立即签到';button.disabled=false;button.onclick=()=>fetch('/submit',{method:'POST'}).then(()=>{document.querySelector('#success').hidden=false})`, domain.Succeeded, 1, true},
		{"automatic_already", `button.textContent='今日已签到'`, domain.AlreadyDone, 0, true},
		{"automatic_ambiguous", `button.disabled=false;button.insertAdjacentHTML('afterend','<button>立即签到</button>')`, domain.NeedsAttention, 0, true},
		{"automatic_accessible_label", `button.textContent='★';button.setAttribute('aria-label','Check in');button.disabled=false;button.setAttribute('aria-disabled','true');setTimeout(()=>button.removeAttribute('aria-disabled'),400);button.onclick=()=>fetch('/submit',{method:'POST'}).then(()=>{document.querySelector('#success').hidden=false})`, domain.Succeeded, 1, true},
		{"automatic_hidden_duplicate", `button.disabled=false;button.insertAdjacentHTML('afterend','<button style="visibility:hidden">Check in</button><button inert>Check in</button>');button.onclick=()=>fetch('/submit',{method:'POST'}).then(()=>{document.querySelector('#success').hidden=false})`, domain.Succeeded, 1, true},
		{"automatic_redirect", `button.disabled=false;button.onclick=()=>fetch('/submit',{method:'POST'}).then(()=>location.href='/done')`, domain.Succeeded, 1, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var clicks atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/submit" {
					clicks.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if r.URL.Path == "/done" {
					fmt.Fprint(w, `<div id="success" role="status">签到成功</div>`)
					return
				}
				fmt.Fprint(w, `<button id="checkin" disabled>Check in</button><div id="success" role="status" hidden>签到成功</div><div id="already" hidden>Already done</div><script>const button=document.querySelector('#checkin');`+scenario.script+`</script>`)
			}))
			defer server.Close()
			template, _ := domain.TemplateByID("newapi")
			site := template.Site
			site.BaseURL, site.Adapter, site.Transport, site.CheckInPath = server.URL, "browser", "browser", "/daily"
			if !scenario.automatic {
				site.Selectors.CheckIn, site.Selectors.Success, site.Selectors.Already = "#checkin", "#success", "#already"
			}
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

func TestPageRecognitionRejectsUnsafeOrUnclearMatches(t *testing.T) {
	if os.Getenv("CHECKIN_BROWSER_TEST") != "1" {
		t.Skip("set CHECKIN_BROWSER_TEST=1 for real Chromium integration")
	}
	factory := New(Config{Executable: os.Getenv("CHECKIN_BROWSER_PATH"), TempDir: t.TempDir(), NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true"})
	defer factory.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body></body></html>")
	}))
	defer server.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := factory.open(ctx, site, domain.Credentials{Method: "cookie"}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, scenario := range []struct{ name, html, selector, origin, want string }{
		{"login_is_not_checkin", `<button>Sign in</button><button>登录</button>`, "", server.URL, ""},
		{"generic_success_is_not_checkin", `<div role="status">操作成功</div>`, "", server.URL, ""},
		{"history_is_not_today", `<div>签到成功</div><time>昨天已签到</time>`, "", server.URL, ""},
		{"hidden_state", `<div role="status" style="opacity:0">签到成功</div>`, "", server.URL, ""},
		{"disabled_fieldset", `<fieldset disabled><button>签到</button></fieldset>`, "", server.URL, ""},
		{"cross_origin_link", `<a href="https://other.test/checkin">签到</a>`, "", server.URL, ""},
		{"cross_origin_document", `<button>签到</button>`, "", "https://other.test", "foreign"},
		{"invalid_selector", `<button>签到</button>`, "[", server.URL, "invalid"},
		{"explicit_disambiguation", `<button id="chosen">签到</button><button>签到</button>`, "#chosen", server.URL, "ready"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			selectors := domain.Selectors{CheckIn: scenario.selector}
			expr := "(()=>{document.body.innerHTML=" + jsonJS(scenario.html) + ";return (" + pageCheckInScript + ")(" + jsonJS(selectors) + "," + jsonJS(scenario.origin) + ").inspect()})()"
			var state string
			if err := session.run(ctx, cdpEvaluate(expr, &state)); err != nil {
				t.Fatal(err)
			}
			if state != scenario.want {
				t.Fatalf("state=%q want=%q", state, scenario.want)
			}
		})
	}
}
