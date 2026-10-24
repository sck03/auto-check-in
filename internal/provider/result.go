package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
)

// Match complete acknowledgements, never fragments inside errors or negations.
var alreadyPattern = regexp.MustCompile(`(?i)^(?:(?:今日|今天)?(?:已|已经)签到(?:了)?(?:[,，;；]\s*(?:请勿重复(?:签到|操作)|请明天再来))?|重复签到|(?:you\s+have\s+)?already\s+(?:checked|signed)[ -]?in(?:\s+today)?)[.!！。\s]*$`)

func Decode(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var data map[string]any
	if err := decoder.Decode(&data); err != nil || data == nil {
		return nil, fmt.Errorf("站点未返回有效的 JSON 对象")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("响应包含多余或损坏的数据")
	}
	return data, nil
}
func dataObject(payload map[string]any) map[string]any {
	m, _ := payload["data"].(map[string]any)
	return m
}
func message(payload map[string]any) string {
	for _, key := range []string{"message", "msg"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}
func quota(value any) *int64 {
	if n, ok := value.(json.Number); ok {
		if v, err := n.Int64(); err == nil && v >= 0 {
			return &v
		}
	}
	return nil
}
func Balance(resp port.Response) *int64 {
	payload, err := Decode(resp.Body)
	if err != nil || resp.Status < 200 || resp.Status >= 300 {
		return nil
	}
	if success, ok := payload["success"].(bool); !ok || !success {
		return nil
	}
	return quota(dataObject(payload)["quota"])
}

func HTTPFailure(resp port.Response) *domain.Result {
	if challengeResponse(resp.Body) {
		return &domain.Result{Status: domain.NeedsAttention, Message: fmt.Sprintf("站点返回真人验证页面（HTTP %d）", resp.Status), Evidence: "challenge_required", HTTPStatus: resp.Status, Advice: "使用浏览器登录完成人机验证并保存会话。若 HTTP 执行仍被拦截，将站点执行方式改为浏览器；验证是否通过由站点决定。"}
	}
	if resp.Status >= 200 && resp.Status < 300 {
		return nil
	}
	r := domain.Result{Status: domain.Failed, Message: fmt.Sprintf("站点返回 HTTP %d", resp.Status), Evidence: "http_status", HTTPStatus: resp.Status}
	switch {
	case resp.Status == 401 || resp.Status == 403:
		r.Status = domain.NeedsAttention
		r.Advice = "登录可能失效或用户 ID 不符，请通过浏览器登录更新会话。"
	case resp.Status >= 300 && resp.Status < 400:
		r.Status = domain.NeedsAttention
		r.Message = "站点要求跳转登录，请重新登录"
	case resp.Status == 429 || resp.Status >= 500:
		r.Retryable = true
		r.RetryAfterSeconds = resp.RetryAfterSeconds
		r.Advice = "站点限流或暂时不可用，请稍后重试。"
	case resp.Status == 404 || resp.Status == 405:
		r.Advice = "检查站点模板和接口路径；此站可能不支持该接口。"
	}
	if msg := responseMessage(resp, ""); msg != "" {
		r.Message += "：" + msg
	}
	return &r
}

func responseMessage(resp port.Response, fallback string) string {
	if payload, err := Decode(resp.Body); err == nil {
		if msg := strings.TrimSpace(message(payload)); msg != "" {
			return msg
		}
	}
	return fallback
}

func challengeResponse(body []byte) bool {
	text := strings.ToLower(string(body))
	if !strings.Contains(text, "<html") && !strings.Contains(text, "<!doctype") {
		return false
	}
	for _, marker := range []string{"cf-chl-", "/cdn-cgi/challenge-platform/", "just a moment", "verify you are human", "验证您是真人", "acw_sc__v2", "aliyun_waf_"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func Normalize(adapter string, resp port.Response) domain.Result {
	if resp.Status == 400 || resp.Status == 409 {
		if alreadyPattern.MatchString(strings.TrimSpace(responseMessage(resp, ""))) {
			return domain.Result{Status: domain.AlreadyDone, Message: "站点确认当前周期已签到", Evidence: "provider_already_done", HTTPStatus: resp.Status}
		}
	}
	if failure := HTTPFailure(resp); failure != nil {
		return *failure
	}
	payload, err := Decode(resp.Body)
	if err != nil {
		return domain.Result{Status: domain.Unverified, Message: "响应不是有效 JSON，无法确认签到结果；请核对站点或处理验证", Evidence: "invalid_response"}
	}
	msg := message(payload)
	if alreadyPattern.MatchString(strings.TrimSpace(msg)) {
		return domain.Result{Status: domain.AlreadyDone, Message: "站点确认当前周期已签到", Evidence: "provider_already_done"}
	}
	success, hasSuccess := payload["success"].(bool)
	if !hasSuccess {
		return domain.Result{Status: domain.Unverified, Message: "响应缺少明确的业务成功字段", Evidence: "missing_acknowledgement"}
	}
	if !success {
		if msg == "" {
			msg = "站点拒绝了签到请求"
		}
		status := domain.Failed
		lower := strings.ToLower(msg)
		for _, word := range []string{"登录", "captcha", "turnstile", "验证码", "验证", "unauthorized", "login", "token expired", "用户id", "user id", "pow", "proof of work"} {
			if strings.Contains(lower, word) {
				status = domain.NeedsAttention
				break
			}
		}
		advice := "检查站点是否开启签到，并核对接口配置。"
		if status == domain.NeedsAttention {
			advice = "通过浏览器登录完成验证并保存会话；若签到按钮每次都要求验证码，请在站点手动签到。"
		}
		return domain.Result{Status: status, Message: msg, Evidence: "provider_rejected", Advice: advice, HTTPStatus: resp.Status}
	}
	data := dataObject(payload)
	if adapter == "agentrouter" {
		checked, ok := data["checked_in"].(bool)
		if !ok {
			if transport.UserID(resp.Body) != "" {
				return domain.Result{Status: domain.Succeeded, Message: "已完成一次新的登录，按 AgentRouter 08:00 后重新登录规则完成签到", Evidence: "fresh_login", Reward: quota(data["quota_awarded"])}
			}
			return domain.Result{Status: domain.Unverified, Message: "登录响应没有用户身份或签到证据，无法确认签到", Evidence: "login_without_checkin_proof"}
		}
		if !checked {
			return domain.Result{Status: domain.AlreadyDone, Message: "登录响应确认本次未重复发放签到奖励", Evidence: "checked_in_false"}
		}
	}
	if msg == "" {
		msg = "站点确认签到成功"
	}
	return domain.Result{Status: domain.Succeeded, Message: msg, Evidence: "provider_acknowledgement", Reward: quota(data["quota_awarded"])}
}
