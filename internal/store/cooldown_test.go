package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"auto-check-in/internal/domain"
)

func TestRetryAfterSurvivesNextDayRestartEditsAndLogin(t *testing.T) {
	s, path, vault := testStore(t)
	a, creds := addAccount(t, s, "cooldown")
	ctx := context.Background()
	now := testNow.Add(23 * time.Hour)
	run, err := s.Enqueue(ctx, a.ID, "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true, RetryAfterSeconds: 86400}, now)
	until := now.Add(24 * time.Hour)
	check := func() domain.Account {
		t.Helper()
		got, err := s.Account(ctx, a.ID)
		if err != nil || got.RetryNotBefore == nil || !got.RetryNotBefore.Equal(until) || got.NextRunAt == nil || got.NextRunAt.Before(until) || !got.Schedule.WithinWindow(*got.NextRunAt) {
			t.Fatalf("lost cross-day lower bound: %+v %v", got, err)
		}
		return got
	}
	a = check()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a = check()
	a.Schedule.WindowStart, a.Schedule.WindowEnd, a.Schedule.RetryUntil = "01:00", "02:00", "02:00"
	if _, err = s.SaveAccount(ctx, a, creds, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	a = check()
	login, err := s.BeginLogin(ctx, a.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishLogin(ctx, login.ID, &creds, domain.Succeeded, "saved", now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	check()
	for _, trigger := range []string{"manual", "manual_retry", "scheduled"} {
		if _, err = s.Enqueue(ctx, a.ID, trigger, until.Add(-time.Second)); !errors.Is(err, domain.ErrRateLimited) {
			t.Fatal(trigger, err)
		}
	}
	backup, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dest, _, _ := testStore(t)
	if err = dest.Restore(ctx, backup); err != nil {
		t.Fatal(err)
	}
	restored, _ := dest.Account(ctx, a.ID)
	if restored.RetryNotBefore == nil || !restored.RetryNotBefore.Equal(until) || restored.NextRunAt.Before(until) {
		t.Fatal("backup lost cooldown", restored)
	}
	if _, err = s.Enqueue(ctx, a.ID, "manual", until); err != nil {
		t.Fatal("cooldown did not expire", err)
	}
}

func TestCooldownReschedulesDueWorkAndRejectsClockRollback(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "cooldown")
	ctx := context.Background()
	run, _ := s.Enqueue(ctx, a.ID, "manual", testNow)
	finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true, RetryAfterSeconds: 86400}, testNow)
	until := testNow.Add(24 * time.Hour)
	if _, err := s.db.Exec("UPDATE accounts SET next_run_at=? WHERE id=?", millis(testNow), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, a.ID, "scheduled", testNow.Add(time.Second)); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatal(err)
	}
	if due, err := s.DueAccounts(ctx, testNow.Add(time.Second)); err != nil || len(due) != 0 {
		t.Fatal("cooldown caused a scheduler spin", due, err)
	}
	queued, err := s.Enqueue(ctx, a.ID, "manual", until)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, domain.ClaimOptions{AllowScheduled: true, Now: until.Add(-time.Second)})
	if err != nil || claimed != nil {
		t.Fatal("clock rollback bypassed cooldown", claimed, err)
	}
	if s.QueueRevision() == 0 {
		t.Fatal("implicit reschedule did not notify the engine")
	}
	got, _ := getRun(ctx, s.db, queued.ID)
	if got.Status != domain.Cancelled || got.StartedAt != nil || got.Result.Evidence != "rate_limit_deferred" {
		t.Fatal(got)
	}
}

func TestV5UpgradePreservesDataAndRecoversCooldown(t *testing.T) {
	s, path, vault := testStore(t)
	a, creds := addAccount(t, s, "upgrade")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	run, err := s.Enqueue(ctx, a.ID, "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true, RetryAfterSeconds: 86400}, now)
	var before []byte
	if err = s.db.QueryRow("SELECT secret FROM accounts WHERE id=?", a.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Reconstruct an actual v5 schema and its old next-day random placement.
	if _, err = s.db.Exec(`ALTER TABLE accounts DROP COLUMN retry_not_before;
DROP INDEX run_status_history; DROP INDEX run_action_history; PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE accounts SET next_run_at=? WHERE id=?", millis(a.Schedule.Next(now, a.ID, true)), a.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Account(ctx, a.ID)
	until := now.Add(24 * time.Hour)
	if err != nil || got.RetryNotBefore == nil || !got.RetryNotBefore.Equal(until) || got.NextRunAt.Before(until) {
		t.Fatalf("upgrade failed: %+v %v", got, err)
	}
	var after []byte
	var version int
	s.db.QueryRow("SELECT secret FROM accounts WHERE id=?", a.ID).Scan(&after)
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	decoded, err := s.Credentials(ctx, a.ID)
	if err != nil || !bytes.Equal(before, after) || decoded.CookieHeader != creds.CookieHeader || version != schemaVersion {
		t.Fatal("upgrade changed credentials or version", version, err)
	}
	page, err := s.Runs(ctx, domain.RunFilter{})
	if err != nil || page.Total != 1 || page.Items[0].ID != run.ID {
		t.Fatal("upgrade lost history", page, err)
	}
}

func TestV5UpgradeFailureRollsBackDDL(t *testing.T) {
	s, _, _ := testStore(t)
	if _, err := s.db.Exec(`ALTER TABLE accounts DROP COLUMN retry_not_before;
DROP INDEX run_status_history; DROP INDEX run_action_history;
CREATE TABLE run_status_history(id INTEGER); PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	if err := s.initialize(); err == nil {
		t.Fatal("broken migration unexpectedly committed")
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	_, err := s.db.Exec("SELECT retry_not_before FROM accounts")
	if version != 5 || err == nil || !strings.Contains(err.Error(), "retry_not_before") {
		t.Fatal("failed migration left partial schema", version, err)
	}
}
