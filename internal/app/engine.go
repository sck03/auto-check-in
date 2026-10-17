package app

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type originGate struct {
	token chan struct{}
	next  time.Time
}
type Engine struct {
	repo         port.Repository
	executor     port.Executor
	wg           sync.WaitGroup
	mu           sync.Mutex
	gates        map[string]*originGate
	wake         chan struct{}
	scheduleWake chan struct{}
	wakeMu       sync.Mutex
}

func NewEngine(repo port.Repository, executor port.Executor) *Engine {
	return &Engine{repo: repo, executor: executor, gates: map[string]*originGate{}, wake: make(chan struct{}), scheduleWake: make(chan struct{})}
}
func (e *Engine) Start(ctx context.Context) {
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.scheduleLoop(ctx) }()
	for i := 0; i < 4; i++ {
		e.wg.Add(1)
		go func(index int) { defer e.wg.Done(); e.worker(ctx, index) }(i)
	}
}
func (e *Engine) Wait() { e.wg.Wait() }
func (e *Engine) Notify() {
	e.wakeMu.Lock()
	close(e.scheduleWake)
	e.scheduleWake = make(chan struct{})
	e.notifyWorkersLocked()
	e.wakeMu.Unlock()
}
func (e *Engine) notifyWorkersLocked() {
	close(e.wake)
	e.wake = make(chan struct{})
}
func (e *Engine) subscription() <-chan struct{} {
	e.wakeMu.Lock()
	defer e.wakeMu.Unlock()
	return e.wake
}
func (e *Engine) waitForWork(ctx context.Context, wake <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	}
}

