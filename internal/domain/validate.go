package domain

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var headerName = regexp.MustCompile("^[A-Za-z0-9-]{1,64}$")

func (s *Site) Validate(allowHTTP bool) error {
	s.Name = strings.TrimSpace(s.Name)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if s.Name == "" || len(s.Name) > 120 {
		return fmt.Errorf("站点名称应为 1 至 120 个字节")
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return fmt.Errorf("站点地址只能包含协议、主机和可选端口")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return fmt.Errorf("站点必须使用 HTTPS；本地测试可显式启用 HTTP")
	}
	u.Host = strings.ToLower(u.Host)
	s.BaseURL = CanonicalOrigin(u.String())
	template, ok := TemplateByID(s.TemplateID)
	if !ok || !template.Supports(s.Adapter) {
		return fmt.Errorf("请选择有效的平台模板和签到方式")
	}
	if s.Transport != "http" && s.Transport != "browser" {
		return fmt.Errorf("执行方式必须是 http 或 browser")
	}
	if s.Adapter == "browser" && s.Transport != "browser" {
		return fmt.Errorf("网页按钮签到需要浏览器执行方式")
	}
	for _, p := range []string{s.LoginPath, s.LoginAPIPath, s.CheckInPath, s.UserInfoPath} {
		if p != "" {
			if err := ValidatePath(p); err != nil {
				return err
			}
		}
	}
	if s.LoginPath == "" || s.LoginAPIPath == "" || s.UserInfoPath == "" {
		return fmt.Errorf("登录页面、登录接口和用户信息路径不能为空")
	}
	if s.Adapter != "agentrouter" && s.Adapter != "anyrouter" && s.CheckInPath == "" {
		return fmt.Errorf("签到路径不能为空")
	}
	if !headerName.MatchString(s.UserHeader) {
		return fmt.Errorf("无效的用户 ID 请求头")
	}
	switch strings.ToLower(s.UserHeader) {
	case "cookie", "authorization", "host", "origin", "referer", "content-length", "connection", "content-type":
		return fmt.Errorf("用户 ID 请求头不能覆盖认证或协议头")
	}
	if s.QuotaPerUnit < 1 || s.QuotaPerUnit > 1000000000000 {
		return fmt.Errorf("额度换算比例应大于 0")
	}
	if len(s.Unit) == 0 || len(s.Unit) > 16 {
		return fmt.Errorf("额度单位应为 1 至 16 个字节")
	}
	for _, selector := range []string{s.Selectors.LoginEntry, s.Selectors.Username, s.Selectors.Password, s.Selectors.Submit, s.Selectors.GitHub, s.Selectors.LinuxDO, s.Selectors.CheckIn, s.Selectors.Success, s.Selectors.Already} {
		if len(selector) > 1000 {
			return fmt.Errorf("页面选择器过长")
		}
	}
	return nil
}

// CanonicalOrigin uses the same default-port representation as location.origin.
// Callers validate the URL first; this function never changes a non-default port.
func CanonicalOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return origin
	}
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		u.Host = u.Hostname()
		if net.ParseIP(u.Host) != nil && strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String()
}

func ValidatePath(p string) error {
	u, err := url.Parse(p)
	if err != nil || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n") || u.IsAbs() || u.Host != "" || u.Fragment != "" || len(p) > 1024 {
		return fmt.Errorf("接口路径必须是同站点的 / 开头相对路径")
	}
	if strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		return fmt.Errorf("接口路径不能指向其他站点")
	}
	return nil
}

func (c Credentials) Validate(site Site) error {
	if site.Adapter == "x666" && c.Method != "cookie" {
		return fmt.Errorf("薄荷签到请选择浏览器会话 / Cookie，在 up.x666.me 用 LinuxDO 登录并保存会话，或填入该页面的 userToken")
	}
	switch c.Method {
	case "cookie", "password", "github", "linuxdo":
	default:
		return fmt.Errorf("不支持的登录方式")
	}
	if len(c.Username) > 320 || len(c.Password) > 4096 || len(c.CookieHeader) > 32768 || len(c.Token) > 16384 || len(c.APIUser) > 64 || len(c.UserAgent) > 1024 {
		return fmt.Errorf("凭据字段过长")
	}
	if strings.ContainsAny(c.APIUser+c.Token+c.CookieHeader+c.UserAgent, "\r\n") {
		return fmt.Errorf("凭据不能包含换行")
	}
	if c.CookieHeader != "" {
		req := http.Request{Header: http.Header{"Cookie": []string{c.CookieHeader}}}
		if len(req.Cookies()) == 0 {
			return fmt.Errorf("Cookie 应为 name=value; name2=value2 格式")
		}
	}
	if c.Method == "password" && (c.Username == "" || c.Password == "") {
		return fmt.Errorf("密码登录需要用户名和密码")
	}
	if (c.Method == "github" || c.Method == "linuxdo") && site.Transport != "browser" {
		return fmt.Errorf("OAuth 登录需要浏览器执行方式")
	}
	if len(c.Cookies) > 300 || len(c.Storage) > 4 {
		return fmt.Errorf("会话数据超过限制")
	}
	return nil
}

func (s Settings) Validate() error {
	if s.Concurrency < 1 || s.Concurrency > 4 {
		return fmt.Errorf("并发数应为 1 至 4")
	}
	if s.MaxRetries < 0 || s.MaxRetries > 3 {
		return fmt.Errorf("自动重试次数应为 0 至 3")
	}
	if s.RetryBaseSeconds < 60 || s.RetryBaseSeconds > 1800 {
		return fmt.Errorf("首次重试间隔应为 60 至 1800 秒")
	}
	if s.TaskTimeoutSeconds < 20 || s.TaskTimeoutSeconds > 600 {
		return fmt.Errorf("任务超时应为 20 至 600 秒")
	}
	if s.SiteGapSeconds < 0 || s.SiteGapSeconds > 300 {
		return fmt.Errorf("同站点间隔应为 0 至 300 秒")
	}
	if s.RetentionDays < 7 || s.RetentionDays > 3650 {
		return fmt.Errorf("历史保留应为 7 至 3650 天")
	}
	return nil
}
