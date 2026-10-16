// Package desktop contains the small, replaceable Windows display shell.
package desktop

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"auto-check-in/internal/browser"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// Run opens an isolated Edge/Chrome application window. Closing its last window
// ends this function, allowing the caller to gracefully stop the shared core.
func Run(ctx context.Context, baseURL, token, dataDir, browserPath string, ready func(int)) error {
	if browserPath == "" {
		browserPath = browser.FindExecutable()
	}
	if browserPath == "" {
		return fmt.Errorf("请先安装 Microsoft Edge 或 Google Chrome，或设置 CHECKIN_BROWSER_PATH")
	}
	target := baseURL + "/#token=" + url.QueryEscape(token)
	opts, err := browser.LaunchOptions(true)
	if err != nil {
		return err
	}
	opts = append(opts, chromedp.ExecPath(browserPath), chromedp.UserDataDir(filepath.Join(dataDir, "desktop-browser")),
		chromedp.Flag("headless", false), chromedp.Flag("app", target), chromedp.WindowSize(1180, 840),
		chromedp.Flag("no-first-run", true), chromedp.Flag("no-default-browser-check", true))
	// Optional local test hook. Production uses chromedp's ephemeral loopback
	// debugger endpoint, which is never exposed through the application API.
	if raw := os.Getenv("CHECKIN_DESKTOP_TEST_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1024 || port > 65535 {
			return fmt.Errorf("无效的桌面测试端口")
		}
		opts = append(opts, remote.WebSocket, chromedp.Flag("remote-debugging-port", strconv.Itoa(port)))
	}
	alloc, closeAllocator := chromedp.NewExecAllocator(ctx, opts...)
	defer closeAllocator()
	window, cancel := chromedp.NewContext(alloc)
	defer cancel()
	if err := chromedp.Do(window); err != nil {
		return err
	}
	if err := chromedp.Do(window, chromedp.Navigate(target)); err != nil {
		return err
	}
	if ready != nil {
		ready(chromedp.FromContext(window).Browser.Process().Pid)
	}
	<-window.Done()
	return nil
}
