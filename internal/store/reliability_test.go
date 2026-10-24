package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"auto-check-in/internal/domain"
)

func TestResultWriteIsIdempotentAndRejectsIdentityChange(t *testing.T) {
	s, _, _ := testStore(t)
	a, c := addAccount(t, s, "bound-user")
	ctx := context.Background()
	r, err := s.Enqueue(ctx, a.ID, "manual", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, domain.ClaimOptions{AllowScheduled: true, Now: testNow}); err != nil {
		t.Fatal(err)
	}
	c.APIUser = "wrong-user"
	result := domain.Result{Status: domain.Succeeded}
	if err := s.Finish(ctx, r.ID, result, &c, domain.DefaultSettings(), testNow); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("accepted a changed identity", err)
	}
	if err := s.Finish(ctx, r.ID, result, nil, domain.DefaultSettings(), testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, r.ID, domain.Result{Status: domain.Unverified}, nil, domain.DefaultSettings(), testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	saved, err := getRun(ctx, s.db, r.ID)
	if err != nil || saved.Status != domain.Succeeded || !saved.FinishedAt.Equal(testNow) {
		t.Fatalf("retry overwrote committed result: %+v %v", saved, err)
	}
	stored, err := s.Credentials(ctx, a.ID)
	if err != nil || stored.APIUser != "bound-user" {
		t.Fatal("identity changed", err)
	}
}

func TestEquivalentOriginsReopenWithExistingStorage(t *testing.T) {
	s, path, vault := testStore(t)
	ctx := context.Background()
	site, err := s.Site(ctx, "anyrouter")
	if err != nil {
		t.Fatal(err)
	}
	site.BaseURL = "https://anyrouter.top:443"
	if _, err := s.db.Exec("UPDATE sites SET config=? WHERE id=?", jsonText(site), site.ID); err != nil {
		t.Fatal(err)
	}
	c := domain.Credentials{Method: "cookie", APIUser: "one", Storage: map[string]map[string]string{site.BaseURL: {"user": `{"id":"one"}`}}}
	if _, err := s.SaveAccount(ctx, domain.Account{ID: "one", SiteID: site.ID, Name: "one", Enabled: true, Schedule: domain.DefaultSchedule()}, c, testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	site, err = reopened.Site(ctx, site.ID)
	if err != nil || site.BaseURL != "https://anyrouter.top" {
		t.Fatal(site.BaseURL, err)
	}
	c, err = reopened.Credentials(ctx, "one")
	if err != nil || c.Storage[site.BaseURL]["user"] != `{"id":"one"}` {
		t.Fatal("storage origin was not normalized", err)
	}
}

func TestClaimSkipsAllSitesSharingAReservedOrigin(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	for i, id := range []string{"a-one", "a-two", "b"} {
		siteID := "anyrouter"
		if id == "a-two" {
			site, _ := s.Site(ctx, siteID)
			site.ID, site.Version = "same-origin", 0
			if _, err := s.SaveSite(ctx, site); err != nil {
				t.Fatal(err)
			}
			siteID = site.ID
		} else if id == "b" {
			siteID = "agentrouter"
		}
		if _, err := s.SaveAccount(ctx, domain.Account{ID: id, SiteID: siteID, Name: id, Enabled: true, Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, testNow); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Enqueue(ctx, id, "manual", testNow.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	run, err := s.Claim(ctx, domain.ClaimOptions{AllowScheduled: true, Now: testNow, ExcludedOrigins: []string{"https://anyrouter.top"}})
	if err != nil || run == nil || run.AccountID != "b" {
		t.Fatalf("unrelated origin was blocked: %+v %v", run, err)
	}
	page, err := s.Runs(ctx, domain.RunFilter{Status: domain.Queued})
	if err != nil || page.Total != 2 {
		t.Fatalf("waiting work was consumed: %+v %v", page, err)
	}
}
