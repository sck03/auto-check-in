package browser

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"github.com/chromedp/chromedp"
)

//go:embed page_checkin.js
var pageCheckInScript string

// Inspect the configured check-in page before choosing whether to log in.
// Generic pages need not implement a JSON user-info API. Only an actionable
// check-in/result or an explicit login form is evidence for this decision.
func (s *Session) PreparePageCheckIn(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.navigateCheckIn(ctx); err != nil {
		return false, err
	}
	driver := s.pageDriver()
	login := `const visible=e=>e&&!e.closest('[hidden],[inert],[aria-hidden="true"]')&&e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});
const find=q=>q?[...document.querySelectorAll(q)].find(visible):null;
const a=` + jsonJS(s.site.Selectors) + `;
if(find(a.password||'input[type="password"]')&&(find(a.username)||find('input[autocomplete="username"],input[name="username"],input[name="email"],input[type="email"],input#username,input#email')))return 'login';
if(find(a.login_entry))return 'login';`
	if s.creds.Method == "github" || s.creds.Method == "linuxdo" {
		login += "if(" + s.oauthDriver(s.creds.Method) + ".inspect('entry')==='ready')return 'login';"
	}
	expr := "(()=>{const state=" + driver + ".inspect();if(state)return state;try{" + login + "}catch{return 'invalid'}return ''})()"
	var state string
	if err := s.pollPage(ctx, expr, &state, 20*time.Second); err != nil {
		return false, s.loginDiagnostic(ctx, "未确认签到页面或登录表单，请检查页面和选择器")
	}
	if state == "login" {
		return false, nil
	}
	if state != "ready" && state != "success" && state != "already" {
		return false, pageStateError(state)
	}
	if err := s.checkPageIdentity(ctx); err != nil {
		return false, err
	}
	s.pagePrepared = true
	return true, nil
}

func (s *Session) pageDriver() string {
	return "(" + pageCheckInScript + ")(" + jsonJS(s.site.Selectors) + "," + jsonJS(s.site.BaseURL) + ")"
}

// A freshly captured login response can precede the SPA's final redirect. Retry
// only this read-only navigation when that redirect aborts it, never a click.
func (s *Session) navigateCheckIn(ctx context.Context) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.runSite(ctx, cdpNavigateDOMReady(s.site.BaseURL+s.site.CheckInPath))
		if err == nil || !strings.Contains(err.Error(), "net::ERR_ABORTED") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return err
}

func (s *Session) checkPageIdentity(ctx context.Context) error {
	expected := s.creds.APIUser
	if err := s.refreshUserID(ctx); err != nil {
		return err
	}
	if expected != "" && s.creds.APIUser != expected {
		return fmt.Errorf("页面用户身份与账号不符，未提交签到：%w", port.ErrManualRequired)
	}
	return nil
}

func pageStateError(state string) error {
	reason := map[string]string{
		"ambiguous": "页面存在多个签到候选按钮，请填写唯一的签到按钮 CSS 选择器",
		"invalid":   "CSS 选择器语法无效，请检查站点配置",
		"foreign":   "签到页面跳转到了其他站点，请完成登录并检查签到页面地址",
	}[state]
	if reason == "" {
		reason = "签到按钮已消失或不可点击，请检查页面及登录状态"
	}
	return fmt.Errorf("%s：%w", reason, port.ErrManualRequired)
}

func (s *Session) PageCheckIn(ctx context.Context) (domain.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pagePrepared {
		if err := s.navigateCheckIn(ctx); err != nil {
			return domain.Result{}, err
		}
	}
	s.pagePrepared = false
	driver := s.pageDriver()
	var state string
	if err := s.pollPage(ctx, driver+".inspect()", &state, 20*time.Second); err != nil {
		return domain.Result{}, s.loginDiagnostic(ctx, "未找到可用的签到按钮或结果标识；请确认签到页面，必要时填写 CSS 选择器")
	}
	if state == "ready" {
		if err := s.checkPageIdentity(ctx); err != nil {
			return domain.Result{}, err
		}
		// Recheck state and target atomically. Never replay a click if its CDP
		// response is lost: the request may already have reached the website.
		if err := s.run(ctx, cdpEvaluate(driver+".click()", &state)); err != nil {
			return domain.Result{}, port.ErrUncertain
		}
	}
	evidence := "configured_page_state"
	if s.site.Selectors.Success == "" && s.site.Selectors.Already == "" {
		evidence = "recognized_page_state"
	}
	if state == "already" || state == "success" {
		return domain.Result{Status: domain.AlreadyDone, Message: "页面已显示签到完成状态", Evidence: evidence}, nil
	}
	if state != "clicked" {
		return domain.Result{}, pageStateError(state)
	}
	check := "(()=>{const state=" + driver + ".inspect();return state==='success'||state==='already'?state:''})()"
	if err := s.pollPage(ctx, check, &state, 25*time.Second); err != nil {
		return domain.Result{Status: domain.Unverified, Message: "已点击签到，但未发现明确的签到结果，请在站点核对", Evidence: "page_result_missing"}, nil
	}
	status := domain.Succeeded
	if state == "already" {
		status = domain.AlreadyDone
	}
	return domain.Result{Status: status, Message: "页面已显示明确的签到结果", Evidence: evidence}, nil
}

// Only read-only predicates may resume after navigation replaces the JS context.
func (s *Session) pollPage(ctx context.Context, expression string, out any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := s.run(ctx, cdpPoll(expression, out, chromedp.WithPollingInterval(200*time.Millisecond), chromedp.WithPollingTimeout(0)))
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "Cannot find context with specified id") &&
			!strings.Contains(err.Error(), "Execution context was destroyed") &&
			!strings.Contains(err.Error(), "Inspected target navigated or closed") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
