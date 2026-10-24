package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"auto-check-in/internal/domain"
)

func TestAccountListKeepsOneRowAndMatchingActiveRun(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	for _, id := range []string{"idle", "done", "running", "queued", "login"} {
		addAccount(t, s, id)
	}
	done, _ := s.Enqueue(ctx, "done", "manual", testNow)
	finish(t, s, done.ID, domain.Result{Status: domain.Succeeded}, testNow)
	running, _ := s.Enqueue(ctx, "running", "manual", testNow)
	if run, err := s.Claim(ctx, domain.ClaimOptions{AllowScheduled: true, Now: testNow}); err != nil || run == nil || run.ID != running.ID {
		t.Fatal("could not start fixture task", err)
	}
	queued, _ := s.Enqueue(ctx, "queued", "manual", testNow)
	login, _ := s.BeginLogin(ctx, "login", testNow)
	want := map[string][2]string{
		"idle": {}, "done": {}, "running": {running.ID, domain.Running},
		"queued": {queued.ID, domain.Queued}, "login": {login.ID, domain.WaitingUser},
	}
	accounts, err := s.Accounts(ctx)
	if err != nil || len(accounts) != len(want) {
		t.Fatalf("history duplicated or hid accounts: %d, %v", len(accounts), err)
	}
	for _, account := range accounts {
		if [2]string{account.ActiveRunID, account.ActiveStatus} != want[account.ID] {
			t.Fatalf("wrong active task for %s: %+v", account.ID, account)
		}
		delete(want, account.ID)
	}
	if len(want) != 0 {
		t.Fatal("account missing from list", want)
	}
}

func BenchmarkAccountList(b *testing.B) {
	s, _, _ := testStore(b)
	ctx := context.Background()
	for i := 0; i < 300; i++ {
		addAccount(b, s, fmt.Sprint(i))
	}
	tx, err := s.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 300; i++ {
		for j := 0; j < 50; j++ {
			finished := testNow.Add(-time.Duration(j+1) * 24 * time.Hour)
			run := domain.Run{ID: fmt.Sprintf("%d-%d", i, j), AccountID: fmt.Sprint(i), Action: "checkin", Status: domain.Succeeded, CreatedAt: finished, StartedAt: &finished, FinishedAt: &finished}
			if err = insertRun(ctx, tx, run); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		accounts, err := s.Accounts(ctx)
		if err != nil || len(accounts) != 300 {
			b.Fatalf("list failed: %d, %v", len(accounts), err)
		}
	}
}
