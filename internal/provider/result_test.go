package provider

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/testsite"
	"auto-check-in/internal/transport"
	"context"
	"testing"
)

func TestStrictResultMatrix(t *testing.T) {
	tests := []struct {
		name, adapter, body, status string
		http                        int
	}{
		{"newapi success", "newapi", `{"success":true,"data":{"quota_awarded":500}}`, domain.Succeeded, 200},
		{"already", "newapi", `{"success":false,"message":"今日已签到"}`, domain.AlreadyDone, 200},
		{"html false positive", "newapi", "<html>not a success</html>", domain.Unverified, 200},
		{"string success", "newapi", `{"success":"true"}`, domain.Unverified, 200},
		{"missing success", "newapi", `{"data":{"quota":999}}`, domain.Unverified, 200},
		{"trailing data", "newapi", `{"success":true} garbage`, domain.Unverified, 200},
		{"rejected", "newapi", `{"success":false,"message":"签到功能未开启"}`, domain.Failed, 200},
		{"captcha", "newapi", `{"success":false,"message":"需要验证码"}`, domain.NeedsAttention, 200},
		{"auth", "newapi", `{}`, domain.NeedsAttention, 401},
		{"rate limited", "newapi", `{}`, domain.Failed, 429},
		{"code without acknowledgement", "newapi", `{"code":0}`, domain.Unverified, 200},
		{"any explicit rejection wins", "anyrouter", `{"code":0,"success":false}`, domain.Failed, 200},
		{"agent true", "agentrouter", `{"success":true,"data":{"checked_in":true}}`, domain.Succeeded, 200},
		{"agent false", "agentrouter", `{"success":true,"data":{"checked_in":false}}`, domain.AlreadyDone, 200},
		{"agent missing", "agentrouter", `{"success":true,"data":{"quota":12500000}}`, domain.Unverified, 200},
		{"agent login rule", "agentrouter", `{"success":true,"data":{"id":12}}`, domain.Succeeded, 200},
		{"cloudflare HTML", "newapi", `<!doctype html><html><title>Just a moment...</title></html>`, domain.NeedsAttention, 403},
		{"turnstile", "newapi", `{"success":false,"message":"Turnstile token 为空"}`, domain.NeedsAttention, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Normalize(tc.adapter, port.Response{Status: tc.http, Body: []byte(tc.body)})
			if r.Status != tc.status {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestTwoFactorIsNotLoginCompletion(t *testing.T) {
	r := loginFailure(port.Response{Status: 200, Body: []byte(`{"success":true,"data":{"require_2fa":true}}`)})
	if r == nil || r.Status != domain.NeedsAttention || r.Evidence != "two_factor_required" {
		t.Fatalf("two-factor challenge accepted as authenticated: %+v", r)
	}
}
func TestHTTPWorkflowAndRewardIsNotBalanceDelta(t *testing.T) {
	fixture := testsite.New()
	defer fixture.Close()
	site := domain.Templates()[2].Site
	site.BaseURL = fixture.Server.URL
	runner := Runner{Factory: transport.Factory{}}
	for _, user := range []string{"alice", "bob"} {
		creds := domain.Credentials{Method: "password", Username: user, Password: "test-password"}
		result, updated := runner.Execute(context.Background(), site, creds)
		if result.Status != domain.Succeeded || result.Reward == nil || *result.Reward != 500 {
			t.Fatalf("%s: %+v", user, result)
		}
		if result.BalanceAfter == nil || *result.BalanceAfter != 1400 {
			t.Fatalf("wrong balance %+v", result)
		}
		if updated == nil || updated.APIUser != user || len(updated.Cookies) == 0 {
			t.Fatal("session not persisted")
		}
		if fixture.Count("checkin:"+user) != 1 {
			t.Fatal("wrong account context")
		}
		again, _ := runner.Execute(context.Background(), site, *updated)
		if again.Status != domain.AlreadyDone {
			t.Fatal(again)
		}
	}
}
func TestAgentCookieDoesNotPretendToCheckIn(t *testing.T) {
	site := domain.Templates()[1].Site
	result, _ := (&Runner{}).Execute(context.Background(), site, domain.Credentials{Method: "cookie"})
	if result.Status != domain.NeedsAttention {
		t.Fatal(result)
	}
}
