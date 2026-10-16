package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"auto-check-in/internal/domain"
)

func TestUpgradeDefaultRetryWindow(t *testing.T) {
	s, path, vault := testStore(t)
	ctx := context.Background()
	old := domain.DefaultSchedule()
	old.RetryUntil = "09:30"
	custom := old
	custom.WindowStart = "08:15"
	late := time.Date(2026, 10, 15, 1, 5, 0, 0, time.UTC)
	for id, schedule := range map[string]domain.Schedule{"default": old, "custom": custom} {
		addAccount(t, s, id)
		if _, err := s.db.Exec("UPDATE accounts SET schedule=?,next_run_at=? WHERE id=?", jsonText(schedule), millis(late), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	for id, expected := range map[string]domain.Schedule{"default": domain.DefaultSchedule(), "custom": custom} {
		account, err := upgraded.Account(ctx, id)
		if err != nil || account.Schedule != expected {
			t.Fatalf("%s: schedule=%+v err=%v", id, account.Schedule, err)
		}
		version := int64(1)
		if id == "default" {
			version = 2
		}
		if account.Version != version {
			t.Fatalf("%s: unexpected account version %d", id, account.Version)
		}
	}
	if err := upgraded.migrate(); err != nil {
		t.Fatal(err)
	}
	account, _ := upgraded.Account(ctx, "default")
	if account.Version != 2 {
		t.Fatal("migration ran more than once")
	}
	if _, err := upgraded.Enqueue(ctx, "default", "scheduled", late); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("old pending time caused a check-in after 09:00: %v", err)
	}
	account, _ = upgraded.Account(ctx, "default")
	if account.NextRunAt == nil || !account.NextRunAt.After(late) || !account.Schedule.WithinWindow(*account.NextRunAt) {
		t.Fatal("old retry was not deferred to the next window")
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
