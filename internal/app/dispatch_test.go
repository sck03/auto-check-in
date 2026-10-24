package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

type heldOriginExecutor struct {
	started chan string
	release chan struct{}
}

func (e *heldOriginExecutor) Execute(ctx context.Context, site domain.Site, _ domain.Credentials) (domain.Result, *domain.Credentials) {
	e.started <- site.ID
	if site.ID == "anyrouter" {
		select {
		case <-e.release:
		case <-ctx.Done():
		}
	}
	return domain.Result{Status: domain.Succeeded}, nil
}

func TestWaitingOriginDoesNotOccupyOtherWorkers(t *testing.T) {
	v, _ := security.NewVault(bytes.Repeat([]byte{3}, 32))
	repo, err := store.Open(filepath.Join(t.TempDir(), "test.db"), v)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx, cancel := context.WithCancel(context.Background())
	settings, _ := repo.Settings(ctx)
	settings.SchedulerEnabled, settings.Concurrency, settings.SiteGapSeconds = false, 2, 0
	if _, err := repo.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, id := range []string{"a1", "a2", "a3", "b"} {
		siteID := "anyrouter"
		if id == "b" {
			siteID = "agentrouter"
		}
		if _, err := repo.SaveAccount(ctx, domain.Account{ID: id, Name: id, SiteID: siteID, Enabled: true, Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, now); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Enqueue(ctx, id, "manual", now.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	executor := &heldOriginExecutor{started: make(chan string, 8), release: make(chan struct{})}
	e := NewEngine(repo, executor)
	e.Start(ctx)
	defer func() { cancel(); e.Wait() }()
	seen := map[string]bool{}
	for range 2 {
		select {
		case got := <-executor.started:
			seen[got] = true
		case <-time.After(3 * time.Second):
			t.Fatal("another origin was starved by waiting accounts")
		}
	}
	if !seen["anyrouter"] || !seen["agentrouter"] {
		t.Fatal("same-origin tasks occupied both workers")
	}
	close(executor.release)
}
