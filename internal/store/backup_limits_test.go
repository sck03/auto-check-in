package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"auto-check-in/internal/domain"
)

func TestSiteCapacityMatchesBackupContract(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	site, _ := s.Site(ctx, "anyrouter")
	// Seed the public limit without making thousands of individual transactions.
	_, err := s.db.Exec(`WITH RECURSIVE entries(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM entries WHERE n<?)
INSERT INTO sites(id,config) SELECT 'limit-'||n,json_set(?,'$.id','limit-'||n) FROM entries`, domain.MaxSites-2, jsonText(site))
	if err != nil {
		t.Fatal(err)
	}
	backup, err := s.Export(ctx)
	if err != nil || len(backup.Sites) != domain.MaxSites {
		t.Fatal("limit is not exportable", err)
	}
	dest, _, _ := testStore(t)
	if err = dest.Restore(ctx, backup); err != nil {
		t.Fatal("exported limit could not be restored", err)
	}
	site.ID, site.Version = "one-too-many", 0
	if _, err = s.SaveSite(ctx, site); err == nil {
		t.Fatal("creation bypassed backup limit")
	}
	// Older databases may already exceed the new creation limit. Fail before
	// issuing an apparently successful backup download.
	if _, err = s.db.Exec("INSERT INTO sites(id,config) VALUES(?,?)", site.ID, jsonText(site)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Export(ctx); err == nil || !strings.Contains(err.Error(), "站点数量") {
		t.Fatal("unrestorable export accepted", err)
	}
}

func TestAccountCapacityIsCheckedBeforeDecryptingExport(t *testing.T) {
	s, _, _ := testStore(t)
	_, err := s.db.Exec(`WITH RECURSIVE entries(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM entries WHERE n<?)
INSERT INTO accounts(id,site_id,name,enabled,schedule,auth,secret,version,created_at,updated_at)
SELECT 'limit-'||n,'anyrouter','limit',1,?,?,'legacy-invalid-secret',1,1,1 FROM entries`, domain.MaxAccounts, jsonText(domain.DefaultSchedule()), jsonText(domain.Credentials{Method: "cookie"}.Summary()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveAccount(context.Background(), domain.Account{ID: "one-too-many", SiteID: "anyrouter", Name: "limit", Enabled: true, Schedule: domain.DefaultSchedule()}, domain.Credentials{Method: "cookie"}, testNow)
	if err == nil || !strings.Contains(err.Error(), "账号数量") {
		t.Fatal("creation bypassed account capacity", err)
	}
	if _, err = s.db.Exec(`INSERT INTO accounts(id,site_id,name,enabled,schedule,auth,secret,version,created_at,updated_at)
VALUES('extra','anyrouter','extra',1,?,?,'invalid',1,1,1)`, jsonText(domain.DefaultSchedule()), jsonText(domain.AuthSummary{})); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Export(context.Background()); err == nil || !strings.Contains(err.Error(), "账号数量") {
		t.Fatal("export decrypted records before checking capacity", err)
	}
}

func TestBackupRejectsOversizedLegacySession(t *testing.T) {
	s, _, _ := testStore(t)
	a, c := addAccount(t, s, "legacy")
	c.Storage = map[string]map[string]string{"https://anyrouter.top": {"oversized": strings.Repeat("x", domain.MaxSessionBytes+1)}}
	sealed, err := s.sealCredentials(c, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE accounts SET secret=? WHERE id=?", sealed, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Export(context.Background()); !errors.Is(err, domain.ErrSessionLimit) {
		t.Fatal("exported session that restore rejects", err)
	}
	if _, err = s.Credentials(context.Background(), a.ID); err != nil {
		t.Fatal("legacy session became unreadable", err)
	}
}

func TestBackupCollectionStopsAtSerializedBudget(t *testing.T) {
	budget := backupBudget(32)
	if err := budget.add(map[string]string{"key": "value"}); err != nil {
		t.Fatal(err)
	}
	if err := budget.add(strings.Repeat("\n", 20)); err == nil {
		t.Fatal("JSON escaping bypassed backup budget")
	}
}

func BenchmarkBackupExport(b *testing.B) {
	s, _, _ := testStore(b)
	for i := 0; i < 300; i++ {
		addAccount(b, s, fmt.Sprintf("account-%d", i))
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Export(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
