package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSchemaVersionRequiresFreshDirectory(t *testing.T) {
	for _, version := range []int{1, 2, 3, 4, schemaVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			s, path, vault := testStore(t)
			if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if reopened, err := Open(path, vault); err == nil {
				reopened.Close()
				t.Fatal("incompatible database was accepted")
			} else if !strings.Contains(err.Error(), "空数据目录") {
				t.Fatal("missing reinstall guidance", err)
			}
		})
	}
}

func TestNewDatabaseIndexesAndReopen(t *testing.T) {
	s, path, vault := testStore(t)
	addAccount(t, s, "existing")
	var indexes int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='index' AND name IN ('queued_runs','daily_checkin_attempts','site_accounts')").Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("missing query indexes: %d, %v", indexes, err)
	}
	var id, parent, unused int
	var detail string
	if err := s.db.QueryRow("EXPLAIN QUERY PLAN SELECT id FROM accounts WHERE site_id=?", "anyrouter").Scan(&id, &parent, &unused, &detail); err != nil || !strings.Contains(detail, "site_accounts") {
		t.Fatalf("site lookup scans the account table: %s, %v", detail, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Credentials(context.Background(), "existing"); err != nil {
		t.Fatal("reopening lost credentials", err)
	}
}
