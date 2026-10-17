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

func (s *Session) PageCheckIn(ctx context.Context) (domain.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.runSite(ctx, cdpNavigateDOMReady(s.site.BaseURL+s.site.CheckInPath)); err != nil {
		return domain.Result{}, err
	}
	driver := "(" + pageCheckInScript + ")(" + jsonJS(s.site.Selectors) + "," + jsonJS(s.site.BaseURL) + ")"
	var state string
	if err := s.pollPage(ctx, driver+".inspect()", &state, 20*time.Second); err != nil {
		return domain.Result{}, s.loginDiagnostic(ctx, "未找到可用的签到按钮或结果标识；请确认签到页面，必要时填写 CSS 选择器")
	}
	if state == "ready" {
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
		reasons := map[string]string{
			"ambiguous": "页面存在多个签到候选按钮，请填写唯一的签到按钮 CSS 选择器",
			"invalid":   "CSS 选择器语法无效，请检查站点配置",
			"foreign":   "签到页面跳转到了其他站点，请完成登录并检查签到页面地址",
		}
		reason := reasons[state]
		if reason == "" {
			reason = "签到按钮已消失或不可点击，请检查页面及登录状态"
		}
		return domain.Result{}, fmt.Errorf("%s：%w", reason, port.ErrManualRequired)
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
