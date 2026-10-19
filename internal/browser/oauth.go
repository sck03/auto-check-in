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
	return "(" + oauthScript + ")(" + jsonJS(s.site.Selectors) + "," + jsonJS(method) + "," + jsonJS(s.site.BaseURL) + "," + jsonJS(s.site.LoginPath) + ")"
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

// Run only while automatic Login is waiting for its callback. Interactive
// windows select the provider, leaving passwords, consent and 2FA to the user.
func (s *Session) continueOAuth(ctx context.Context, attempted *bool) error {
	if *attempted || (s.creds.Method != "github" && s.creds.Method != "linuxdo") {
		return nil
	}
	driver := s.oauthDriver(s.creds.Method)
	var state string
	if err := s.run(ctx, cdpEvaluate(driver+".inspect('consent')", &state)); err != nil || state != "ready" {
		return nil // Navigation may temporarily replace the document.
	}
	*attempted = true
	if err := s.run(ctx, cdpEvaluate(driver+".click('consent')", &state, oauthUserGesture)); err != nil {
		return port.ErrUncertain
	}
	return nil
}

func oauthUserGesture(params *runtime.EvaluateParams) {
	yes := true
	params.UserGesture = &yes // Permit a site's normal window.open authorization flow.
}
