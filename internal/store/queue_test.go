package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
)

func TestClaimSkipsExpiredWorkWithoutDelayingManualRun(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	end := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	for _, id := range []string{"expired-a", "expired-b", "expired-c"} {
		addAccount(t, s, id)
		if _, err := s.Enqueue(ctx, id, "scheduled", end.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	addAccount(t, s, "manual")
	want, err := s.Enqueue(ctx, "manual", "manual", end)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Claim(ctx, true, end)
	if err != nil || got == nil || got.ID != want.ID {
		t.Fatalf("expired tasks stalled runnable work: %+v, %v", got, err)
	}
	page, err := s.Runs(ctx, domain.RunFilter{Status: domain.Cancelled})
	if err != nil || page.Total != 3 {
		t.Fatalf("expired tasks were not all cancelled: %+v, %v", page, err)
	}
	for _, run := range page.Items {
		if run.StartedAt != nil {
			t.Fatal("expired work consumed an attempt")
		}
	}
}

func TestReducedRetryBudgetDoesNotLeaveAccountPermanentlyDue(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	addAccount(t, s, "retry")
	run, err := s.Enqueue(ctx, "retry", "manual", testNow)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true}, testNow)
	a, err := s.Account(ctx, "retry")
	if err != nil || a.NextRunAt == nil {
		t.Fatal("missing retry", err)
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.MaxRetries = 0
	if _, err = s.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Enqueue(ctx, a.ID, "scheduled", *a.NextRunAt); !errors.Is(err, domain.ErrDailyLimit) {
		t.Fatal("disabled retry was accepted", err)
	}
	ids, err := s.DueAccounts(ctx, *a.NextRunAt)
	if err != nil || len(ids) != 0 {
		t.Fatalf("rejected retry keeps consuming scheduler scans: %v, %v", ids, err)
	}
}

func TestConnectionSettingsSurviveReconnect(t *testing.T) {
	s, _, _ := testStore(t)
	addAccount(t, s, "account")
	// Cancellation or a discarded connection can make database/sql reconnect.
	s.db.SetMaxIdleConns(0)
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000} {
		var got int
		if err := s.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Errorf("%s reset after reconnect: %d, %v", pragma, got, err)
		}
	}
	if err := s.DeleteSite(context.Background(), "anyrouter"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("reconnected database allowed an orphan account: %v", err)
	}
}

func TestDatabasePathRetainsSpecialCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "签到 #100% &+.db")
	vault, _ := security.NewVault(bytes.Repeat([]byte{7}, 32))
	s, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	addAccount(t, s, "portable")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("database was not created at the requested path", err)
	}
	reopened, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Credentials(context.Background(), "portable"); err != nil {
		t.Fatal("database with URI characters could not be reopened", err)
	}
}

func BenchmarkHistoryQueries(b *testing.B) {
	for _, query := range []string{"idle_queue", "daily_attempts"} {
		b.Run(query, func(b *testing.B) {
			s, _, _ := testStore(b)
			addAccount(b, s, "account")
			_, err := s.db.Exec(`WITH RECURSIVE history(n) AS (
				VALUES(1) UNION ALL SELECT n+1 FROM history WHERE n<20000
			) INSERT INTO runs(id,account_id,account_name,site_name,action,trigger,status,attempt,created_at,started_at,finished_at)
			SELECT 'history-'||n,'account','account','site','checkin','scheduled','succeeded',1,n,n,n FROM history`)
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				if query == "idle_queue" {
					_, err = s.Claim(ctx, true, testNow)
				} else {
					_, err = dailyAttempts(ctx, s.db, "account", testNow)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestQueuedAutomaticWorkExpiresAtNine(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "first"
		if retry {
			name = "retry"
		}
		t.Run(name, func(t *testing.T) {
			s, _, _ := testStore(t)
			a, _ := addAccount(t, s, "a")
			ctx := context.Background()
			if retry {
				first, err := s.Enqueue(ctx, a.ID, "manual", testNow)
				if err != nil {
					t.Fatal(err)
				}
				finish(t, s, first.ID, domain.Result{Status: domain.Failed, Retryable: true}, testNow)
			}
			end := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
			queued, err := s.Enqueue(ctx, a.ID, "scheduled", end.Add(-time.Second))
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := s.Claim(ctx, true, end)
			if err != nil || claimed != nil {
				t.Fatalf("automatic work started at 09:00: %+v %v", claimed, err)
			}
			run, err := getRun(ctx, s.db, queued.ID)
			if err != nil || run.Status != domain.Cancelled || run.StartedAt != nil {
				t.Fatalf("expired queue entry was not cancelled: %+v %v", run, err)
			}
		})
	}
}
