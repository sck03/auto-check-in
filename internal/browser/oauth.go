package browser

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"auto-check-in/internal/port"
	"github.com/chromedp/cdproto/runtime"
)

//go:embed oauth.js
var oauthScript string

func (s *Session) oauthDriver(method string) string {
	return "(" + oauthScript + ")(" + jsonJS(s.site.Selectors) + "," + jsonJS(method) + "," + jsonJS(s.site.BaseURL) + "," + jsonJS(s.site.LoginPath) + "," + jsonJS(s.site.Adapter == "agentrouter") + ")"
}

func (s *Session) selectOAuthEntry(ctx context.Context, method string) error {
	driver := s.oauthDriver(method)
	var state string
	if err := s.pollPage(ctx, driver+".inspect('entry')", &state, 20*time.Second); err != nil {
		return err
	}
	if state == "left" {
		return nil // The user or an existing session already left the login page.
	}
	if state == "ready" {
		// Poll only the read. A lost click response must never submit OAuth twice.
		if err := s.run(ctx, cdpEvaluate(driver+".click('entry')", &state, oauthUserGesture)); err != nil {
			return port.ErrUncertain
		}
		if state == "clicked" || state == "left" {
			return nil
		}
	}
	return fmt.Errorf("%s 登录入口不可用（%s），请检查选择器或在登录窗口选择对应方式：%w", method, state, port.ErrManualRequired)
}

// Both interactive login and automatic execution stop after a single consent
// action. Password/2FA entry stays with the user; consent resumes afterwards.
func (s *Session) continueOAuth(ctx context.Context, method string) (bool, error) {
	driver := s.oauthDriver(method)
	var state string
	if err := s.run(ctx, cdpEvaluate(driver+".inspect('consent')", &state)); err != nil || state != "ready" {
		return state == "returned", nil // Navigation may replace the document.
	}
	if err := s.run(ctx, cdpEvaluate(driver+".click('consent')", &state, oauthUserGesture)); err != nil {
		return true, port.ErrUncertain
	}
	return state == "clicked" || state == "returned", nil
}

func (s *Session) assistOAuth(ctx context.Context, method string) {
	if err := s.selectOAuthEntry(ctx, method); err != nil {
		return
	}
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if id := s.confirmedUser.Load(); id != nil && *id != "" {
				return
			}
			op, cancel := context.WithTimeout(ctx, 2*time.Second)
			done, err := s.continueOAuth(op, method)
			cancel()
			if done || err != nil {
				return
			}
		}
	}
}

func oauthUserGesture(params *runtime.EvaluateParams) {
	yes := true
	params.UserGesture = &yes // Permit a site's normal window.open authorization flow.
}