func (e *Engine) Tick(ctx context.Context, now time.Time) error {
	settings, err := e.repo.Settings(ctx)
	if err != nil {
		return err
	}
	if !settings.SchedulerEnabled {
		return nil
	}
	ids, err := e.repo.DueAccounts(ctx, now)
	if err != nil {
		return err
	}
	queued := false
	for _, id := range ids {
		_, err = e.repo.Enqueue(ctx, id, "scheduled", now)
		if err == nil {
			queued = true
		}
		if err != nil && !errors.Is(err, domain.ErrBusy) && !errors.Is(err, domain.ErrCooldown) && !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrDisabled) && !errors.Is(err, domain.ErrDailyLimit) {
			return err
		}
	}
	if queued {
		e.wakeMu.Lock()
		e.notifyWorkersLocked()
		e.wakeMu.Unlock()
	}
	return nil
}
func (e *Engine) scheduleLoop(ctx context.Context) {
	nextPrune := time.Time{}
	for ctx.Err() == nil {
		// Subscribe before reading storage so edits/completions during a scan
		// cannot be lost between computing the deadline and going to sleep.
		e.wakeMu.Lock()
		wake := e.scheduleWake
		e.wakeMu.Unlock()
		now := time.Now().UTC()
		err := e.Tick(ctx, now)
		if !now.Before(nextPrune) {
			if settings, err := e.repo.Settings(ctx); err == nil {
				if err = e.repo.Prune(ctx, now.AddDate(0, 0, -settings.RetentionDays)); err != nil && ctx.Err() == nil {
					slog.Error("history cleanup failed", "error", err)
				}
			}
			nextPrune = now.Add(time.Hour)
		}
		deadline := nextPrune
		if err == nil {
			var settings domain.Settings
			settings, err = e.repo.Settings(ctx)
			if err == nil && settings.SchedulerEnabled {
				var next *time.Time
				next, err = e.repo.NextScheduled(ctx)
				if next != nil && next.Before(deadline) {
					deadline = *next
				}
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("scheduler failed", "error", err)
			deadline = time.Now().Add(5 * time.Second)
			// Also allow workers to recover after a transient storage failure.
			e.wakeMu.Lock()
			e.notifyWorkersLocked()
			e.wakeMu.Unlock()
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}
func (e *Engine) worker(ctx context.Context, index int) {
	for ctx.Err() == nil {
		wake := e.subscription()
		settings, err := e.repo.Settings(ctx)
		if err != nil {
			if !wait(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if index >= settings.Concurrency {
			if !e.waitForWork(ctx, wake) {
				return
			}
			continue
		}
		run, err := e.repo.Claim(ctx, settings.SchedulerEnabled, time.Now().UTC())
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("task claim failed", "error", err)
			}
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		if run == nil {
			if !e.waitForWork(ctx, wake) {
				return
			}
			continue
		}
		e.execute(ctx, *run, settings)
	}
}
func (e *Engine) gate(origin string) *originGate {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := e.gates[origin]
	if g == nil {
		g = &originGate{token: make(chan struct{}, 1)}
		e.gates[origin] = g
	}
	return g
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (e *Engine) execute(ctx context.Context, run domain.Run, settings domain.Settings) {
	defer e.Notify()
	result := domain.Result{Status: domain.Failed, Message: "无法读取账号配置", Evidence: "configuration_error"}
	var updated *domain.Credentials
	a, err := e.repo.Account(ctx, run.AccountID)
	if err == nil {
		var site domain.Site
		var creds domain.Credentials
		site, err = e.repo.Site(ctx, a.SiteID)
		if err == nil {
			creds, err = e.repo.Credentials(ctx, a.ID)
		}
		if err == nil {
			g := e.gate(site.BaseURL)
			acquired := false
			select {
			case g.token <- struct{}{}:
				acquired = true
			case <-ctx.Done():
			}
			if acquired {
				if wait(ctx, time.Until(g.next)) {
					now := time.Now()
					start, end := a.Schedule.Bounds(now)
					if run.Trigger == "scheduled_retry" {
						start, end = a.Schedule.RetryBounds(now)
					}
					automatic := run.Trigger == "scheduled" || run.Trigger == "scheduled_retry"
					if automatic && (now.Before(start) || !now.Before(end)) {
						result = domain.Result{Status: domain.Cancelled, Message: "等待期间已超过本日窗口，安排至下次窗口", Evidence: "schedule_deferred"}
					} else {
						deadline := now.Add(time.Duration(settings.TaskTimeoutSeconds) * time.Second)
						if automatic && end.Before(deadline) {
							deadline = end
						}
						taskCtx, cancel := context.WithDeadline(ctx, deadline)
						result, updated = e.safeExecute(taskCtx, site, creds)
						cancel()
					}
				} else {
					result = domain.Result{Status: domain.Interrupted, Message: "任务在执行前被中断", Evidence: "shutdown"}
				}
				g.next = time.Now().Add(time.Duration(settings.SiteGapSeconds) * time.Second)
				<-g.token
			} else {
				result = domain.Result{Status: domain.Interrupted, Message: "任务在执行前被中断", Evidence: "shutdown"}
			}
		}
	}
	if ctx.Err() != nil && result.Status == domain.Failed {
		result = domain.Result{Status: domain.Interrupted, Message: "程序退出导致任务中断", Evidence: "shutdown"}
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = e.repo.Finish(finishCtx, run.ID, result, updated, settings, time.Now().UTC()); err != nil {
		slog.Error("task result could not be saved", "run_id", run.ID, "error", err)
		fallback := domain.Result{Status: domain.Unverified, Message: "执行结束，但会话或结果保存失败；请核对站点结果", Evidence: "persistence_error"}
		if err = e.repo.Finish(finishCtx, run.ID, fallback, nil, settings, time.Now().UTC()); err != nil {
			slog.Error("task requires recovery", "run_id", run.ID, "error", err)
		}
	}
}
func (e *Engine) safeExecute(ctx context.Context, site domain.Site, c domain.Credentials) (result domain.Result, updated *domain.Credentials) {
	defer func() {
		if recover() != nil {
			result = domain.Result{Status: domain.Unverified, Message: "执行器异常退出，请核对站点结果", Evidence: "executor_panic"}
			updated = nil
		}
	}()
	return e.executor.Execute(ctx, site, c)
}
