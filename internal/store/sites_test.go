package store

import (
	"context"
	"errors"
	"testing"

	"auto-check-in/internal/domain"
)

func TestSiteModesSurviveEditsAndBackup(t *testing.T) {
	for _, adapter := range []string{"newapi", "browser"} {
		t.Run(adapter, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			template, _ := domain.TemplateByID("newapi")
			site := template.Site
			site.ID, site.Name, site.BaseURL = "custom", "我的站点", "https://mirror.test"
			site.Adapter, site.Transport, site.CheckInPath = adapter, "browser", "/custom/checkin"
			site.Selectors.CheckIn, site.Selectors.Success = "#checkin", "#success"
			site, err := s.SaveSite(ctx, site)
			if err != nil {
				t.Fatal(err)
			}
			site.BaseURL = "https://new-mirror.test"
			site, err = s.SaveSite(ctx, site)
			if err != nil {
				t.Fatal(err)
			}
			account, err := s.SaveAccount(ctx, domain.Account{ID: "attached", SiteID: site.ID, Name: "账号", Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			changed := site
			changed.Adapter = "browser"
			if adapter == "browser" {
				changed.Adapter = "newapi"
			}
			if _, err := s.SaveSite(ctx, changed); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("referenced site changed check-in mode: %v", err)
			}
			backup, err := s.Export(ctx)
			if err != nil {
				t.Fatal(err)
			}
			restored, _, _ := testStore(t)
			if err = restored.Restore(ctx, backup); err != nil {
				t.Fatal(err)
			}
			got, err := restored.Site(ctx, site.ID)
			if err != nil || got != site {
				t.Fatalf("backup changed site: %+v, %v", got, err)
			}
			if _, err = restored.SaveSite(ctx, changed); !errors.Is(err, domain.ErrConflict) {
				t.Fatal(err)
			}
			if err = restored.DeleteAccount(ctx, account.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = restored.SaveSite(ctx, changed); err != nil {
				t.Fatal("unreferenced site cannot change mode", err)
			}
		})
	}
}

func TestSiteTransportCannotInvalidateOAuthAccounts(t *testing.T) {
	for _, method := range []string{"github", "linuxdo"} {
		t.Run(method, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			account, credentials := addAccount(t, s, "oauth")
			credentials.Method = method
			if _, err := s.SaveAccount(ctx, account, credentials, testNow); err != nil {
				t.Fatal(err)
			}
			site, _ := s.Site(ctx, account.SiteID)
			site.Transport = "http"
			if _, err := s.SaveSite(ctx, site); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("OAuth account lost its browser transport: %v", err)
			}
			unchanged, _ := s.Site(ctx, site.ID)
			if unchanged.Transport != "browser" || unchanged.Version != site.Version {
				t.Fatal("rejected edit was partially applied")
			}
			if err := s.DeleteAccount(ctx, account.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveSite(ctx, site); err != nil {
				t.Fatal("unreferenced site cannot change transport", err)
			}
			account.Version = 0
			if _, err := s.SaveAccount(ctx, account, credentials, testNow); err == nil {
				t.Fatal("account save accepted OAuth against the newer HTTP configuration")
			}
		})
	}
}
