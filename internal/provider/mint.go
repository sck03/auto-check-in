package provider

import (
	"context"
	"errors"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

// Thin, same-origin adapter for up.x666.me's daily free spin. Credentials for
// x666.me are never forwarded to the reward host; it has its own saved session.
func mintStatus(ctx context.Context, session port.Session, site domain.Site) (bool, domain.Result, error) {
	resp, err := session.Request(ctx, "GET", site.UserInfoPath, nil)
	if err != nil {
		return false, domain.Result{}, err
	}
	if r := HTTPFailure(resp); r != nil {
		r.Stage = "session_check"
		return false, *r, nil
	}
	payload, err := Decode(resp.Body)
	if err != nil || payload["success"] != true {
		return false, domain.Result{Status: domain.NeedsAttention, Stage: "session_check", Message: "薄荷签到状态读取失败：" + responseMessage(resp, "无有效状态响应"), Evidence: "session_invalid", Advice: "在 up.x666.me 用 LinuxDO 登录后保存会话；主站 Cookie 不能替代签到页面的 userToken。"}, nil
	}
	canSpin, ok := payload["can_spin"].(bool)
	if ok && canSpin {
		return true, domain.Result{}, nil
	}
	if record, ok := payload["today_record"].(map[string]any); ok && len(record) > 0 && payload["can_spin"] == false {
		return false, domain.Result{Status: domain.AlreadyDone, Message: "薄荷签到状态确认今日已领取奖励", Evidence: "daily_spin_status", Reward: quota(record["quota_amount"])}, nil
	}
	return false, domain.Result{Status: domain.Unverified, Message: "薄荷未提供可领取标志或今日领取记录，无法确认签到", Evidence: "missing_acknowledgement", Advice: "打开 up.x666.me 核对今日领取状态。"}, nil
}

func mintCheckIn(ctx context.Context, session port.Session, site domain.Site, _ port.Response) (domain.Result, error) {
	canSpin, result, err := mintStatus(ctx, session, site)
	if err != nil || !canSpin {
		return result, err
	}
	resp, err := session.Request(ctx, "POST", site.CheckInPath, nil)
	if err == nil {
		result = Normalize("x666", resp)
		if result.Complete() {
			payload, _ := Decode(resp.Body)
			result.Reward = quota(payload["quota"])
			result.Evidence = "daily_spin_acknowledgement"
			return result, nil
		}
	}
	// A failed or lost POST must never trigger another spin in this attempt.
	// Read today's record once to settle ambiguous responses, including HTTP 400
	// from an already-completed daily spin.
	if (err == nil || errors.Is(err, port.ErrUncertain)) && ctx.Err() == nil {
		verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, confirmed, verifyErr := mintStatus(verifyCtx, session, site)
		if verifyErr == nil && confirmed.Complete() {
			return confirmed, nil
		}
	}
	return result, err
}
