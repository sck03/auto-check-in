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
)

var alreadyPattern = regexp.MustCompile(`(?i)(今日已签到|今天已签到|今天已经签到|今日已经签到|已经签到|重复签到|already\s+(checked|signed)[ -]?in|already\s+checked\s+in\s+today)`)

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
	if resp.Status >= 200 && resp.Status < 300 {
		return nil
	}
	r := domain.Result{Status: domain.Failed, Message: fmt.Sprintf("站点返回 HTTP %d", resp.Status), Evidence: "http_status"}
	switch {
	case resp.Status == 401 || resp.Status == 403:
		r.Status = domain.NeedsAttention
		r.Message = "登录已失效或站点要求验证，请更新凭据或使用人工登录"
	case resp.Status >= 300 && resp.Status < 400:
		r.Status = domain.NeedsAttention
		r.Message = "站点要求跳转登录，请重新登录"
	case resp.Status == 429 || resp.Status >= 500:
		r.Retryable = true
		r.RetryAfterSeconds = resp.RetryAfterSeconds
	}
	return &r
}

func Normalize(adapter string, resp port.Response) domain.Result {
	if failure := HTTPFailure(resp); failure != nil {
		return *failure
	}
	payload, err := Decode(resp.Body)
	if err != nil {
		return domain.Result{Status: domain.Unverified, Message: "响应不是有效 JSON，无法确认签到结果；请核对站点或处理验证", Evidence: "invalid_response"}
	}
	msg := message(payload)
	if alreadyPattern.MatchString(msg) || strings.TrimSpace(msg) == "已签到" {
		return domain.Result{Status: domain.AlreadyDone, Message: "站点确认当前周期已签到", Evidence: "provider_already_done"}
	}
	success, hasSuccess := payload["success"].(bool)
	if adapter == "anyrouter" && !hasSuccess {
		if ret, ok := payload["ret"].(json.Number); ok && ret == "1" {
			success = true
			hasSuccess = true
		}
		if code, ok := payload["code"].(json.Number); ok && code == "0" {
			success = true
			hasSuccess = true
		}
	}
	if !hasSuccess {
		return domain.Result{Status: domain.Unverified, Message: "响应缺少明确的业务成功字段", Evidence: "missing_acknowledgement"}
	}
	if !success {
		if msg == "" {
			msg = "站点拒绝了签到请求"
		}
		status := domain.Failed
		lower := strings.ToLower(msg)
		for _, word := range []string{"登录", "captcha", "turnstile", "验证码", "unauthorized", "login", "token expired"} {
			if strings.Contains(lower, word) {
				status = domain.NeedsAttention
				break
			}
		}
		return domain.Result{Status: status, Message: msg, Evidence: "provider_rejected"}
	}
	data := dataObject(payload)
	if adapter == "agentrouter" {
		checked, ok := data["checked_in"].(bool)
		if !ok {
			return domain.Result{Status: domain.Unverified, Message: "登录成功，但响应没有 checked_in 签到证据；请在站点确认", Evidence: "login_without_checkin_proof"}
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
