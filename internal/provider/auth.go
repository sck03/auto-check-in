package provider

import (
	"net/http"
	"net/url"
	"strings"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
)

func authenticated(resp port.Response, expectedID string) bool {
	id := transport.UserID(resp.Body)
	return resp.Status >= 200 && resp.Status < 300 && id != "" && (expectedID == "" || id == expectedID)
}

func loginFailure(resp port.Response) *domain.Result {
	if r := HTTPFailure(resp); r != nil {
		return r
	}
	payload, err := Decode(resp.Body)
	if err != nil {
		return &domain.Result{Status: domain.NeedsAttention, Message: "登录接口未返回有效 JSON，无法确认登录", Evidence: "login_unverified", HTTPStatus: resp.Status, Advice: "使用浏览器登录完成验证；若持续出现，请检查站点登录接口路径。"}
	}
	data := dataObject(payload)
	if data["require_2fa"] == true || data["two_factor_required"] == true || data["requires_two_factor"] == true {
		return &domain.Result{Status: domain.NeedsAttention, Message: "密码已提交，站点要求二次验证", Evidence: "two_factor_required", Advice: "点击浏览器登录，完成二次验证后保存会话。"}
	}
	if payload["success"] != true {
		msg := message(payload)
		if msg == "" {
			msg = "站点未确认登录成功"
		}
		return &domain.Result{Status: domain.NeedsAttention, Message: "登录失败：" + msg, Evidence: "login_rejected", HTTPStatus: resp.Status, Advice: "核对本站的用户名和密码；OAuth 注册账号可能需要先在站点设置密码。验证码请通过浏览器登录处理。"}
	}
	return nil
}

// A server logout can revoke a cookie the user also uses in their normal
// browser. Start fresh locally, retaining only WAF cookies and identity-provider
// sessions; never invalidate the imported session at the server.
func freshLoginCredentials(site domain.Site, c domain.Credentials) domain.Credentials {
	base, _ := url.Parse(site.BaseURL)
	keep := func(cookie domain.Cookie) bool {
		d := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		siteCookie := d == "" || base.Hostname() == d || strings.HasSuffix(base.Hostname(), "."+d)
		return !siteCookie || cookie.Name == "cf_clearance" || strings.HasPrefix(cookie.Name, "__cf") || strings.HasPrefix(cookie.Name, "acw_")
	}
	cookies := make([]domain.Cookie, 0, len(c.Cookies))
	for _, cookie := range c.Cookies {
		if keep(cookie) {
			cookies = append(cookies, cookie)
		}
	}
	req := http.Request{Header: http.Header{"Cookie": []string{c.CookieHeader}}}
	for _, cookie := range req.Cookies() {
		v := domain.Cookie{Name: cookie.Name, Value: cookie.Value, Domain: base.Hostname(), Path: "/", Secure: base.Scheme == "https"}
		if keep(v) {
			cookies = append(cookies, v)
		}
	}
	c.Cookies, c.CookieHeader, c.Token, c.APIUser = cookies, "", "", ""
	storage := make(map[string]map[string]string)
	for origin, values := range c.Storage {
		if origin != site.BaseURL {
			storage[origin] = values
		}
	}
	c.Storage = storage
	return c
}
