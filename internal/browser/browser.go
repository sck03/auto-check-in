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
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/publicsuffix"
)

type Config struct {
	Executable, TempDir, ProxyURL string
	NoSandbox                     bool
	InteractiveVisible            bool
	IdleTimeout                   time.Duration
}
type Factory struct {
	config    Config
	slots     chan struct{}
	lifetime  context.Context
	shutdown  context.CancelFunc
	poolMu    sync.Mutex
	teardowns sync.WaitGroup
	pool      *browserProcess
	starting  chan struct{}
	active    int
	starts    int
	idle      *time.Timer
	closed    bool
	visible   map[*Session]struct{}
}

func New(config Config) *Factory {
	if config.Executable == "" {
		config.Executable = findBrowser()
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 60 * time.Second
	}
	lifetime, shutdown := context.WithCancel(context.Background())
	return &Factory{config: config, slots: make(chan struct{}, 2), lifetime: lifetime, shutdown: shutdown, visible: make(map[*Session]struct{})}
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
	inputMu        sync.Mutex
	tabMu          sync.RWMutex
	tabs           map[target.ID]*sessionTab
	tabOrder       []target.ID
	activeTab      target.ID
	nativeWindow   bool
	site           domain.Site
	creds          domain.Credentials
	browserToken   bool
	loginResponses chan port.Response
}

func (f *Factory) Open(ctx context.Context, site domain.Site, c domain.Credentials) (port.Session, error) {
	return f.open(ctx, site, c, false)
}
func (f *Factory) OpenInteractive(ctx context.Context, site domain.Site, c domain.Credentials) (port.InteractiveSession, error) {
	return f.open(ctx, site, c, true)
}
func (f *Factory) open(ctx context.Context, site domain.Site, c domain.Credentials, interactive bool) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		case <-f.lifetime.Done():
			return nil, f.lifetime.Err()
		}
	}
	s, err := f.allocateSession(ctx, site, c, interactive && f.config.InteractiveVisible)
	if err != nil {
		<-f.slots
		return nil, err
	}
	initTimer := time.AfterFunc(30*time.Second, s.cancel)
	err = chromedp.Do(s.ctx)
	initTimer.Stop()
	if err != nil {
		s.Close()
		return nil, f.startupError(ctx, "账号浏览器上下文初始化", err)
	}
	s.trackTabs()
	s.listenLoginResponses(s.ctx)
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
	if c.UserAgent != "" && !s.nativeWindow {
		if err = s.run(openCtx, cdpActionFunc(func(ctx context.Context) error {
			_, e := cdp.Call(ctx, chromedp.FromContext(ctx).Target, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{UserAgent: c.UserAgent})
			return e
		})); err != nil {
			s.Close()
			return nil, err
		}
	}
	restored, err := s.restore(openCtx)
	if err != nil {
		s.Close()
		return nil, err
	}
	navigate := cdpNavigateDOMReady(site.BaseURL + site.LoginPath)
	if interactive {
		// Let the user interact while slow third-party assets are still loading.
		navigate = cdpNavigateAsync(site.BaseURL + site.LoginPath)
		if restored != "" {
			navigate = cdpNavigateDOMReady(site.BaseURL + site.LoginPath)
		}
	}
	if err = s.run(openCtx, navigate); err != nil {
		s.Close()
		return nil, err
	}
	if restored != "" {
		// Seed the first document only. Replaying storage on reload can undo a
		// completed login or resurrect the user that a logout just removed.
		if err = s.run(openCtx, func(ctx context.Context) error {
			_, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.RemoveScriptToEvaluateOnNewDocument, page.RemoveScriptToEvaluateOnNewDocumentParams{Identifier: restored})
			return err
		}); err != nil {
			s.Close()
			return nil, err
		}
	}
	if interactive && (c.Method == "github" || c.Method == "linuxdo") {
		// Do not hold up the visible window while a SPA renders its login form.
		// Cancellation/close also stops this bounded, one-shot provider selection.
		go func() { _ = s.selectOAuthEntry(s.ctx, c.Method) }()
	}
	return s, nil
}

func (s *Session) run(ctx context.Context, actions ...cdpActionFunc) error {
	return s.runOn(s.activeContext(), ctx, actions...)
}
func (s *Session) runOn(tab, ctx context.Context, actions ...cdpActionFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op, cancel := context.WithCancel(tab)
	stop := context.AfterFunc(ctx, cancel)
	defer cancel()
	defer stop()
	for _, action := range actions {
		if err := op.Err(); err != nil {
			return err
		}
		if err := action(op); err != nil {
			if cause := ctx.Err(); cause != nil {
				return cause
			}
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
func (s *Session) restore(ctx context.Context) (page.ScriptIdentifier, error) {
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
			return "", err
		}
	}
	values := s.creds.Storage[s.site.BaseURL]
	if len(values) == 0 {
		return "", nil
	}
	script := "(()=>{if(location.origin!==" + jsonJS(s.site.BaseURL) + ")return;for(const [key,value] of Object.entries(" + jsonJS(values) + ")){localStorage.setItem(key,value)}})()"
	var id page.ScriptIdentifier
	err := s.run(ctx, cdpActionFunc(func(ctx context.Context) error {
		result, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: script})
		id = result.Identifier
		return err
	}))
	return id, err
}

