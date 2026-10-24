package browser

import (
	"context"
	"time"

	"github.com/chromedp/chromedp"
)

type PoolStats struct {
	Running            bool           `json:"running"`
	Active             int            `json:"active"`
	Capacity           int            `json:"capacity"`
	Starts             int            `json:"starts"`
	IdleReleaseSeconds int            `json:"idle_release_seconds"`
	NativeWindows      int            `json:"native_windows"`
	ProfileCleanup     ProfileCleanup `json:"profile_cleanup"`
}

func (f *Factory) Stats() PoolStats {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	return PoolStats{Running: f.pool != nil || len(f.visible) > 0, Active: f.active + len(f.visible), Capacity: cap(f.slots), Starts: f.starts, IdleReleaseSeconds: int(f.config.IdleTimeout.Seconds()), NativeWindows: len(f.visible), ProfileCleanup: f.cleanup}
}
func (f *Factory) Diagnostics() any { return f.Stats() }
func (f *Factory) acquirePool(ctx context.Context) (context.Context, error) {
	for {
		f.poolMu.Lock()
		if f.closed || ctx.Err() != nil {
			f.poolMu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		}
		if ready := f.starting; ready != nil {
			f.poolMu.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.lifetime.Done():
				return nil, f.lifetime.Err()
			}
		}
		if f.idle != nil {
			f.idle.Stop()
			f.idle = nil
		}
		if process := f.pool; process != nil {
			select {
			case <-chromedp.FromContext(process.ctx).Browser.LostConnection:
				f.pool = nil
				f.teardowns.Add(1)
				f.poolMu.Unlock()
				process.Close()
				f.teardowns.Done()
				continue
			default:
				f.active++
				f.poolMu.Unlock()
				return process.ctx, nil
			}
		}
		ready := make(chan struct{})
		f.starting = ready
		f.poolMu.Unlock()

		// Process startup and teardown never hold the bookkeeping lock. Other
		// accounts, diagnostics and shutdown remain responsive during startup.
		process, err := f.startProcess(ctx, false)
		f.poolMu.Lock()
		if err == nil && (f.closed || ctx.Err() != nil) {
			err = context.Canceled
		}
		if err == nil {
			f.pool = process
			f.active++
			f.starts++
		}
		f.starting = nil
		close(ready)
		f.poolMu.Unlock()
		if err != nil {
			if process != nil {
				process.Close()
			}
			return nil, err
		}
		return process.ctx, nil
	}
}
func (f *Factory) releasePool() {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	f.active--
	if f.active != 0 || f.closed || f.pool == nil {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(f.config.IdleTimeout, func() {
		f.poolMu.Lock()
		if f.idle != timer || f.active != 0 {
			f.poolMu.Unlock()
			return
		}
		process := f.pool
		f.pool, f.idle = nil, nil
		f.teardowns.Add(1)
		f.poolMu.Unlock()
		defer f.teardowns.Done()
		if process != nil {
			process.Close()
		}
	})
	f.idle = timer
}
func (f *Factory) Close() {
	f.shutdown()
	f.poolMu.Lock()
	if f.closed {
		f.poolMu.Unlock()
		return
	}
	f.closed = true
	if f.idle != nil {
		f.idle.Stop()
		f.idle = nil
	}
	process := f.pool
	f.pool = nil
	visible := make([]*Session, 0, len(f.visible))
	for session := range f.visible {
		visible = append(visible, session)
	}
	f.poolMu.Unlock()
	if process != nil {
		process.Close()
	}
	for _, session := range visible {
		session.Close()
	}
	f.teardowns.Wait()
}
