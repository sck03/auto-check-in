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
	if err := s.runSite(ctx, cdpNavigateDOMReady(s.site.BaseURL+s.site.LoginPath)); err != nil {
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
	args := map[string]any{"selectors": s.site.Selectors, "username": s.creds.Username, "password": s.creds.Password}
	var clicked bool
	if s.creds.Method == "password" {
		// A normal email-login entry may precede the actual form.
		poll := "(()=>{const a=" + jsonJS(args) + ";" + loginDOMHelpers + "if(find(a.selectors.username)||find(userFallback))return true;const entry=find(a.selectors.login_entry)||[...document.querySelectorAll('button,a')].find(e=>visible(e)&&/邮箱或用户名|使用.*邮箱|Email or Username|Sign in with Email/i.test(e.textContent));if(entry&&!entry.dataset.checkinOpened){entry.dataset.checkinOpened='1';entry.click()}return false})()"
		if err := s.run(ctx, cdpPoll(poll, nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
			return port.Response{}, s.loginDiagnostic(ctx, "未找到可见的用户名输入框，请检查登录页面和选择器")
		}
		expr := "(()=>{const a=" + jsonJS(args) + ";" + loginDOMHelpers + "const u=find(a.selectors.username)||find(userFallback),p=find(a.selectors.password)||find('input[type=password]');if(!u||!p)return false;const set=(el,value)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}))};set(u,a.username);set(p,a.password);const submit=find(a.selectors.submit)||[...document.querySelectorAll('button')].find(e=>visible(e)&&/^(登录|登 录|登入|Sign in|Log in|Login)$/i.test(e.textContent.trim()));if(!submit||submit.disabled)return false;submit.click();return true})()"
		if err := s.run(ctx, cdpEvaluate(expr, &clicked)); err != nil || !clicked {
			return port.Response{}, s.loginDiagnostic(ctx, "无法提交登录表单：输入框或按钮不存在、按钮不可用；请检查选择器或在浏览器中完成必填项")
		}
	} else if s.creds.Method == "github" || s.creds.Method == "linuxdo" {
		if err := s.selectOAuthEntry(ctx, s.creds.Method); err != nil {
			if err == port.ErrUncertain {
				return port.Response{}, err
			}
			return port.Response{}, s.loginDiagnostic(ctx, "未找到可用的 "+s.creds.Method+" 授权入口，请检查站点配置")
		}
	} else {
		return port.Response{}, fmt.Errorf("登录方式 %s 无法自动提交：%w", s.creds.Method, port.ErrManualRequired)
	}
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	consentAttempted := false
	for {
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
			return port.Response{}, s.loginDiagnostic(ctx, "提交登录后 45 秒未收到登录接口响应；可能停留在授权页、验证页，或登录接口路径不匹配")
		case <-ticker.C:
			op, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := s.continueOAuth(op, &consentAttempted)
			cancel()
			if err != nil {
				return port.Response{}, err
			}
			// Do not repeatedly fetch user info or collect diagnostics while the
			// only page is still at an external identity provider.
			var atSite bool
			op, cancel = context.WithTimeout(ctx, 3*time.Second)
			err = s.runSite(op, cdpEvaluate("location.origin==="+jsonJS(s.site.BaseURL), &atSite))
			cancel()
			if err != nil || !atSite {
				continue
			}
			// A fast redirect can destroy the login response body before CDP reads
			// it. Verify the resulting session without ever resubmitting the login.
			op, cancel = context.WithTimeout(ctx, 3*time.Second)
			response, err := s.request(op, "GET", s.site.UserInfoPath, nil)
			cancel()
			if err == nil && response.Status >= 200 && response.Status < 300 && transport.UserID(response.Body) != "" {
				return response, nil
			}
		}
	}
}

const loginDOMHelpers = `const visible=e=>e&&e.getClientRects().length;
const find=q=>q?[...document.querySelectorAll(q)].find(visible):null;
const userFallback='input[autocomplete="username"],input[name="username"],input[name="email"],input[type="email"],input#username,input#email';`

// Read a small diagnostic, never a full page, request body, cookie or OAuth URL.
func (s *Session) loginDiagnostic(ctx context.Context, fallback string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var detail string
	expr := `(()=>{const visible=e=>e&&e.getClientRects().length;
const text=(document.title+' '+(document.body?.innerText||'').slice(0,12000)).toLowerCase();
let reason='';
if(/just a moment|verify you are human|验证您是真人|请验证您|checking your browser/.test(text)||[...document.querySelectorAll('iframe[src*="challenges.cloudflare.com"]')].some(visible))reason='Cloudflare / Turnstile 真人验证未完成，请在浏览器登录窗口处理';
else if([...document.querySelectorAll('input[autocomplete="one-time-code"],input[name="otp"],input[name="code"]')].some(visible))reason='站点要求验证码或二次验证，请在浏览器登录窗口完成';
else {const alert=[...document.querySelectorAll('[role="alert"],.semi-toast-content,.ant-message-notice-content,.el-message__content')].find(visible);if(alert)reason='页面提示：'+alert.innerText.slice(0,200)}
return (reason?reason+'；':'')+'当前页面 '+location.origin+location.pathname})()`
	op, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = s.run(op, cdpEvaluate(expr, &detail))
	return fmt.Errorf("%s；%s：%w", fallback, detail, port.ErrManualRequired)
}

func (s *Session) Visit(ctx context.Context, path string) error {
	if err := domain.ValidatePath(path); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runSite(ctx, cdpNavigateDOMReady(s.site.BaseURL+path))
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
