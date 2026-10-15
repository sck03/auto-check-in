package provider

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"context"
	"errors"
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
		"anyrouter": api,
		"agentrouter": {RequiresFreshLogin: true, CheckIn: func(_ context.Context, _ port.Session, _ domain.Site, login port.Response) (domain.Result, error) {
			return Normalize("agentrouter", login), nil
		}},
		"browser": {CheckIn: func(ctx context.Context, session port.Session, _ domain.Site, _ port.Response) (domain.Result, error) {
			return session.PageCheckIn(ctx)
		}},
	}
}
func apiCheckIn(ctx context.Context, session port.Session, site domain.Site, _ port.Response) (domain.Result, error) {
	if site.Adapter == "newapi" && verifyStatus(ctx, session, site) {
		return domain.Result{Status: domain.AlreadyDone, Message: "签到状态接口确认今日已签到", Evidence: "status_api"}, nil
	}
	response, err := session.Request(ctx, "POST", site.CheckInPath, map[string]any{})
	if err != nil {
		if errors.Is(err, port.ErrUncertain) && site.Adapter == "newapi" && ctx.Err() != context.Canceled {
			verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			confirmed := verifyStatus(verifyCtx, session, site)
			cancel()
			if confirmed {
				return domain.Result{Status: domain.AlreadyDone, Message: "响应丢失后，状态接口确认今日已签到", Evidence: "status_api"}, nil
			}
		}
		return domain.Result{}, err
	}
	return Normalize(site.Adapter, response), nil
}