func (s *Session) listenLoginResponses(tab context.Context) {
	events := chromedp.Events(tab, network.ResponseReceived)
	go func() {
		for ev, err := range events {
			if err != nil {
				return
			}
			u, err := url.Parse(ev.Response.URL)
			if err != nil {
				continue
			}
			base, _ := url.Parse(s.site.BaseURL)
			if u.Scheme != base.Scheme || u.Host != base.Host {
				continue
			}
			if u.Path != s.site.LoginAPIPath && u.Path != "/api/oauth/github" && u.Path != "/api/oauth/linuxdo" {
				continue
			}
			go func(id network.RequestID, status int) {
				ctx, cancel := context.WithTimeout(tab, 5*time.Second)
				defer cancel()
				// ResponseReceived precedes body completion. Retry this read-only
				// CDP operation until the body is available; never resend login.
				for ctx.Err() == nil {
					var body []byte
					err := s.runOn(tab, ctx, cdpActionFunc(func(ctx context.Context) error {
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
	if method == http.MethodGet {
		headers["Cache-Control"] = "no-store"
	}
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
	expression := "(async()=>{const a=" + jsonJS(args) + ";if(location.origin!==a.origin){return {status:0,body:''}};const r=await fetch(a.path,{method:a.method,headers:a.headers,credentials:'include',cache:'no-store',redirect:'error',body:a.body===null?undefined:JSON.stringify(a.body)});const body=await r.text();if(body.length>1048576)throw Error('response too large');return {status:r.status,body:body,retry:Number(r.headers.get('Retry-After'))||0}})()"
	var value struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Retry  int    `json:"retry"`
	}
	err := s.runSite(ctx, cdpEvaluate(expression, &value, chromedp.EvalAwaitPromise))
	if err != nil {
		if method != "GET" {
			return out, port.ErrUncertain
		}
		return out, err
	}
	if value.Status == 0 {
		return out, s.loginDiagnostic(ctx, "请求 "+method+" "+path+" 时未找到本站页面，请完成授权并返回站点")
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
	if s.site.Adapter == "x666" {
		var token string
		err := s.runSite(ctx, cdpEvaluate("location.origin==="+jsonJS(s.site.BaseURL)+" ? localStorage.getItem('userToken')||'' : ''", &token))
		if err == nil {
			s.browserToken = s.browserToken || token != ""
			if s.browserToken {
				// Once authentication comes from the page, logout must clear it.
				// An explicitly supplied token still works without browser storage.
				s.creds.Token = token
			}
		}
		return err
	}
	var id string
	expr := "(()=>{if(location.origin!==" + jsonJS(s.site.BaseURL) + ")return '';try{const u=JSON.parse(localStorage.getItem('user')||'{}');return u.id==null?'':String(u.id)}catch{return ''}})()"
	err := s.runSite(ctx, cdpEvaluate(expr, &id))
	if err == nil && id != "" {
		s.creds.APIUser = id
	}
	return err
}

func (s *Session) Snapshot(ctx context.Context) (domain.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cookies []*network.Cookie
	if err := s.runOn(s.ctx, ctx, cdpActionFunc(func(ctx context.Context) error {
		owner := chromedp.FromContext(s.ctx)
		r, e := cdp.Call(ctx, owner.Browser, storage.GetCookies, storage.GetCookiesParams{BrowserContextID: owner.BrowserContextID})
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
	if err := s.runSite(ctx, cdpEvaluate(expr, &state)); err != nil {
		return c, err
	}
	c.Storage = map[string]map[string]string{s.site.BaseURL: state}
	if s.nativeWindow {
		_ = s.runSite(ctx, cdpEvaluate("navigator.userAgent", &c.UserAgent))
	}
	_ = s.refreshUserID(ctx)
	c.APIUser = s.creds.APIUser
	c.Token = s.creds.Token
	return c, nil
}
func (s *Session) Close() {
	s.closeOnce.Do(func() { s.cancel(); s.cleanup() })
}

var _ port.BrowserFactory = (*Factory)(nil)
var _ port.InteractiveSession = (*Session)(nil)
