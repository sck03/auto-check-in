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

func (*idleLoginSession) NativeWindow() bool                  { return false }
func (*idleLoginSession) ConfirmedLogin(context.Context) bool { return false }
func (s *idleLoginSession) Done() <-chan struct{}             { return s.ctx.Done() }
func (*idleLoginSession) Close()                              {}

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

type confirmedLoginSession struct {
	idleLoginSession
	confirmed bool
	requests  int
}

func (s *confirmedLoginSession) ConfirmedLogin(context.Context) bool { return s.confirmed }
func (s *confirmedLoginSession) Request(context.Context, string, string, any) (port.Response, error) {
	s.requests++
	return port.Response{Status: 200, Body: []byte(`<!doctype html><meta name="aliyun_waf_aa" content="fixture">`)}, nil
}
func (*confirmedLoginSession) Snapshot(context.Context) (domain.Credentials, error) {
	return domain.Credentials{Method: "linuxdo", APIUser: "7", Cookies: []domain.Cookie{{Name: "session", Value: "fresh", Domain: "agentrouter.org", Path: "/", HTTPOnly: true, Secure: true}}, Storage: map[string]map[string]string{"https://agentrouter.org": {"user": `{"id":7}`}}}, nil
}

func TestSaveAgentRouterObservedLoginWithoutRedundantProfileRequest(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed", true: "confirmed"}[confirmed], func(t *testing.T) {
			var session *confirmedLoginSession
			browser := &loginBrowser{open: func(ctx context.Context, site domain.Site, c domain.Credentials) (port.InteractiveSession, error) {
				if c.APIUser != "" || len(c.Storage[site.BaseURL]) != 0 || len(c.Cookies) != 1 || c.Cookies[0].Domain != "linux.do" {
					t.Fatal("interactive AgentRouter login must clear its old session and preserve the identity provider")
				}
				session = &confirmedLoginSession{idleLoginSession: idleLoginSession{ctx: ctx}, confirmed: confirmed}
				return session, nil
			}}
			service, _ := loginService(t, browser)
			ctx := context.Background()
			credentials := domain.Credentials{Method: "linuxdo", APIUser: "7", Cookies: []domain.Cookie{
				{Name: "session", Value: "old", Domain: "agentrouter.org", Path: "/"},
				{Name: "provider", Value: "saved", Domain: "linux.do", Path: "/"},
			}, Storage: map[string]map[string]string{"https://agentrouter.org": {"user": `{"id":7}`}}}
			account, err := service.Repo.SaveAccount(ctx, domain.Account{Name: "agent", SiteID: "agentrouter", Schedule: domain.DefaultSchedule()}, credentials, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			info, err := service.StartLogin(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			err = service.SaveLogin(ctx, info.ID)
			if (err == nil) != confirmed {
				t.Fatalf("confirmed=%v save error=%v", confirmed, err)
			}
			if confirmed {
				if session.requests != 0 {
					t.Fatal("confirmed fresh login made an unnecessary user-info request")
				}
				saved, err := service.Repo.Credentials(ctx, account.ID)
				if err != nil || saved.APIUser != "7" || len(saved.Cookies) != 1 {
					t.Fatal("confirmed session was not persisted")
				}
			} else if session.requests != 1 {
				t.Fatal("unconfirmed login skipped verification")
			}
		})
	}
}

func TestInteractiveLoginPreservesOtherSiteSessions(t *testing.T) {
	for _, siteID := range []string{"anyrouter", "newapi"} {
		t.Run(siteID, func(t *testing.T) {
			browser := &loginBrowser{open: func(ctx context.Context, site domain.Site, c domain.Credentials) (port.InteractiveSession, error) {
				if c.CookieHeader != "session=saved" || c.APIUser != "7" || c.Storage[site.BaseURL]["user"] != `{"id":7}` {
					t.Fatal("opening an ordinary site's login window discarded its saved session")
				}
				return &idleLoginSession{ctx: ctx}, nil
			}}
			service, _ := loginService(t, browser)
			ctx := context.Background()
			site, err := service.Repo.Site(ctx, siteID)
			if errors.Is(err, domain.ErrNotFound) {
				template, _ := domain.TemplateByID(siteID)
				site = template.Site
				site.ID, site.BaseURL, site.TemplateID, site.Transport = siteID, "https://fixture.example", siteID, "browser"
				site, err = service.Repo.SaveSite(ctx, site)
			}
			if err != nil {
				t.Fatal(err)
			}
			creds := domain.Credentials{Method: "linuxdo", CookieHeader: "session=saved", APIUser: "7", Storage: map[string]map[string]string{site.BaseURL: {"user": `{"id":7}`}}}
			account, err := service.Repo.SaveAccount(ctx, domain.Account{Name: "reuse", SiteID: siteID, Schedule: domain.DefaultSchedule()}, creds, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.StartLogin(ctx, account.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoginStartupFollowsRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	browser := &loginBrowser{open: func(ctx context.Context, _ domain.Site, _ domain.Credentials) (port.InteractiveSession, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service, id := loginService(t, browser)
	wake := service.Engine.subscription()
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
	select {
	case <-wake:
	default:
		t.Fatal("cancelled login did not wake the scheduler after releasing the account")
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
