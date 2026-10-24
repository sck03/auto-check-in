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

type Engine struct {
	repo         port.Repository
	executor     port.Executor
	wg           sync.WaitGroup
	mu           sync.Mutex
	gates        map[string]originState
	wake         chan struct{}
	scheduleWake chan struct{}
	wakeMu       sync.Mutex
	persistMu    sync.Mutex
	pending      map[string]pendingResult
	recoveryWake chan struct{}
	saveFailures uint64
}

func NewEngine(repo port.Repository, executor port.Executor) *Engine {
	return &Engine{repo: repo, executor: executor, gates: map[string]originState{}, wake: make(chan struct{}), scheduleWake: make(chan struct{}), pending: map[string]pendingResult{}, recoveryWake: make(chan struct{}, 1)}
}
func (e *Engine) Start(ctx context.Context) {
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.scheduleLoop(ctx) }()
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.recoveryLoop(ctx) }()
	for i := 0; i < 4; i++ {
		e.wg.Add(1)
		go func(index int) { defer e.wg.Done(); e.worker(ctx, index) }(i)
	}
}
func (e *Engine) Wait() {
	e.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.retryPending(ctx)
}
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
func (e *Engine) waitForWork(ctx context.Context, wake <-chan struct{}, deadline time.Time, capacityWake <-chan struct{}) bool {
	var tick <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		tick = timer.C
	}
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-capacityWake:
		return true
	case <-tick:
		return true
	}
}

func (e *Engine) enqueueDue(ctx context.Context, now time.Time) error {
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
		if err != nil && !errors.Is(err, domain.ErrBusy) && !errors.Is(err, domain.ErrCooldown) && !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrDisabled) && !errors.Is(err, domain.ErrDailyLimit) && !errors.Is(err, domain.ErrRateLimited) {
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
		settings, err := e.repo.Settings(ctx)
		if err == nil {
			if !now.Before(nextPrune) {
				if err := e.repo.Prune(ctx, now.AddDate(0, 0, -settings.RetentionDays)); err != nil && ctx.Err() == nil {
					slog.Error("history cleanup failed", "error", err)
				}
				// One attempt at startup and per Beijing day. Maintenance never
				// needs an hourly timer or repeated settings reads.
				_, nextPrune = domain.DayBounds(now)
			}
			if settings.SchedulerEnabled {
				err = e.enqueueDue(ctx, now)
			}
		}
		deadline := nextPrune
		if err == nil && settings.SchedulerEnabled {
			var next *time.Time
			next, err = e.repo.NextScheduled(ctx)
			if next != nil && next.Before(deadline) {
				deadline = *next
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
			if !e.waitForWork(ctx, wake, time.Time{}, nil) {
				return
			}
			continue
		}
		capacity := port.Capacity{}
		if source, ok := e.executor.(port.CapacitySource); ok {
			capacity = source.Capacity()
		}
		run, origin, next, err := e.claim(ctx, settings, capacity.BlockedTransports...)
		if err != nil {
			if run != nil {
				e.finishResult(pendingResult{id: run.ID, result: domain.Result{Status: domain.Failed, Message: "读取任务配置失败，未访问站点", Evidence: "configuration_error", Retryable: true}, settings: settings, at: time.Now().UTC()})
				e.Notify()
			}
			if ctx.Err() == nil {
				slog.Error("task claim failed", "error", err)
			}
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		if run == nil {
			if !e.waitForWork(ctx, wake, next, capacity.Changed) {
				return
			}
			continue
		}
		e.execute(ctx, *run, settings, func() { e.releaseOrigin(origin, settings.SiteGapSeconds) })
	}
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
func (e *Engine) execute(ctx context.Context, run domain.Run, settings domain.Settings, onExecuted func()) {
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
		}
	}
	if ctx.Err() != nil && result.Status == domain.Failed {
		result = domain.Result{Status: domain.Interrupted, Message: "程序退出导致任务中断", Evidence: "shutdown"}
	}
	if onExecuted != nil {
		onExecuted()
	}
	e.finishResult(pendingResult{id: run.ID, result: result, credentials: updated, settings: settings, at: time.Now().UTC()})
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
