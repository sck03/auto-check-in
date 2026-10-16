package app

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

type trackingExecutor struct {
	mu          sync.Mutex
	active, max int
	origins     map[string]int
	maxOrigin   int
	panic       bool
	deadline    time.Time
}

func (e *trackingExecutor) Execute(ctx context.Context, site domain.Site, c domain.Credentials) (domain.Result, *domain.Credentials) {
	if e.panic {
		panic("test failure")
	}
	e.mu.Lock()
	e.deadline, _ = ctx.Deadline()
	e.active++
	e.origins[site.BaseURL]++
	if e.active > e.max {
		e.max = e.active
	}
	if e.origins[site.BaseURL] > e.maxOrigin {
		e.maxOrigin = e.origins[site.BaseURL]
	}
	e.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(40 * time.Millisecond):
	}
	e.mu.Lock()
	e.active--
	e.origins[site.BaseURL]--
	e.mu.Unlock()
	return domain.Result{Status: domain.Succeeded, Message: "fixture success"}, nil
}
func TestWorkersRespectGlobalAndOriginConcurrency(t *testing.T) {
	v, _ := security.NewVault(bytes.Repeat([]byte{3}, 32))
	repo, err := store.Open(filepath.Join(t.TempDir(), "test.db"), v)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings, _ := repo.Settings(ctx)
	settings.SchedulerEnabled = false
	settings.Concurrency = 2
	settings.SiteGapSeconds = 0
	if _, err = repo.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		siteID := "anyrouter"
		if i%2 == 1 {
			siteID = "agentrouter"
		}
		a := domain.Account{ID: id, SiteID: siteID, Name: id, Enabled: true, Schedule: domain.DefaultSchedule()}
		if _, err = repo.SaveAccount(ctx, a, domain.Credentials{Method: "cookie", APIUser: id}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Enqueue(ctx, id, "manual", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	executor := &trackingExecutor{origins: map[string]int{}}
	engine := NewEngine(repo, executor)
	engine.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, _ := repo.Runs(ctx, domain.RunFilter{})
		finished := 0
		for _, r := range page.Items {
			if r.Status == domain.Succeeded {
				finished++
			}
		}
		if finished == 5 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	cancel()
	engine.Wait()
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.max != 2 || executor.maxOrigin != 1 {
		t.Fatalf("global=%d origin=%d", executor.max, executor.maxOrigin)
	}
	page, _ := repo.Runs(context.Background(), domain.RunFilter{Status: domain.Succeeded})
	if page.Total != 5 {
		t.Fatalf("only %d completed", page.Total)
	}
}
func TestExecutorPanicBecomesUnverified(t *testing.T) {
	e := NewEngine(nil, &trackingExecutor{panic: true})
	result, c := e.safeExecute(context.Background(), domain.Site{}, domain.Credentials{})
	if result.Status != domain.Unverified || c != nil {
		t.Fatal(result)
	}
}

func TestAutomaticExecutionDeadlineStopsAtWindowEnd(t *testing.T) {
	for _, trigger := range []string{"scheduled", "scheduled_retry", "manual"} {
		t.Run(trigger, func(t *testing.T) {
			vault, _ := security.NewVault(bytes.Repeat([]byte{3}, 32))
			repo, err := store.Open(filepath.Join(t.TempDir(), "test.db"), vault)
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			ctx := context.Background()
			now := time.Now()
			schedule := domain.DefaultSchedule()
			schedule.WindowStart, schedule.WindowEnd, schedule.RetryUntil = "00:00", "23:58", "23:59"
			account := domain.Account{ID: "deadline", SiteID: "anyrouter", Name: "deadline", Enabled: true, Schedule: schedule}
			if _, err := repo.SaveAccount(ctx, account, domain.Credentials{Method: "cookie"}, now); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Enqueue(ctx, account.ID, "manual", now); err != nil {
				t.Fatal(err)
			}
			run, err := repo.Claim(ctx, true, now)
			if err != nil || run == nil {
				t.Fatal("claim failed", err)
			}
			run.Trigger = trigger
			executor := &trackingExecutor{origins: map[string]int{}}
			settings := domain.DefaultSettings()
			settings.TaskTimeoutSeconds = 48 * 60 * 60
			NewEngine(repo, executor).execute(ctx, *run, settings)
			_, end := schedule.Bounds(now)
			if trigger == "scheduled_retry" {
				_, end = schedule.RetryBounds(now)
			}
			if trigger == "manual" {
				if executor.deadline.Before(now.Add(47 * time.Hour)) {
					t.Fatal("manual task was restricted to the automatic window")
				}
			} else if !now.Before(end) {
				if !executor.deadline.IsZero() {
					t.Fatal("expired automatic task reached the executor")
				}
			} else if !executor.deadline.Equal(end) {
				t.Fatalf("automatic deadline=%v, expected window end=%v", executor.deadline, end)
			}
		})
	}
}
