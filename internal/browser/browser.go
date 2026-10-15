// Package browser owns disposable Chromium processes and exposes no CDP types
// outside the package.
package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/publicsuffix"
)

type Config struct {
	Executable, TempDir, ProxyURL string
	NoSandbox                     bool
	IdleTimeout                   time.Duration
}
type Factory struct {
	config              Config
	slots               chan struct{}
	poolMu              sync.Mutex
	poolCtx             context.Context
	poolCancel          context.CancelFunc
	poolAllocatorCancel context.CancelFunc
	poolProfile         string
	active              int
	starts              int
	idle                *time.Timer
	closed              bool
}

func New(config Config) *Factory {
	if config.Executable == "" {
		config.Executable = findBrowser()
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 60 * time.Second
	}
	return &Factory{config: config, slots: make(chan struct{}, 2)}
}
func (f *Factory) Available() bool {
	if f.config.Executable == "" {
		return false
	}
	info, err := os.Stat(f.config.Executable)
	return err == nil && !info.IsDir()
}
func findBrowser() string {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "msedge", "chrome"} {
		if p, e := exec.LookPath(name); e == nil {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		} {
			if _, e := os.Stat(p); e == nil {
				return p
			}
		}
	}
	return ""
}

func FindExecutable() string { return findBrowser() }

type Session struct {
	ctx            context.Context
	cancel         context.CancelFunc
	cleanup        func()
	closeOnce      sync.Once
	mu             sync.Mutex
	site           domain.Site
	creds          domain.Credentials
	loginResponses chan port.Response
}

func (f *Factory) Open(ctx context.Context, site domain.Site, c domain.Credentials) (port.Session, error) {
	return f.open(ctx, site, c, false)
}
func (f *Factory) OpenInteractive(ctx context.Context, site domain.Site, c domain.Credentials) (port.InteractiveSession, error) {
	return f.open(ctx, site, c, true)
}
func (f *Factory) open(ctx context.Context, site domain.Site, c domain.Credentials, interactive bool) (*Session, error) {
	if !f.Available() {
		return nil, port.ErrBrowserUnavailable
	}
	if interactive {
		select {
		case f.slots <- struct{}{}:
		default:
			return nil, port.ErrBrowserBusy
		}
	} else {
		select {
		case f.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	root, err := f.acquirePool()
	if err != nil {
		<-f.slots
		return nil, err
	}
	browserCtx, cancel := chromedp.NewContext(root, chromedp.WithNewBrowserContext())
	stop := context.AfterFunc(ctx, cancel)
	s := &Session{ctx: browserCtx, cancel: func() { stop(); cancel() }, site: site, creds: c, loginResponses: make(chan port.Response, 8)}
	s.cleanup = func() { f.releasePool(); <-f.slots }
	err = chromedp.Do(s.ctx)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.listenLoginResponses()
	openCtx, openCancel := context.WithTimeout(ctx, 45*time.Second)
	defer openCancel()
	err = s.run(openCtx, cdpActionFunc(func(ctx context.Context) error {
		_, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, network.Enable, network.EnableParams{})
		return e
	}))
	if err != nil {
		s.Close()
		return nil, err
	}
	if err = s.restore(openCtx); err != nil {
		s.Close()
		return nil, err
	}
	if err = s.run(openCtx, cdpNavigate(site.BaseURL+site.LoginPath)); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) run(ctx context.Context, actions ...cdpActionFunc) error {
	op, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer cancel()
	defer stop()
	for _, action := range actions {
		if err := action(op); err != nil {
			return err
		}
	}
	return nil
}
func jsonJS(value any) string { b, _ := json.Marshal(value); return string(b) }

func (s *Session) allowedDomain(domainName string) bool {
	d := strings.TrimPrefix(strings.ToLower(domainName), ".")
	u, _ := url.Parse(s.site.BaseURL)
	if d == u.Hostname() {
		return true
	}
	// Domain cookies may cover the configured host, but never a public suffix.
	suffix, _ := publicsuffix.PublicSuffix(d)
	if strings.HasSuffix(u.Hostname(), "."+d) && d != suffix {
		return true
	}
	switch s.creds.Method {
	case "github":
		return d == "github.com"
	case "linuxdo":
		return d == "linux.do" || d == "connect.linux.do"
	}
	return false
}
func (s *Session) restore(ctx context.Context) error {
	cookies := []*network.CookieParam{}
	u, _ := url.Parse(s.site.BaseURL)
	add := func(c domain.Cookie) {
		if c.Domain == "" {
			c.Domain = u.Hostname()
		}
		if !s.allowedDomain(c.Domain) || (c.Expires > 0 && c.Expires < float64(time.Now().Unix())) {
			return
		}
		p := &network.CookieParam{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly}
		if p.Path == "" {
			p.Path = "/"
		}
		switch c.SameSite {
		case "Strict", "Lax", "None":
			p.SameSite = network.CookieSameSite(c.SameSite)
		}
		if c.Expires > 0 {
			p.Expires = cdp.TimeSinceEpoch(c.Expires)
		}
		cookies = append(cookies, p)
	}
	for _, c := range s.creds.Cookies {
		add(c)
	}
	req := http.Request{Header: http.Header{"Cookie": []string{s.creds.CookieHeader}}}
	for _, c := range req.Cookies() {
		add(domain.Cookie{Name: c.Name, Value: c.Value, Domain: u.Hostname(), Path: "/", Secure: u.Scheme == "https"})
	}
	if len(cookies) > 0 {
		if err := s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
			_, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, network.SetCookies, network.SetCookiesParams{Cookies: cookies})
			return e
		})); err != nil {
			return err
		}
	}
	state := map[string]map[string]string{}
	for origin, values := range s.creds.Storage {
		if origin == s.site.BaseURL {
			state[origin] = values
		}
	}
	script := "(()=>{const state=" + jsonJS(state) + "; const values=state[location.origin]; if(values){for(const [key,value] of Object.entries(values)){localStorage.setItem(key,value)}}})()"
	return s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
		_, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: script})
		return err
	}))
}

