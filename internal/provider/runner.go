package provider

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

type Runner struct {
	Factory  port.SessionFactory
	Registry Registry
	Now      func() time.Time
}

func (r *Runner) Execute(ctx context.Context, site domain.Site, c domain.Credentials) (result domain.Result, updated *domain.Credentials) {
	stage := "prepare"
	var before *int64
	defer func() {
		if result.Stage == "" {
			result.Stage = stage
		}
		result.BalanceBefore = before
		result.Unit = site.Unit
		result.QuotaPerUnit = site.QuotaPerUnit
		result.Message = security.Redact(result.Message, c)
		result.Advice = security.Redact(result.Advice, c)
		if updated != nil {
			result.Message = security.Redact(result.Message, *updated)
			result.Advice = security.Redact(result.Advice, *updated)
		}
	}()
	registry := r.Registry
	if registry == nil {
		registry = DefaultRegistry()
	}
	adapter, ok := registry[site.Adapter]
	if !ok {
		return domain.Result{Status: domain.NeedsAttention, Message: "未注册的站点适配器"}, nil
	}
	if adapter.RequiresFreshLogin && c.Method == "cookie" {
		return domain.Result{Status: domain.NeedsAttention, Message: "AgentRouter 需要重新登录触发签到；请选择密码或 OAuth 登录方式", Evidence: "login_required"}, nil
	}
	if site.Adapter == "anyrouter" || site.Adapter == "agentrouter" {
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		day, _ := domain.DayBounds(now)
		if now.Before(day.Add(8 * time.Hour)) {
			return domain.Result{Status: domain.Failed, Message: "尚未到北京时间 08:00，未执行本日签到", Evidence: "before_reset", Advice: "请在 08:00 后执行；本次不会记为今日签到成功。"}, nil
		}
	}
	working := c
	if adapter.RequiresFreshLogin {
		working = freshLoginCredentials(site, c)
	}
	stage = "open_session"
	session, err := r.Factory.Open(ctx, site, working)
	if err != nil {
		return failure(err), nil
	}
	defer session.Close()
	defer func() {
		if snapshot, e := session.Snapshot(ctx); e == nil {
			updated = &snapshot
		}
	}()
	stage = "session_check"
	valid := false
	if site.Adapter != "browser" && site.Adapter != "x666" && !adapter.RequiresFreshLogin {
		resp, e := session.Request(ctx, "GET", site.UserInfoPath, nil)
		if e != nil {
			return failure(e), nil
		}
		before = Balance(resp)
		valid = authenticated(resp, c.APIUser)
		// A timeout, outage, challenge or wrong identity is not proof that the
		// saved session expired. Reauthentication can revoke other sessions.
		if !valid && (c.Method == "cookie" || !sessionExpired(resp)) {
			if f := HTTPFailure(resp); f != nil {
				return *f, nil
			}
			return domain.Result{Status: domain.NeedsAttention, Message: "保存的会话未通过用户身份验证：" + responseMessage(resp, "会话失效、用户 ID 不符或响应缺少用户信息"), Evidence: "session_invalid", HTTPStatus: resp.Status, Advice: "点击浏览器登录，确认是此账号后保存会话；Cookie 登录还需正确的 API 用户 ID。"}, nil
		}
	}
	var login port.Response
	if c.Method != "cookie" && !valid {
		stage = "login"
		login, err = session.Login(ctx)
		if err != nil {
			return failure(err), nil
		}
		if f := loginFailure(login); f != nil {
			return *f, nil
		}
	}
	stage = "checkin"
	result, err = adapter.CheckIn(ctx, session, site, login)
	if err != nil {
		return failure(err), nil
	}
	if site.Adapter != "browser" && site.Adapter != "x666" && result.BalanceAfter == nil && result.Complete() {
		if resp, e := session.Request(ctx, "GET", site.UserInfoPath, nil); e == nil {
			result.BalanceAfter = Balance(resp)
		}
	}
	return result, nil
}

// Only New API has a known read-only check-in status endpoint. Other adapters
// must supply their own verifier; a balance read is never a substitute.
func verifyStatus(ctx context.Context, session port.Session, site domain.Site) bool {
	path, err := url.Parse(site.CheckInPath)
	if err != nil {
		return false
	}
	start, _ := domain.DayBounds(time.Now())
	query := path.Query()
	query.Set("month", start.Format("2006-01"))
	path.RawQuery = query.Encode()
	response, err := session.Request(ctx, "GET", path.String(), nil)
	if err != nil || response.Status < 200 || response.Status >= 300 {
		return false
	}
	payload, err := Decode(response.Body)
	if err != nil || payload["success"] != true {
		return false
	}
	data := dataObject(payload)
	// WONG reports today's state directly instead of New API's monthly stats.
	if checked, ok := data["checked_in"].(bool); ok {
		return checked
	}
	stats, ok := data["stats"].(map[string]any)
	return ok && stats["checked_in_today"] == true
}

func failure(err error) domain.Result {
	switch {
	case errors.Is(err, port.ErrUncertain):
		return domain.Result{Status: domain.Unverified, Message: err.Error(), Evidence: "uncertain_request", Advice: "操作可能已生效，请先在站点核对，避免重复提交。"}
	case errors.Is(err, port.ErrManualRequired), errors.Is(err, port.ErrBrowserUnavailable):
		return domain.Result{Status: domain.NeedsAttention, Message: err.Error(), Evidence: "manual_required", Advice: "点击浏览器登录，完成登录或验证后保存会话，再手动重试。"}
	case errors.Is(err, context.Canceled):
		return domain.Result{Status: domain.Interrupted, Message: "任务因程序退出而中断，请核对站点结果", Evidence: "cancelled_context"}
	case errors.Is(err, context.DeadlineExceeded):
		return domain.Result{Status: domain.Failed, Message: "执行超时：" + err.Error(), Evidence: "timeout", Retryable: true, Advice: "检查站点与网络；持续超时可在设置中增加任务超时。"}
	default:
		return domain.Result{Status: domain.Failed, Message: fmt.Sprintf("连接或执行失败：%v", err), Evidence: "transport_error", Retryable: true, Advice: "检查网络、代理和站点配置，具体错误见上方。"}
	}
}
