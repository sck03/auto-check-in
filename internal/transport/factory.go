package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type Factory struct {
	Browser  port.SessionFactory
	ProxyURL string
	shared   *http.Transport
}

// Share only the connection pool. Headers, credentials and cookie jars always
// belong to individual HTTPSessions. The bootstrap owns the factory lifetime.
func NewFactory(browser port.SessionFactory, proxy string) (*Factory, error) {
	tr, err := newHTTPTransport(proxy)
	if err != nil {
		return nil, err
	}
	return &Factory{Browser: browser, ProxyURL: proxy, shared: tr}, nil
}

func newHTTPTransport(proxy string) (*http.Transport, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 30 * time.Second
	tr.MaxIdleConns, tr.MaxIdleConnsPerHost = 16, 2
	tr.IdleConnTimeout = 60 * time.Second
	if proxy != "" {
		p, err := url.Parse(proxy)
		if err != nil || p.Host == "" || p.User != nil || (p.Scheme != "http" && p.Scheme != "https" && p.Scheme != "socks5") {
			return nil, fmt.Errorf("代理地址应为不含用户名密码的 http、https 或 socks5 地址")
		}
		tr.Proxy = http.ProxyURL(p)
	}
	return tr, nil
}

func (f Factory) Close() {
	if f.shared != nil {
		f.shared.CloseIdleConnections()
	}
}

func (f Factory) Capacity() port.Capacity {
	if source, ok := f.Browser.(port.CapacitySource); ok {
		return source.Capacity()
	}
	return port.Capacity{}
}

func (f Factory) Open(ctx context.Context, site domain.Site, c domain.Credentials) (port.Session, error) {
	if site.Transport == "browser" {
		if f.Browser == nil {
			return nil, port.ErrBrowserUnavailable
		}
		return f.Browser.Open(ctx, site, c)
	}
	if f.shared != nil {
		return openHTTP(site, c, f.shared, false)
	}
	return OpenHTTP(site, c, f.ProxyURL)
}
