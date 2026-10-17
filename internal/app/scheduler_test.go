package app

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type sleepingRepository struct {
	port.Repository
	mu                            sync.Mutex
	next                          time.Time
	scans, claims, queued, prunes int
}

func (r *sleepingRepository) Settings(context.Context) (domain.Settings, error) {
	return domain.DefaultSettings(), nil
}
func (r *sleepingRepository) DueAccounts(_ context.Context, now time.Time) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scans++
	if !r.next.IsZero() && !r.next.After(now) {
		return []string{"due"}, nil
	}
	return nil, nil
}
func (r *sleepingRepository) NextScheduled(context.Context) (*time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next.IsZero() {
		return nil, nil
	}
	next := r.next
	return &next, nil
}
func (r *sleepingRepository) Enqueue(context.Context, string, string, time.Time) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued++
	r.next = time.Time{}
	return domain.Run{}, nil
}
func (r *sleepingRepository) Claim(context.Context, bool, time.Time) (*domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims++
	return nil, nil
}
func (r *sleepingRepository) Prune(context.Context, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prunes++
	return nil
}

func (r *sleepingRepository) counts() [4]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return [4]int{r.scans, r.claims, r.queued, r.prunes}
}

func TestIdleEngineSleepsAndRearmsOnChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		repo := &sleepingRepository{next: time.Now().Add(48 * time.Hour)}
		engine := NewEngine(repo, nil)
		engine.Start(ctx)
		synctest.Wait()
		initial := repo.counts()
		time.Sleep(14 * time.Hour)
		synctest.Wait()
		if got := repo.counts(); got != initial || got[0] != 1 || got[2] != 0 || got[3] != 1 {
			t.Fatalf("idle engine polled storage: %v", got)
		}
		// Changing an account/login/retry must replace the old timer immediately.
		repo.mu.Lock()
		repo.next = time.Now().Add(10 * time.Second)
		repo.mu.Unlock()
		engine.Notify()
		synctest.Wait()
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if repo.counts()[2] != 0 {
			t.Fatal("task ran before its deadline")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if got := repo.counts()[2]; got != 1 {
			t.Fatalf("missed deadline: queued=%d", got)
		}
		finished := repo.counts()
		time.Sleep(40 * time.Second)
		synctest.Wait()
		if repo.counts() != finished {
			t.Fatal("completed queue kept polling")
		}
		cancel()
		engine.Wait()
	})
}

func TestHistoryCleanupRunsAtStartupAndOncePerBeijingDay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		repo := &sleepingRepository{}
		engine := NewEngine(repo, nil)
		engine.Start(ctx)
		synctest.Wait()
		initial := repo.counts()
		_, midnight := domain.DayBounds(time.Now())
		time.Sleep(time.Until(midnight) - time.Millisecond)
		synctest.Wait()
		if repo.counts() != initial {
			t.Fatal("maintenance woke before the next Beijing day")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := repo.counts(); got[3] != 2 || got[1] != initial[1] {
			t.Fatalf("unexpected daily maintenance: %v", got)
		}
		for range 3 {
			engine.Notify()
			synctest.Wait()
		}
		if repo.counts()[3] != 2 {
			t.Fatal("configuration/resume notifications repeated cleanup")
		}
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		if repo.counts()[3] != 3 {
			t.Fatal("missed the next day's cleanup")
		}
		cancel()
		engine.Wait()
	})
}
