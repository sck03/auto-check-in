package app

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type failingResultRepository struct {
	port.Repository
	calls       int
	fail        int
	expireFirst bool
	saved       domain.Result
	savedAt     time.Time
}

func (r *failingResultRepository) Finish(ctx context.Context, _ string, result domain.Result, c *domain.Credentials, _ domain.Settings, at time.Time) error {
	r.calls++
	if r.calls == 1 && r.expireFirst {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.calls <= r.fail {
		return errors.New("fixture database temporarily locked")
	}
	if c != nil {
		return errors.New("recovery must not retain credentials")
	}
	r.saved, r.savedAt = result, at
	return nil
}

func TestResultFallbackUsesFreshDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &failingResultRepository{expireFirst: true}
		e := NewEngine(repo, nil)
		e.finishResult(pendingResult{id: "run", result: domain.Result{Status: domain.Succeeded}, at: time.Now()})
		if repo.calls != 2 || repo.saved.Status != domain.Unverified || e.Stats().PendingResults != 0 {
			t.Fatalf("fallback failed: calls=%d saved=%s stats=%+v", repo.calls, repo.saved.Status, e.Stats())
		}
	})
}

func TestFailedResultRecoversWithoutRestartOrSiteReplay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &failingResultRepository{fail: 2}
		e := NewEngine(repo, nil)
		at := time.Now()
		e.finishResult(pendingResult{id: "run", result: domain.Result{Status: domain.Succeeded}, at: at, credentials: &domain.Credentials{Token: "fixture"}})
		if e.Stats().PendingResults != 1 {
			t.Fatal("failed write was lost")
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); e.recoveryLoop(ctx) }()
		synctest.Wait()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if repo.calls != 3 || e.Stats().PendingResults != 0 || repo.saved.Status != domain.Unverified || !repo.savedAt.Equal(at) {
			t.Fatalf("recovery failed: calls=%d stats=%+v", repo.calls, e.Stats())
		}
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		if repo.calls != 3 {
			t.Fatal("empty recovery queue kept polling")
		}
		cancel()
		<-done
	})
}
