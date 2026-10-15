package browser

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"os"
	"time"
)

type PoolStats struct {
	Running            bool `json:"running"`
	Active             int  `json:"active"`
	Capacity           int  `json:"capacity"`
	Starts             int  `json:"starts"`
	IdleReleaseSeconds int  `json:"idle_release_seconds"`
}

func (f *Factory) Stats() PoolStats {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	return PoolStats{f.poolCtx != nil, f.active, cap(f.slots), f.starts, int(f.config.IdleTimeout.Seconds())}
}
func (f *Factory) Diagnostics() any { return f.Stats() }
func (f *Factory) acquirePool() (context.Context, error) {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	if f.closed {
		return nil, fmt.Errorf("浏览器执行器已关闭")
	}
	if f.idle != nil {
		f.idle.Stop()
		f.idle = nil
	}
	if f.poolCtx != nil {
		select {
		case <-chromedp.FromContext(f.poolCtx).Browser.LostConnection:
			f.stopPoolLocked()
		default:
		}
	}
	if f.poolCtx == nil {
		if err := os.MkdirAll(f.config.TempDir, 0700); err != nil {
			return nil, err
		}
		profile, err := os.MkdirTemp(f.config.TempDir, "browser-pool-")
		if err != nil {
			return nil, err
		}
		opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
		opts = append(opts, chromedp.ExecPath(f.config.Executable), chromedp.UserDataDir(profile), chromedp.Flag("headless", "new"),
			chromedp.Flag("disable-gpu", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.WindowSize(1200, 800), chromedp.Flag("no-sandbox", f.config.NoSandbox))
		if f.config.ProxyURL != "" {
			opts = append(opts, chromedp.ProxyServer(f.config.ProxyURL))
		}
		allocator, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
		root, cancel := chromedp.NewContext(allocator)
		initTimer := time.AfterFunc(30*time.Second, cancel)
		err = chromedp.Do(root)
		initTimer.Stop()
		if err != nil {
			cancel()
			allocCancel()
			_ = os.RemoveAll(profile)
			return nil, err
		}
		f.poolCtx = root
		f.poolCancel = cancel
		f.poolAllocatorCancel = allocCancel
		f.poolProfile = profile
		f.starts++
	}
	f.active++
	return f.poolCtx, nil
}
func (f *Factory) releasePool() {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	f.active--
	if f.active == 0 && !f.closed {
		f.idle = time.AfterFunc(f.config.IdleTimeout, func() {
			f.poolMu.Lock()
			defer f.poolMu.Unlock()
			if f.active == 0 {
				f.stopPoolLocked()
			}
		})
	}
}
func (f *Factory) stopPoolLocked() {
	if f.poolCtx == nil {
		return
	}
	f.poolCancel()
	f.poolAllocatorCancel()
	_ = os.RemoveAll(f.poolProfile)
	f.poolCtx = nil
	f.poolCancel = nil
	f.poolAllocatorCancel = nil
	f.poolProfile = ""
}
func (f *Factory) Close() {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	f.closed = true
	if f.idle != nil {
		f.idle.Stop()
	}
	f.stopPoolLocked()
}
