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
}

func (e *trackingExecutor) Execute(ctx context.Context, site domain.Site, c domain.Credentials) (domain.Result, *domain.Credentials) {
	if e.panic {
		panic("test failure")
	}
	e.mu.Lock()
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
