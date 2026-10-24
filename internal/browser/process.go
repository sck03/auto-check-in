package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

type browserProcess struct {
	ctx   context.Context
	close func()
	once  sync.Once
}

func (p *browserProcess) Close() { p.once.Do(p.close) }

func (f *Factory) startProcess(ctx context.Context, visible bool) (*browserProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.lifetime.Err(); err != nil {
		return nil, err
	}
	opts, err := LaunchOptions(visible)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(f.config.TempDir, 0700); err != nil {
		return nil, err
	}
	prefix, height := "browser-pool-", 800
	if visible {
		prefix, height = "login-window-", 850
	}
	profile, err := os.MkdirTemp(f.config.TempDir, prefix)
	if err != nil {
		return nil, err
	}
	opts = append(opts, chromedp.ExecPath(f.config.Executable), chromedp.UserDataDir(profile),
		chromedp.Flag("headless", !visible), chromedp.Flag("hide-scrollbars", !visible),
		chromedp.Flag("disable-gpu", !visible), chromedp.Flag("disable-dev-shm-usage", !visible),
		chromedp.WindowSize(1200, height), chromedp.Flag("no-sandbox", f.config.NoSandbox))
	if f.config.ProxyURL != "" {
		opts = append(opts, chromedp.ProxyServer(f.config.ProxyURL))
	}
	allocator, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	root, cancel := chromedp.NewContext(allocator)
	process := &browserProcess{ctx: root, close: func() {
		CloseProcess(root, cancel, allocCancel)
		f.cleanupProfile(profile)
	}}
	stopRequest := context.AfterFunc(ctx, cancel)
	stopShutdown := context.AfterFunc(f.lifetime, cancel)
	timer := time.AfterFunc(30*time.Second, cancel)
	err = chromedp.Do(root)
	timer.Stop()
	stopRequest()
	stopShutdown()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		process.Close()
		return nil, f.startupError(ctx, "浏览器进程初始化", err)
	}
	return process, nil
}

// CloseProcess gives Chromium a bounded graceful close before the cancellation
// fallback, so cache writers can release their handles before profile removal.
func CloseProcess(ctx context.Context, cancel, closeAllocator context.CancelFunc) {
	if owner := chromedp.FromContext(ctx); owner != nil && owner.Browser != nil && ctx.Err() == nil {
		deadline, done := context.WithTimeout(ctx, 2*time.Second)
		_ = chromedp.Cancel(deadline)
		done()
	}
	cancel()
	closeAllocator()
}

// RemoveProfile is called only after the owning browser/allocator has stopped.
// Chromium child processes can retain Windows file handles briefly after the
// main process exits. Bound retries to teardown; never add idle maintenance.
func RemoveProfile(profile string) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := os.RemoveAll(profile)
		if err == nil || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Human login starts with normal browser defaults. The local desktop shell may
// append resource limits that are not applied to third-party login windows.
// Do not disable browser security, background throttling or enable test mode in
// windows the user operates. Every caller still supplies its own user-data-dir.
func LaunchOptions(visible bool) ([]chromedp.ExecAllocatorOption, error) {
	if visible {
		// Both a debugger pipe and port=0 enable Chromium's automation mode.
		// A dedicated non-zero loopback port permits normal human interaction.
		// The allocator reads this child process's endpoint; it never attaches
		// to the user's existing browser, even if a port is taken before launch.
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("无法创建本地浏览器连接：%w", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			return nil, err
		}
		return []chromedp.ExecAllocatorOption{chromedp.NoFirstRun, chromedp.NoDefaultBrowserCheck,
			remote.WebSocket, chromedp.Flag("remote-debugging-address", "127.0.0.1"),
			chromedp.Flag("remote-debugging-port", strconv.Itoa(port))}, nil
	}
	return append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...), nil
}

func (f *Factory) startupError(ctx context.Context, stage string, err error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if cause := f.lifetime.Err(); cause != nil {
		return cause
	}
	// An internal initialization watchdog is a retryable startup failure, not
	// a user-requested shutdown. No site operation has been sent at this stage.
	return fmt.Errorf("%s失败：%v", stage, err)
}
