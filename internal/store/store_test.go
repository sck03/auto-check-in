package store

import (
	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 15, 0, 5, 0, 0, time.UTC)

func testStore(t *testing.T) (*Store, string, *security.Vault) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checkin.db")
	v, _ := security.NewVault(bytes.Repeat([]byte{7}, 32))
	s, err := Open(path, v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path, v
}
func addAccount(t *testing.T, s *Store, id string) (domain.Account, domain.Credentials) {
	t.Helper()
	c := domain.Credentials{Method: "cookie", APIUser: id, CookieHeader: "session=private-" + id}
	a, err := s.SaveAccount(context.Background(), domain.Account{ID: id, SiteID: "anyrouter", Name: id, Enabled: true, Schedule: domain.DefaultSchedule()}, c, testNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return a, c
}
func finish(t *testing.T, s *Store, id string, result domain.Result, now time.Time) domain.Run {
	t.Helper()
	ctx := context.Background()
	r, err := s.Claim(ctx, true, now)
	if err != nil || r == nil {
		t.Fatalf("claim: %v %+v", err, r)
	}
	if r.ID != id {
		t.Fatal("wrong run")
	}
	if err = s.Finish(ctx, id, result, nil, domain.DefaultSettings(), now); err != nil {
		t.Fatal(err)
	}
	return *r
}
func TestEncryptedStorageAndOptimisticUpdate(t *testing.T) {
	s, _, _ := testStore(t)
	a, c := addAccount(t, s, "a")
	ctx := context.Background()
	var sealed []byte
	if err := s.db.QueryRow("SELECT secret FROM accounts WHERE id='a'").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("private-a")) {
		t.Fatal("plaintext secret stored")
	}
	list, _ := s.Accounts(ctx)
	raw, _ := json.Marshal(list)
	if bytes.Contains(raw, []byte("private-a")) {
		t.Fatal("secret in public projection")
	}
	a.Name = "renamed"
	saved, err := s.SaveAccount(ctx, a, c, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if *saved.NextRunAt != *a.NextRunAt {
		t.Fatal("renaming changed schedule")
	}
	if _, err = s.SaveAccount(ctx, a, c, testNow); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("lost update accepted", err)
	}
	cookie, err := s.Credentials(ctx, a.ID)
	if err != nil || cookie.CookieHeader != c.CookieHeader {
		t.Fatal("secret lost on edit")
	}
}
func TestConcurrentEnqueueHasOneWinner(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "a")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Enqueue(context.Background(), a.ID, "manual", testNow)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, domain.ErrBusy) {
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	if err := s.DeleteAccount(context.Background(), a.ID); !errors.Is(err, domain.ErrBusy) {
		t.Fatal("active account deleted")
	}
}
func TestSuccessBlocksEveryFurtherAttemptThatDay(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "a")
	ctx := context.Background()
	run, err := s.Enqueue(ctx, a.ID, "manual", testNow)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, run.ID, domain.Result{Status: domain.Succeeded}, testNow)
	for _, trigger := range []string{"manual", "manual_retry", "scheduled"} {
		if _, err = s.Enqueue(ctx, a.ID, trigger, testNow.Add(time.Minute)); !errors.Is(err, domain.ErrCooldown) {
			t.Fatal(trigger, err)
		}
	}
	account, _ := s.Account(ctx, a.ID)
	if account.NextRunAt == nil || account.NextRunAt.Before(testNow.Add(23*time.Hour)) {
		t.Fatal(account.NextRunAt)
	}
}
func TestRetryBudgetSurvivesRestartAndStopsAfterThreeAttempts(t *testing.T) {
	s, path, v := testStore(t)
	a, _ := addAccount(t, s, "a")
	ctx := context.Background()
	now := testNow
	run, err := s.Enqueue(ctx, a.ID, "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		r := finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true}, now)
		if r.Attempt != attempt {
			t.Fatalf("attempt %d: %+v", attempt, r)
		}
		account, _ := s.Account(ctx, a.ID)
		if attempt == 3 {
			if account.NextRunAt.Before(now.Add(20 * time.Hour)) {
				t.Fatal("unbounded retries", account.NextRunAt)
			}
			break
		}
		delay := account.NextRunAt.Sub(now)
		min := 5 * time.Minute
		if attempt == 2 {
			min = 15 * time.Minute
		}
		if delay < min || delay > min+30*time.Second {
			t.Fatal("wrong backoff", delay)
		}
		now = *account.NextRunAt
		_ = s.Close()
		s, err = Open(path, v)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if err = s.Recover(ctx, now); err != nil {
			t.Fatal(err)
		}
		run, err = s.Enqueue(ctx, a.ID, "scheduled", now)
		if err != nil {
			t.Fatal(err)
		}
		if run.Trigger != "scheduled_retry" {
			t.Fatal(run.Trigger)
		}
	}
	if _, err = s.Enqueue(ctx, a.ID, "manual", now); !errors.Is(err, domain.ErrDailyLimit) {
		t.Fatal(err)
	}
}
func TestRetryAfterOutsideCutoffDefersToTomorrow(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "a")
	ctx := context.Background()
	run, _ := s.Enqueue(ctx, a.ID, "manual", testNow)
	finish(t, s, run.ID, domain.Result{Status: domain.Failed, Retryable: true, RetryAfterSeconds: 7200}, testNow)
	current, _ := s.Account(ctx, a.ID)
	if current.NextRunAt.Before(testNow.Add(20 * time.Hour)) {
		t.Fatal("retry ignored cutoff", current.NextRunAt)
	}
}
func TestRecoveryKeepsQueueAndPausesAmbiguousWork(t *testing.T) {
	s, path, v := testStore(t)
	addAccount(t, s, "a")
	addAccount(t, s, "b")
	ctx := context.Background()
	_, _ = s.Enqueue(ctx, "a", "manual", testNow)
	_, _ = s.Enqueue(ctx, "b", "manual", testNow.Add(time.Second))
	r, err := s.Claim(ctx, true, testNow)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path, v)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Recover(ctx, testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Account(ctx, r.AccountID)
	if a.LastStatus != domain.Interrupted || a.NextRunAt != nil {
		t.Fatal(a)
	}
	page, _ := s.Runs(ctx, domain.RunFilter{})
	counts := map[string]int{}
	for _, run := range page.Items {
		counts[run.Status]++
	}
	if counts[domain.Queued] != 1 || counts[domain.Interrupted] != 1 {
		t.Fatal(counts)
	}
}
func TestUnknownResultStaysPausedAfterMetadataEdit(t *testing.T) {
	s, _, _ := testStore(t)
	a, c := addAccount(t, s, "a")
	ctx := context.Background()
	run, _ := s.Enqueue(ctx, a.ID, "manual", testNow)
	finish(t, s, run.ID, domain.Result{Status: domain.Unverified}, testNow)
	a, _ = s.Account(ctx, a.ID)
	a.Name = "new label"
	a, err := s.SaveAccount(ctx, a, c, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.NextRunAt != nil {
		t.Fatal("metadata edit resumed unknown result")
	}
}
func TestCancelQueueDoesNotImmediatelyRequeue(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "a")
	ctx := context.Background()
	run, _ := s.Enqueue(ctx, a.ID, "manual", testNow)
	if err := s.CancelQueued(ctx, run.ID, testNow); err != nil {
		t.Fatal(err)
	}
	ids, err := s.DueAccounts(ctx, testNow.Add(time.Minute))
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
}
func TestLateRestartDoesNotRunOutsideWindow(t *testing.T) {
	s, _, _ := testStore(t)
	a, _ := addAccount(t, s, "a")
	ctx := context.Background()
	late := testNow.Add(4 * time.Hour)
	if _, err := s.Enqueue(ctx, a.ID, "scheduled", late); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	a, _ = s.Account(ctx, a.ID)
	if a.NextRunAt.Before(late) || !a.Schedule.WithinWindow(*a.NextRunAt) {
		t.Fatal(a.NextRunAt)
	}
}
func TestBackupRestoreAndAtomicRollback(t *testing.T) {
	s, _, _ := testStore(t)
	a, c := addAccount(t, s, "a")
	ctx := context.Background()
	backup, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dst, _, _ := testStore(t)
	bad := backup
	bad.Accounts = append([]domain.BackupAccount{}, backup.Accounts...)
	bad.Accounts[0].Account.SiteID = "missing"
	if err = dst.Restore(ctx, bad); err == nil {
		t.Fatal("invalid foreign key accepted")
	}
	sites, _ := dst.Sites(ctx)
	if len(sites) != 2 {
		t.Fatal("partial restore committed")
	}
	if err = dst.Restore(ctx, backup); err != nil {
		t.Fatal(err)
	}
	got, _ := dst.Credentials(ctx, a.ID)
	if got.CookieHeader != c.CookieHeader {
		t.Fatal("credentials not re-encrypted")
	}
	settings, _ := dst.Settings(ctx)
	if settings.SchedulerEnabled {
		t.Fatal("restored instance ran without review")
	}
	if err = dst.Restore(ctx, backup); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("overwrote populated instance", err)
	}
}
