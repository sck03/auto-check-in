package provider

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"context"
	"errors"
	"fmt"
	"time"
)

type Adapter struct {
	RequiresFreshLogin bool
	CheckIn            func(context.Context, port.Session, domain.Site, port.Response) (domain.Result, error)
}
type Registry map[string]Adapter

// Registries are assembled before workers start and are immutable thereafter.
func DefaultRegistry() Registry {
	api := Adapter{CheckIn: apiCheckIn}
	return Registry{
		"newapi":    api,
		"anyrouter": {CheckIn: visitCheckIn},
		"x666":      {CheckIn: mintCheckIn},
		"agentrouter": {RequiresFreshLogin: true, CheckIn: func(_ context.Context, _ port.Session, _ domain.Site, login port.Response) (domain.Result, error) {
			return Normalize("agentrouter", login), nil
		}},
		"browser": {CheckIn: func(ctx context.Context, session port.Session, _ domain.Site, _ port.Response) (domain.Result, error) {
			return session.PageCheckIn(ctx)
		}},
	}
}

func visitCheckIn(ctx context.Context, session port.Session, site domain.Site, _ port.Response) (domain.Result, error) {
	if browser, ok := session.(interface {
		Visit(context.Context, string) error
	}); ok {
		if err := browser.Visit(ctx, "/console"); err != nil {
			return domain.Result{}, fmt.Errorf("打开已登录页面：%w", err)
		}
	} else {
		resp, err := session.Request(ctx, "GET", "/", nil)
		if err != nil {
			return domain.Result{}, err
		}
		if f := HTTPFailure(resp); f != nil {
			return *f, nil
		}
	}
	resp, err := session.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil {
		return domain.Result{}, err
	}
	if f := HTTPFailure(resp); f != nil {
		return *f, nil
	}
	if !Authenticated(resp, "") {
		return domain.Result{Status: domain.NeedsAttention, Message: "访问页面后未确认登录身份：" + responseMessage(resp, "请重新登录"), Evidence: "session_invalid", Advice: "完成浏览器登录并保存会话后重试。"}, nil
	}
	return domain.Result{Status: domain.Succeeded, Message: "已访问登录页面并确认用户身份，按 AnyRouter 08:00 后访问规则完成签到", Evidence: "authenticated_visit", BalanceAfter: Balance(resp)}, nil
}

func apiCheckIn(ctx context.Context, session port.Session, site domain.Site, _ port.Response) (domain.Result, error) {
	if site.Adapter == "newapi" && verifyStatus(ctx, session, site) {
		return domain.Result{Status: domain.AlreadyDone, Message: "签到状态接口确认今日已签到", Evidence: "status_api"}, nil
	}
	response, err := session.Request(ctx, "POST", site.CheckInPath, map[string]any{})
	if err != nil {
		if errors.Is(err, port.ErrUncertain) && site.Adapter == "newapi" && ctx.Err() == nil {
			verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			confirmed := verifyStatus(verifyCtx, session, site)
			cancel()
			if confirmed {
				return domain.Result{Status: domain.AlreadyDone, Message: "响应丢失后，状态接口确认今日已签到", Evidence: "status_api"}, nil
			}
		}
		return domain.Result{}, err
	}
	result := Normalize(site.Adapter, response)
	if !result.Complete() && result.Status == domain.Unverified && verifyStatus(ctx, session, site) {
		return domain.Result{Status: domain.AlreadyDone, Message: "响应不明确，状态接口确认今日已签到", Evidence: "status_api"}, nil
	}
	return result, nil
}
