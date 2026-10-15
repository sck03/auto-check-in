package provider

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
	"context"
	"errors"
	"net/url"
	"time"
)

type Runner struct {
	Factory  port.SessionFactory
	Registry Registry
}

func (r *Runner) Execute(ctx context.Context, site domain.Site, c domain.Credentials) (result domain.Result, updated *domain.Credentials) {
	defer func() {
		result.Unit = site.Unit
		result.QuotaPerUnit = site.QuotaPerUnit
		result.Message = security.Redact(result.Message, c)
		if updated != nil {
			result.Message = security.Redact(result.Message, *updated)
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
		return domain.Result{Status: domain.NeedsAttention, Message: "Agent Router 需要重新登录触发签到；请选择密码或 OAuth 登录方式", Evidence: "login_required"}, nil
	}
	session, err := r.Factory.Open(ctx, site, c)
	if err != nil {
		return failure(err), nil
	}
	defer session.Close()
	defer func() {
		if snapshot, e := session.Snapshot(ctx); e == nil {
			updated = &snapshot
		}
	}()
	var before *int64
	if site.Adapter != "browser" {
		if resp, e := session.Request(ctx, "GET", site.UserInfoPath, nil); e == nil {
			before = Balance(resp)
		}
	}
	var login port.Response
	if c.Method != "cookie" {
		login, err = session.Login(ctx)
		if err != nil {
			return failure(err), nil
		}
		if f := HTTPFailure(login); f != nil {
			return *f, nil
		}
		payload, e := Decode(login.Body)
		if e != nil {
			return domain.Result{Status: domain.NeedsAttention, Message: "登录响应无法验证，请使用人工登录", Evidence: "login_unverified"}, nil
		}
		if success, ok := payload["success"].(bool); !ok || !success {
			return domain.Result{Status: domain.NeedsAttention, Message: "登录失败，请核对凭据或处理站点验证", Evidence: "login_rejected"}, nil
		}
	}
	result, err = adapter.CheckIn(ctx, session, site, login)
	if err != nil {
		return failure(err), nil
	}
	result.BalanceBefore = before
	if site.Adapter != "browser" {
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
	stats, ok := dataObject(payload)["stats"].(map[string]any)
	return ok && stats["checked_in_today"] == true
}

func failure(err error) domain.Result {
	switch {
	case errors.Is(err, port.ErrUncertain):
		return domain.Result{Status: domain.Unverified, Message: "操作可能已生效，但未收到确认；请检查站点后手动重试", Evidence: "uncertain_request"}
	case errors.Is(err, port.ErrManualRequired), errors.Is(err, port.ErrBrowserUnavailable):
		return domain.Result{Status: domain.NeedsAttention, Message: err.Error(), Evidence: "manual_required"}
	case errors.Is(err, context.Canceled):
		return domain.Result{Status: domain.Interrupted, Message: "任务因程序退出而中断，请核对站点结果", Evidence: "cancelled_context"}
	default:
		return domain.Result{Status: domain.Failed, Message: "连接或执行失败，请检查网络与站点状态", Evidence: "transport_error", Retryable: true}
	}
}
