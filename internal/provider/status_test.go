package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/transport"
)

func TestStatusFailuresNeverSubmitCheckIn(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		code               int
		posts              int
	}{
		{"rate limit", `{}`, domain.Failed, 429, 0},
		{"outage", `{}`, domain.Failed, 503, 0},
		{"expired", `{}`, domain.NeedsAttention, 401, 0},
		{"challenge", `<html>Just a moment</html>`, domain.NeedsAttention, 200, 0},
		{"challenge on legacy status", `<html>Just a moment</html>`, domain.NeedsAttention, 404, 0},
		{"missing status", `{"success":true}`, domain.NeedsAttention, 200, 0},
		{"malformed", `not JSON`, domain.NeedsAttention, 200, 0},
		{"not checked", `{"success":true,"data":{"checked_in":false}}`, domain.Succeeded, 200, 1},
		{"checked", `{"success":true,"data":{"checked_in":true}}`, domain.AlreadyDone, 200, 0},
		{"legacy GET", `{}`, domain.Succeeded, 405, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/user/self" {
					fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
				} else if r.Method == http.MethodGet {
					w.Header().Set("Retry-After", "900")
					w.WriteHeader(tc.code)
					fmt.Fprint(w, tc.body)
				} else {
					posts.Add(1)
					fmt.Fprint(w, `{"success":true}`)
				}
			}))
			defer server.Close()
			site := domain.Templates()[2].Site
			site.BaseURL = server.URL
			result, _ := (&Runner{Factory: transport.Factory{}}).Execute(context.Background(), site, domain.Credentials{Method: "cookie"})
			if result.Status != tc.status || int(posts.Load()) != tc.posts {
				t.Fatalf("result=%+v posts=%d want=%d", result, posts.Load(), tc.posts)
			}
			if tc.code == 429 && (!result.Retryable || result.RetryAfterSeconds != 900 || result.Stage != "status_check") {
				t.Fatalf("lost rate limit details: %+v", result)
			}
		})
	}
}

func responseJSON(code int, message string) port.Response {
	body, _ := json.Marshal(map[string]any{"success": false, "message": message})
	return port.Response{Status: code, Body: body}
}

func TestAlreadyDoneRequiresWholeAcknowledgement(t *testing.T) {
	for _, msg := range []string{"查询今日已签到状态失败，请稍后重试", "未签到：无法确认是否已经签到", "not already checked in today", "重复签到检测失败"} {
		for _, code := range []int{200, 400, 409} {
			result := Normalize("newapi", responseJSON(code, msg))
			if result.Complete() {
				t.Fatalf("error confirmed completion: %q HTTP %d", msg, code)
			}
		}
	}
	for _, msg := range []string{"今日已签到", "今天已经签到！", "今日已签到，请勿重复签到", "Already checked in today."} {
		if result := Normalize("newapi", responseJSON(200, msg)); result.Status != domain.AlreadyDone {
			t.Fatalf("valid acknowledgement rejected: %q %+v", msg, result)
		}
	}
}
