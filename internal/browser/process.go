package browser

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
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
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
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
		cancel()
		allocCancel()
		_ = os.RemoveAll(profile)
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
		return nil, err
	}
	return process, nil
}
