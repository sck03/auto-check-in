package store

import (
	"context"
	"errors"
	"testing"

	"auto-check-in/internal/domain"
)

func TestLegacySitesUpgradeWithoutChangingAccounts(t *testing.T) {
	s, path, vault := testStore(t)
	ctx := context.Background()
	account, credentials := addAccount(t, s, "existing")
	for _, template := range domain.Templates() {
		site := template.Site
		site.ID, site.TemplateID, site.Version = template.ID, "", 7
		switch template.ID {
		case "anyrouter":
			site.Name = "Any Router"
		case "agentrouter":
			site.Name = "我的 AgentRouter"
		case "wong":
			site.BaseURL, site.CheckInPath = "https://WZW.PP.UA:443/", "/custom/checkin"
		case "x666":
			site.Name = "薄荷 API 每日签到"
		}
		if _, err := s.db.Exec("INSERT OR REPLACE INTO sites(id,config) VALUES(?,?)", site.ID, jsonText(site)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	for _, template := range domain.Templates() {
		site, err := upgraded.Site(ctx, template.ID)
		if err != nil || site.TemplateID != template.ID || site.Version != 8 {
			t.Fatalf("template was not migrated: %+v, %v", site, err)
		}
		if template.ID == "agentrouter" {
			if site.Name != "我的 AgentRouter" {
				t.Fatal("custom name was replaced")
			}
		} else if site.Name != template.Site.Name {
			t.Fatalf("default name not normalized: %s", site.Name)
		}
		if template.ID == "wong" && (site.BaseURL != "https://WZW.PP.UA:443/" || site.CheckInPath != "/custom/checkin") {
			t.Fatal("custom endpoints changed during migration")
		}
	}
	got, err := upgraded.Account(ctx, account.ID)
	if err != nil || got.Version != account.Version || got.SiteID != account.SiteID {
		t.Fatalf("account changed during site migration: %+v, %v", got, err)
	}
	secret, err := upgraded.Credentials(ctx, account.ID)
	if err != nil || jsonText(secret) != jsonText(credentials) {
		t.Fatal("site migration changed account credentials", err)
	}
	if err = upgraded.migrate(); err != nil {
		t.Fatal(err)
	}
	any, _ := upgraded.Site(ctx, "anyrouter")
	if any.Version != 8 {
		t.Fatal("site migration ran twice")
	}
}

func TestSiteTemplateSurvivesAddressChangeAndLegacyUpdates(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	template, _ := domain.TemplateByID("wong")
	site := template.Site
	site.ID, site.BaseURL, site.Name = "wong-mirror", "https://mirror.test", "我的镜像"
	site, err := s.SaveSite(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	site.TemplateID = "" // Requests from a client predating template_id.
	site.BaseURL = "https://new-mirror.test"
	site, err = s.SaveSite(ctx, site)
	if err != nil || site.TemplateID != "wong" || site.Name != "我的镜像" {
		t.Fatalf("editing lost the selected template: %+v, %v", site, err)
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
		t.Fatalf("backup lost template metadata: %+v, %v", got, err)
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
