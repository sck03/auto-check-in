package browser

import (
	"context"
	"fmt"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func (s *Session) NativeWindow() bool    { return s.nativeWindow }
func (s *Session) Done() <-chan struct{} { return s.ctx.Done() }

// An observed successful login response is authoritative even when the site's
// WAF intercepts a later user-info request. Never infer success from old storage:
// require this session's response, matching current page identity and a cookie.
func (s *Session) ConfirmedLogin(ctx context.Context) bool {
	id := s.confirmedUser.Load()
	if id == nil || *id == "" {
		return false
	}
	var matches bool
	expr := "(()=>{if(location.origin!==" + jsonJS(s.site.BaseURL) + ")return false;try{return String(JSON.parse(localStorage.getItem('user')||'{}').id)===" + jsonJS(*id) + "}catch{return false}})()"
	if err := s.runSite(ctx, cdpEvaluate(expr, &matches)); err != nil || !matches {
		return false
	}
	cookies, err := cdp.Call(ctx, chromedp.FromContext(s.ctx).Target, network.GetCookies, network.GetCookiesParams{URLs: []string{s.site.BaseURL}})
	if err != nil {
		return false
	}
	for _, cookie := range cookies.Cookies {
		if cookie.Name == "session" && cookie.Value != "" {
			return true
		}
	}
	return false
}

func (f *Factory) allocateSession(ctx context.Context, site domain.Site, c domain.Credentials, visible bool) (*Session, error) {
	var browserCtx context.Context
	var cancel context.CancelFunc
	var cleanup func()
	if visible {
		process, err := f.startProcess(ctx, true)
		if err != nil {
			return nil, err
		}
		browserCtx = process.ctx
		cancel, cleanup = process.Close, func() {}
	} else {
		root, err := f.acquirePool(ctx)
		if err != nil {
			return nil, err
		}
		browserCtx, cancel = chromedp.NewContext(root, chromedp.WithNewBrowserContext())
		cleanup = f.releasePool
	}
	stop := context.AfterFunc(ctx, cancel)
	s := &Session{ctx: browserCtx, cancel: func() { stop(); cancel() }, site: site, creds: c,
		nativeWindow: visible, browserToken: c.Storage[site.BaseURL]["userToken"] != "", loginResponses: make(chan port.Response, 8)}
	s.cleanup = func() {
		cleanup()
		if visible {
			f.poolMu.Lock()
			delete(f.visible, s)
			f.poolMu.Unlock()
		}
		f.releaseSlot()
	}
	if visible {
		f.poolMu.Lock()
		if f.closed {
			f.poolMu.Unlock()
			s.cancel()
			cleanup()
			return nil, fmt.Errorf("浏览器执行器已关闭")
		}
		f.visible[s] = struct{}{}
		f.starts++
		f.poolMu.Unlock()
	}
	return s, nil
}
