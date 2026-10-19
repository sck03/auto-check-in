package provider

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
)

func authenticated(resp port.Response, expectedID string) bool {
	id := transport.UserID(resp.Body)
	return resp.Status >= 200 && resp.Status < 300 && id != "" && (expectedID == "" || id == expectedID)
}

var expiredSessionPattern = regexp.MustCompile(`(?i)(未登[录陆]|登录.{0,8}(过期|失效)|会话.{0,8}(过期|失效)|session.{0,8}expired|unauthorized|not logged in)`)

func sessionExpired(resp port.Response) bool {
	if challengeResponse(resp.Body) {
		return false
	}
	if resp.Status == http.StatusUnauthorized {
		return true
	}
	if resp.Status != http.StatusOK && resp.Status != http.StatusForbidden {
		return false
	}
	payload, err := Decode(resp.Body)
	return err == nil && payload["success"] == false && expiredSessionPattern.MatchString(message(payload))
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

// FreshLoginCredentials starts fresh locally, retaining only WAF cookies and
// identity-provider sessions. A server logout could revoke an imported cookie
// the user also uses in their normal browser.
func FreshLoginCredentials(site domain.Site, c domain.Credentials) domain.Credentials {
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
