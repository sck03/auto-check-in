package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/security"
)

// Input completion means the click was delivered, not that the site's async
// login finished. Retry only read-only 401 checks, for at most two seconds.
// A wrong identity, WAF challenge or malformed response stops immediately.
func verifyLoginSession(ctx context.Context, entry *loginSession) (domain.Credentials, error) {
	until := time.Now().Add(2 * time.Second)
	for {
		c, err := loginSnapshot(ctx, entry)
		if err != nil {
			return c, err
		}
		if entry.site.Adapter == "browser" || entry.site.Adapter == "agentrouter" && entry.session.ConfirmedLogin(ctx) {
			return c, nil
		}
		response, err := entry.session.Request(ctx, http.MethodGet, entry.site.UserInfoPath, nil)
		if err != nil {
			return c, fmt.Errorf("请先在站点完成登录并返回站点页面：%s", security.Redact(err.Error(), entry.creds))
		}
		if response.Status == http.StatusUnauthorized && time.Now().Before(until) {
			if failure := provider.HTTPFailure(response); failure != nil && failure.Evidence == "challenge_required" {
				return c, fmt.Errorf("登录验证未通过：%s", failure.Message)
			}
			if !wait(ctx, 200*time.Millisecond) {
				return c, ctx.Err()
			}
			continue
		}
		if failure := provider.HTTPFailure(response); failure != nil {
			return c, fmt.Errorf("登录验证未通过：%s", security.Redact(failure.Message, entry.creds))
		}
		payload, decodeErr := provider.Decode(response.Body)
		if decodeErr != nil || payload["success"] != true {
			message, _ := payload["message"].(string)
			if message == "" {
				message = "用户信息接口未返回有效的登录成功响应"
			}
			return c, fmt.Errorf("登录尚未通过站点验证（HTTP %d）：%s", response.Status, security.Redact(message, entry.creds))
		}
		if entry.site.Adapter == "x666" {
			if _, ok := payload["can_spin"].(bool); !ok {
				return c, fmt.Errorf("薄荷签到会话未通过验证，请在 up.x666.me 完成 LinuxDO 登录")
			}
		} else if !provider.Authenticated(response, entry.expectedUser) {
			return c, fmt.Errorf("登录身份与此账号不符或缺少用户 ID，原会话未更新；请确认账号后重新登录")
		}
		c, err = loginSnapshot(ctx, entry)
		if err == nil && entry.site.Adapter != "x666" && !provider.Authenticated(response, c.APIUser) {
			return c, fmt.Errorf("验证响应与浏览器快照的用户身份不符，原会话未更新")
		}
		return c, err
	}
}

func loginSnapshot(ctx context.Context, entry *loginSession) (domain.Credentials, error) {
	c, err := entry.session.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrSessionLimit) {
			return c, err
		}
		return c, fmt.Errorf("无法读取浏览器会话，请重试")
	}
	if entry.expectedUser != "" && c.APIUser != "" && c.APIUser != entry.expectedUser {
		return c, fmt.Errorf("登录身份与此账号不符，原会话未更新；如需更换账号，请编辑并明确清除旧会话")
	}
	return c, nil
}
