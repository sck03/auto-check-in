package browser

import (
	"context"
	"fmt"
	"math"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func (s *Session) Login(ctx context.Context) (port.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.site.Adapter == "agentrouter" && s.site.LogoutPath != "" {
		if _, err := s.request(ctx, "GET", s.site.LogoutPath, nil); err != nil {
			return port.Response{}, err
		}
		if err := s.run(ctx, cdpEvaluate("localStorage.removeItem('user')", nil)); err != nil {
			return port.Response{}, err
		}
	}
	if err := s.run(ctx, cdpNavigate(s.site.BaseURL+s.site.LoginPath)); err != nil {
		return port.Response{}, err
	}
	for {
		select {
		case <-s.loginResponses:
			continue
		default:
			goto drained
		}
	}
drained:
	args := map[string]any{"selectors": s.site.Selectors, "username": s.creds.Username, "password": s.creds.Password, "method": s.creds.Method}
	var clicked bool
	if s.creds.Method == "password" {
		// A normal email-login entry may precede the actual form.
		entry := "(()=>{const a=" + jsonJS(args) + ";const visible=e=>e&&e.getClientRects().length;const user=[...document.querySelectorAll(a.selectors.username)].find(visible);if(user)return true;const entry=a.selectors.login_entry?document.querySelector(a.selectors.login_entry):[...document.querySelectorAll('button,a')].find(e=>visible(e)&&/邮箱或用户名|使用.*邮箱|Email or Username|Sign in with Email/i.test(e.textContent));if(entry)entry.click();return !!entry})()"
		_ = s.run(ctx, cdpEvaluate(entry, &clicked))
		poll := "(()=>{const a=" + jsonJS(args) + ";return [...document.querySelectorAll(a.selectors.username)].some(e=>e.getClientRects().length)})()"
		if err := s.run(ctx, cdpPoll(poll, nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
			return port.Response{}, port.ErrManualRequired
		}
		expr := "(()=>{const a=" + jsonJS(args) + ";const visible=e=>e&&e.getClientRects().length;const find=q=>[...document.querySelectorAll(q)].find(visible);const u=find(a.selectors.username),p=find(a.selectors.password);if(!u||!p)return false;const set=(el,value)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}))};set(u,a.username);set(p,a.password);const submit=find(a.selectors.submit)||[...document.querySelectorAll('button')].find(e=>visible(e)&&/^(登录|登 录|Sign in|Log in)$/i.test(e.textContent.trim()));if(!submit)return false;submit.click();return true})()"
		if err := s.run(ctx, cdpEvaluate(expr, &clicked)); err != nil || !clicked {
			return port.Response{}, port.ErrManualRequired
		}
	} else if s.creds.Method == "github" || s.creds.Method == "linuxdo" {
		expr := "(()=>{const a=" + jsonJS(args) + ";const visible=e=>e&&e.getClientRects().length;const selector=a.method==='github'?a.selectors.github:a.selectors.linuxdo;let button=selector?[...document.querySelectorAll(selector)].find(visible):null;const pattern=a.method==='github'?/GitHub/i:/Linux.?DO/i;if(!button)button=[...document.querySelectorAll('button,a')].find(e=>visible(e)&&pattern.test(e.textContent));if(!button)return false;button.click();return true})()"
		if err := s.run(ctx, cdpEvaluate(expr, &clicked)); err != nil || !clicked {
			return port.Response{}, port.ErrManualRequired
		}
	} else {
		return port.Response{}, port.ErrManualRequired
	}
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case response := <-s.loginResponses:
		if id := transport.UserID(response.Body); id != "" {
			s.creds.APIUser = id
		}
		return response, nil
	case <-ctx.Done():
		return port.Response{}, port.ErrUncertain
	case <-s.ctx.Done():
		return port.Response{}, port.ErrUncertain
	case <-timer.C:
		return port.Response{}, port.ErrManualRequired
	}
}

func (s *Session) PageCheckIn(ctx context.Context) (domain.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.run(ctx, cdpNavigate(s.site.BaseURL+s.site.CheckInPath)); err != nil {
		return domain.Result{}, err
	}
	args := jsonJS(s.site.Selectors)
	check := "(()=>{const s=" + args + ";const visible=q=>q&&[...document.querySelectorAll(q)].some(e=>e.getClientRects().length);if(visible(s.already))return 'already';if(visible(s.success))return 'success';return ''})()"
	var state string
	if err := s.run(ctx, cdpEvaluate(check, &state)); err != nil {
		return domain.Result{}, err
	}
	if state != "" {
		return domain.Result{Status: domain.AlreadyDone, Message: "页面已显示完成状态", Evidence: "configured_page_state"}, nil
	}
	var clicked bool
	click := "(()=>{const s=" + args + ";const el=[...document.querySelectorAll(s.checkin)].find(e=>e.getClientRects().length&&!e.disabled);if(!el)return false;el.click();return true})()"
	if err := s.run(ctx, cdpEvaluate(click, &clicked)); err != nil || !clicked {
		return domain.Result{}, port.ErrManualRequired
	}
	if err := s.run(ctx, cdpPoll(check, &state, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		return domain.Result{Status: domain.Unverified, Message: "已点击签到，但页面未出现配置的结果标识", Evidence: "page_result_missing"}, nil
	}
	status := domain.Succeeded
	if state == "already" {
		status = domain.AlreadyDone
	}
	return domain.Result{Status: status, Message: "页面已显示配置的签到结果", Evidence: "configured_page_state"}, nil
}

func (s *Session) Screenshot(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		var data []byte
		err := s.run(ctx, cdpCaptureScreenshot(&data))
		if err == nil {
			return data, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-time.After(50 * time.Millisecond):
			// Navigation and a just-closed OAuth popup can briefly have no frame.
		}
	}
}
func (s *Session) Interact(ctx context.Context, event port.Interaction) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	switch event.Type {
	case "click":
		if math.IsNaN(event.X) || math.IsNaN(event.Y) || event.X < 0 || event.X > 1200 || event.Y < 0 || event.Y > 800 {
			return fmt.Errorf("点击坐标超出视口")
		}
		return s.run(ctx, cdpMouseClickXY(event.X, event.Y))
	case "text":
		if len(event.Text) > 4096 {
			return fmt.Errorf("输入内容过长")
		}
		return s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
			_, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, input.InsertText, input.InsertTextParams{Text: event.Text})
			return e
		}))
	case "key":
		keys := map[string]string{"Enter": kb.Enter, "Tab": kb.Tab, "Backspace": kb.Backspace, "Escape": kb.Escape, "ArrowUp": kb.ArrowUp, "ArrowDown": kb.ArrowDown, "ArrowLeft": kb.ArrowLeft, "ArrowRight": kb.ArrowRight, "Delete": kb.Delete}
		if key, ok := keys[event.Key]; ok {
			return s.run(ctx, cdpKeyEvent(key))
		}
		if event.Key == "SelectAll" {
			return s.run(ctx, cdpKeyEvent("a", chromedp.KeyModifiers(chromedp.ModifierCtrl)))
		}
		return fmt.Errorf("不支持的按键")
	case "scroll":
		if math.Abs(event.DeltaY) > 2000 || math.IsNaN(event.DeltaY) {
			return fmt.Errorf("滚动量超出限制")
		}
		return s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
			_, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: 600, Y: 400, DeltaY: event.DeltaY})
			return e
		}))
	case "reload":
		return s.run(ctx, cdpReload())
	case "focus":
		return s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
			_, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.BringToFront, cdp.Empty{})
			return err
		}))
	}
	return fmt.Errorf("不支持的浏览器操作")
}
