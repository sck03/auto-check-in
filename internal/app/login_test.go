package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
)

type loginBrowser struct {
	port.BrowserFactory
	open func(context.Context, domain.Site, domain.Credentials) (port.InteractiveSession, error)
}

func (*loginBrowser) Available() bool { return true }
func (b *loginBrowser) OpenInteractive(ctx context.Context, site domain.Site, c domain.Credentials) (port.InteractiveSession, error) {
	return b.open(ctx, site, c)
}

type idleLoginSession struct {
	port.InteractiveSession
	ctx context.Context
}

func (*idleLoginSession) NativeWindow() bool      { return false }
func (s *idleLoginSession) Done() <-chan struct{} { return s.ctx.Done() }
func (*idleLoginSession) Close()                  {}

func loginService(t *testing.T, browser port.BrowserFactory) (*Service, string) {
	t.Helper()
	vault, err := security.NewVault(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := store.Open(filepath.Join(t.TempDir(), "test.db"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	a, err := repo.SaveAccount(context.Background(), domain.Account{ID: "login-account", SiteID: "anyrouter", Name: "login", Enabled: true, Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	service := New(context.Background(), repo, nil, browser, true)
	t.Cleanup(service.CloseLogins)
	return service, a.ID
}

func TestLoginStartupFollowsRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	browser := &loginBrowser{open: func(ctx context.Context, _ domain.Site, _ domain.Credentials) (port.InteractiveSession, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service, id := loginService(t, browser)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := service.StartLogin(ctx, id); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("login did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected request cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled request left browser startup running")
	}
	account, err := service.Repo.Account(context.Background(), id)
	if err != nil || account.ActiveRunID != "" {
		t.Fatalf("cancelled startup left account locked: %+v, %v", account, err)
	}
}

func TestLoginOutlivesCompletedStartRequest(t *testing.T) {
	var session *idleLoginSession
	browser := &loginBrowser{open: func(ctx context.Context, _ domain.Site, _ domain.Credentials) (port.InteractiveSession, error) {
		session = &idleLoginSession{ctx: ctx}
		return session, nil
	}}
	service, id := loginService(t, browser)
	ctx, cancel := context.WithCancel(context.Background())
	info, err := service.StartLogin(ctx, id)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Done():
		t.Fatal("completed HTTP request cancelled the interactive session")
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := service.LoginInfo(info.ID); err != nil {
		t.Fatal(err)
	}
}
