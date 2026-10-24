package app

import (
	"bytes"
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

type limitedExecutor struct {
	free    atomic.Bool
	changed chan struct{}
	started chan string
}

func (e *limitedExecutor) Capacity() port.Capacity {
	if !e.free.Load() {
		return port.Capacity{BlockedTransports: []string{"browser"}, Changed: e.changed}
	}
	return port.Capacity{}
}
func (e *limitedExecutor) Execute(_ context.Context, site domain.Site, _ domain.Credentials) (domain.Result, *domain.Credentials) {
	e.started <- site.ID
	return domain.Result{Status: domain.Succeeded}, nil
}

func TestFullBrowserCapacityKeepsQueueAndAllowsHTTP(t *testing.T) {
	vault, _ := security.NewVault(bytes.Repeat([]byte{8}, 32))
	repo, err := store.Open(filepath.Join(t.TempDir(), "capacity.db"), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	settings, _ := repo.Settings(ctx)
	settings.Concurrency, settings.SiteGapSeconds, settings.SchedulerEnabled = 1, 0, false
	if _, err = repo.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	site := domain.Templates()[2].Site
	site.ID, site.Version, site.BaseURL = "http", 0, "https://fixture.invalid"
	if _, err = repo.SaveSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"anyrouter", "http"} {
		a := domain.Account{ID: id, SiteID: id, Name: id, Enabled: true, Schedule: domain.DefaultSchedule()}
		if _, err = repo.SaveAccount(ctx, a, domain.Credentials{Method: "cookie"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Enqueue(ctx, id, "manual", time.Now().Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	executor := &limitedExecutor{changed: make(chan struct{}), started: make(chan string, 4)}
	engine := NewEngine(repo, executor)
	engine.Start(ctx)
	defer func() { cancel(); engine.Wait() }()
	select {
	case id := <-executor.started:
		if id != "http" {
			t.Fatal("browser task consumed the HTTP worker")
		}
	case <-ctx.Done():
		t.Fatal("HTTP task starved")
	}
	a, err := repo.Account(ctx, "anyrouter")
	if err != nil || a.ActiveStatus != domain.Queued {
		t.Fatal("blocked task did not remain queued", err)
	}
	executor.free.Store(true)
	close(executor.changed)
	select {
	case id := <-executor.started:
		if id != "anyrouter" {
			t.Fatal(id)
		}
	case <-ctx.Done():
		t.Fatal("capacity release did not wake task")
	}
}
