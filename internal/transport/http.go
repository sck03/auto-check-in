package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"golang.org/x/net/publicsuffix"
)

type Factory struct {
	Browser  port.SessionFactory
	ProxyURL string
}

func (f Factory) Open(ctx context.Context, s domain.Site, c domain.Credentials) (port.Session, error) {
	if s.Transport == "browser" {
		if f.Browser == nil {
			return nil, port.ErrBrowserUnavailable
		}
		return f.Browser.Open(ctx, s, c)
	}
	return OpenHTTP(s, c, f.ProxyURL)
}

type HTTPSession struct {
	site      domain.Site
	creds     domain.Credentials
	base      *url.URL
	client    *http.Client
	transport *http.Transport
	saved     map[string]domain.Cookie
}

func OpenHTTP(site domain.Site, c domain.Credentials, proxy string) (*HTTPSession, error) {
	base, err := url.Parse(site.BaseURL)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 30 * time.Second
	if proxy != "" {
		p, e := url.Parse(proxy)
		if e != nil {
			return nil, e
		}
		tr.Proxy = http.ProxyURL(p)
	}
	s := &HTTPSession{site: site, creds: c, base: base, transport: tr, saved: map[string]domain.Cookie{}}
	s.client = &http.Client{Transport: tr, Jar: jar, Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, cookie := range c.Cookies {
		if !cookieForHost(cookie.Domain, base.Hostname()) {
			continue
		}
		h := &http.Cookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly}
		if cookie.Expires > 0 {
			h.Expires = time.Unix(int64(cookie.Expires), 0)
		}
		jar.SetCookies(base, []*http.Cookie{h})
		s.saved[cookieKey(cookie)] = cookie
	}
	req := http.Request{Header: http.Header{"Cookie": []string{c.CookieHeader}}}
	for _, cookie := range req.Cookies() {
		cookie.Domain = base.Hostname()
		cookie.Path = "/"
		cookie.Secure = base.Scheme == "https"
		jar.SetCookies(base, []*http.Cookie{cookie})
		s.remember(cookie, "/")
	}
	return s, nil
}

func cookieForHost(domainName, host string) bool {
	d := strings.TrimPrefix(domainName, ".")
	return d == host || strings.HasSuffix(host, "."+d)
}
func cookieKey(c domain.Cookie) string { return c.Domain + "\n" + c.Path + "\n" + c.Name }
func (s *HTTPSession) remember(c *http.Cookie, requestPath string) {
	d := c.Domain
	if d == "" {
		d = s.base.Hostname()
	}
	p := c.Path
	if p == "" {
		p = "/"
		if i := strings.LastIndex(requestPath, "/"); i > 0 {
			p = requestPath[:i]
		}
	}
	sameSite := ""
	switch c.SameSite {
	case http.SameSiteLaxMode:
		sameSite = "Lax"
	case http.SameSiteStrictMode:
		sameSite = "Strict"
	case http.SameSiteNoneMode:
		sameSite = "None"
	}
	expires := float64(0)
	if !c.Expires.IsZero() {
		expires = float64(c.Expires.Unix())
	}
	if c.MaxAge > 0 {
		expires = float64(time.Now().Add(time.Duration(c.MaxAge) * time.Second).Unix())
	}
	value := domain.Cookie{Name: c.Name, Value: c.Value, Domain: d, Path: p, Secure: c.Secure, HTTPOnly: c.HttpOnly, SameSite: sameSite, Expires: expires}
	if c.MaxAge < 0 || (expires > 0 && expires < float64(time.Now().Unix())) {
		delete(s.saved, cookieKey(value))
		return
	}
	s.saved[cookieKey(value)] = value
}

func (s *HTTPSession) Request(ctx context.Context, method, path string, body any) (port.Response, error) {
	var out port.Response
	if err := domain.ValidatePath(path); err != nil {
		return out, err
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return out, err
		}
	}
	target := s.site.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", s.site.BaseURL)
	req.Header.Set("Referer", s.site.BaseURL+"/")
	req.Header.Set("User-Agent", "AutoCheckIn/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if s.creds.APIUser != "" {
		req.Header.Set(s.site.UserHeader, s.creds.APIUser)
	}
	if s.creds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.creds.Token)
	}
	var sent atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			sent.Store(true)
		}
	}}))
	resp, err := s.client.Do(req)
	if err != nil {
		if method != "GET" && sent.Load() {
			return out, port.ErrUncertain
		}
		return out, fmt.Errorf("连接站点失败：%w", err)
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	for _, cookie := range resp.Cookies() {
		s.remember(cookie, req.URL.Path)
	}
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, e := strconv.Atoi(value); e == nil {
			out.RetryAfterSeconds = seconds
		} else if t, e := http.ParseTime(value); e == nil {
			out.RetryAfterSeconds = int(time.Until(t).Seconds())
		}
	}
	out.Body, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(out.Body) > 1<<20 {
		if method != "GET" {
			return out, port.ErrUncertain
		}
		return out, errors.New("响应读取失败或超过 1 MiB 限制")
	}
	return out, nil
}

func (s *HTTPSession) Login(ctx context.Context) (port.Response, error) {
	if s.creds.Method != "password" {
		return port.Response{}, port.ErrManualRequired
	}
	resp, err := s.Request(ctx, "POST", s.site.LoginAPIPath, map[string]string{"username": s.creds.Username, "password": s.creds.Password})
	if err == nil {
		if id := UserID(resp.Body); id != "" {
			s.creds.APIUser = id
		}
	}
	return resp, err
}
func (*HTTPSession) PageCheckIn(context.Context) (domain.Result, error) {
	return domain.Result{}, port.ErrManualRequired
}
func (s *HTTPSession) Snapshot(context.Context) (domain.Credentials, error) {
	c := s.creds
	c.CookieHeader = ""
	c.Cookies = nil
	for _, v := range s.saved {
		if v.Expires <= 0 || v.Expires > float64(time.Now().Unix()) {
			c.Cookies = append(c.Cookies, v)
		}
	}
	return c, nil
}
func (s *HTTPSession) Close() { s.transport.CloseIdleConnections() }

// UserID only accepts an authenticated user response, never arbitrary page text.
func UserID(body []byte) string {
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			ID json.RawMessage `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &env) != nil || !env.Success || len(env.Data.ID) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(env.Data.ID, &value) == nil {
		return value
	}
	var n json.Number
	if json.Unmarshal(env.Data.ID, &n) == nil {
		if _, err := n.Int64(); err == nil {
			return string(n)
		}
	}
	return ""
}