func (s *Session) listenLoginResponses() {
	events := chromedp.Events(s.ctx, network.ResponseReceived)
	go func() {
		for ev, err := range events {
			if err != nil {
				return
			}
			u, err := url.Parse(ev.Response.URL)
			if err != nil {
				return
			}
			base, _ := url.Parse(s.site.BaseURL)
			if u.Scheme != base.Scheme || u.Host != base.Host {
				continue
			}
			if u.Path != s.site.LoginAPIPath && u.Path != "/api/oauth/github" && u.Path != "/api/oauth/linuxdo" {
				continue
			}
			go func(id network.RequestID, status int) {
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				defer cancel()
				// ResponseReceived precedes body completion. Retry this read-only
				// CDP operation until the body is available; never resend login.
				for ctx.Err() == nil {
					var body []byte
					err := s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
						r, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, network.GetResponseBody, network.GetResponseBodyParams{RequestID: id})
						body = r.Body
						return e
					}))
					if err == nil {
						if len(body) < 1<<20 {
							select {
							case s.loginResponses <- port.Response{Status: status, Body: body}:
							default:
							}
						}
						return
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(100 * time.Millisecond):
					}
				}
			}(ev.RequestID, int(ev.Response.Status))
		}
	}()
}

func (s *Session) Request(ctx context.Context, method, path string, body any) (port.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(ctx, method, path, body)
}
func (s *Session) request(ctx context.Context, method, path string, body any) (port.Response, error) {
	var out port.Response
	if err := domain.ValidatePath(path); err != nil {
		return out, err
	}
	_ = s.refreshUserID(ctx)
	headers := map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest"}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	if s.creds.APIUser != "" {
		headers[s.site.UserHeader] = s.creds.APIUser
	}
	if s.creds.Token != "" {
		headers["Authorization"] = "Bearer " + s.creds.Token
	}
	args := map[string]any{"origin": s.site.BaseURL, "path": path, "method": method, "headers": headers, "body": body}
	expression := "(async()=>{const a=" + jsonJS(args) + ";if(location.origin!==a.origin){return {status:0,body:''}};const r=await fetch(a.path,{method:a.method,headers:a.headers,credentials:'include',redirect:'error',body:a.body===null?undefined:JSON.stringify(a.body)});const body=await r.text();if(body.length>1048576)throw Error('response too large');return {status:r.status,body:body,retry:Number(r.headers.get('Retry-After'))||0}})()"
	var value struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Retry  int    `json:"retry"`
	}
	err := s.run(ctx, cdpEvaluate(expression, &value, chromedp.EvalAwaitPromise))
	if err != nil {
		if method != "GET" {
			return out, port.ErrUncertain
		}
		return out, err
	}
	if value.Status == 0 {
		return out, port.ErrManualRequired
	}
	out = port.Response{Status: value.Status, Body: []byte(value.Body), RetryAfterSeconds: value.Retry}
	if path == s.site.UserInfoPath || path == s.site.LoginAPIPath {
		if id := transport.UserID(out.Body); id != "" {
			s.creds.APIUser = id
		}
	}
	return out, nil
}

func (s *Session) refreshUserID(ctx context.Context) error {
	var id string
	expr := "(()=>{if(location.origin!==" + jsonJS(s.site.BaseURL) + ")return '';try{const u=JSON.parse(localStorage.getItem('user')||'{}');return u.id==null?'':String(u.id)}catch{return ''}})()"
	err := s.run(ctx, cdpEvaluate(expr, &id))
	if err == nil && id != "" {
		s.creds.APIUser = id
	}
	return err
}

func (s *Session) Snapshot(ctx context.Context) (domain.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cookies []*network.Cookie
	if err := s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
		current := chromedp.FromContext(ctx)
		r, e := cdp.Call(ctx, current.Browser, storage.GetCookies, storage.GetCookiesParams{BrowserContextID: current.BrowserContextID})
		cookies = r.Cookies
		return e
	})); err != nil {
		return s.creds, err
	}
	c := s.creds
	c.Cookies = []domain.Cookie{}
	c.CookieHeader = ""
	for _, v := range cookies {
		if !s.allowedDomain(v.Domain) {
			continue
		}
		c.Cookies = append(c.Cookies, domain.Cookie{Name: v.Name, Value: v.Value, Domain: v.Domain, Path: v.Path, Secure: v.Secure, HTTPOnly: v.HTTPOnly, SameSite: string(v.SameSite), Expires: v.Expires})
	}
	state := map[string]string{}
	expr := "(()=>{if(location.origin!==" + jsonJS(s.site.BaseURL) + ")return {};return Object.fromEntries(Object.entries(localStorage))})()"
	if err := s.run(ctx, cdpEvaluate(expr, &state)); err != nil {
		return c, err
	}
	c.Storage = map[string]map[string]string{s.site.BaseURL: state}
	_ = s.refreshUserID(ctx)
	c.APIUser = s.creds.APIUser
	return c, nil
}
func (s *Session) Close() {
	s.closeOnce.Do(func() { s.cancel(); s.cleanup() })
}

var _ port.BrowserFactory = (*Factory)(nil)
var _ port.InteractiveSession = (*Session)(nil)
