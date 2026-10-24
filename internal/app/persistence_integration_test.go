package app

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

func TestSQLiteLockRecoversResultWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "locked.db")
	vault, _ := security.NewVault(bytes.Repeat([]byte{5}, 32))
	repo, err := store.Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	settings, _ := repo.Settings(ctx)
	settings.SchedulerEnabled, settings.SiteGapSeconds = false, 0
	if _, err := repo.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	a, err := repo.SaveAccount(ctx, domain.Account{ID: "locked", SiteID: "anyrouter", Name: "locked", Enabled: true, Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enqueue(ctx, a.ID, "manual", time.Now()); err != nil {
		t.Fatal(err)
	}
	executor := &heldOriginExecutor{started: make(chan string, 2), release: make(chan struct{})}
	e := NewEngine(repo, executor)
	e.Start(ctx)
	defer func() { cancel(); e.Wait() }()
	select {
	case <-executor.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	lock, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	lock.SetMaxOpenConns(1)
	defer lock.Close()
	if _, err := lock.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer lock.Exec("ROLLBACK")
	close(executor.release)
	for e.Stats().PendingResults == 0 {
		if !wait(ctx, 20*time.Millisecond) {
			t.Fatal("result never entered recovery", ctx.Err())
		}
	}
	if _, err := lock.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	for e.Stats().PendingResults != 0 {
		if !wait(ctx, 20*time.Millisecond) {
			t.Fatal("result recovery did not finish", ctx.Err())
		}
	}
	page, err := repo.Runs(ctx, domain.RunFilter{AccountID: a.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != domain.Unverified || page.Items[0].Result.Evidence != "persistence_error" {
		t.Fatalf("unexpected recovered result: %+v %v", page, err)
	}
	a, err = repo.Account(ctx, a.ID)
	if err != nil || a.ActiveRunID != "" || a.NextRunAt != nil {
		t.Fatalf("account did not leave running state: %+v %v", a, err)
	}
	select {
	case <-executor.started:
		t.Fatal("recovery replayed the site operation")
	default:
	}
}
