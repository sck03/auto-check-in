package store

import (
	"database/sql"
	"fmt"
	"time"

	"auto-check-in/internal/domain"
)

const schemaVersion = 6

const historyIndexes = `
CREATE INDEX IF NOT EXISTS run_status_history ON runs(status,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS run_action_history ON runs(action,created_at DESC,id DESC);`

// Only v5 has a supported upgrade path. DDL, cooldown recovery and the version
// change commit together; a failed upgrade leaves the old database intact.
func (s *Store) initialize() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 && version != 5 {
		return fmt.Errorf("数据库版本 %d 与当前版本不兼容，请使用新的空数据目录重新配置", version)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		err = createSchema(tx)
	} else {
		_, err = tx.Exec("ALTER TABLE accounts ADD COLUMN retry_not_before INTEGER NOT NULL DEFAULT 0")
		if err == nil {
			err = recoverCooldowns(tx, time.Now().UTC())
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(historyIndexes); err != nil {
		return err
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func createSchema(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE sites(id TEXT PRIMARY KEY, config TEXT NOT NULL);
CREATE TABLE accounts(
 id TEXT PRIMARY KEY, site_id TEXT NOT NULL REFERENCES sites(id), name TEXT NOT NULL,
 enabled INTEGER NOT NULL, schedule TEXT NOT NULL, auth TEXT NOT NULL, secret BLOB NOT NULL,
 identity TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, next_run_at INTEGER NOT NULL DEFAULT 0,
 last_run_at INTEGER NOT NULL DEFAULT 0, last_success_at INTEGER NOT NULL DEFAULT 0,
 last_status TEXT NOT NULL DEFAULT 'ready', last_message TEXT NOT NULL DEFAULT '',
 retry_not_before INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX account_identity ON accounts(site_id,identity) WHERE identity<>'';
CREATE INDEX due_accounts ON accounts(enabled,next_run_at);
CREATE INDEX site_accounts ON accounts(site_id);
CREATE TABLE runs(
 id TEXT PRIMARY KEY, account_id TEXT REFERENCES accounts(id) ON DELETE SET NULL,
 account_name TEXT NOT NULL, site_name TEXT NOT NULL, action TEXT NOT NULL,
 trigger TEXT NOT NULL, status TEXT NOT NULL, attempt INTEGER NOT NULL,
 created_at INTEGER NOT NULL, started_at INTEGER NOT NULL DEFAULT 0,
 finished_at INTEGER NOT NULL DEFAULT 0, result TEXT NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX one_active_run ON runs(account_id) WHERE status IN ('queued','running','waiting_user');
CREATE INDEX run_history ON runs(created_at DESC,id DESC);
CREATE INDEX account_history ON runs(account_id,created_at DESC);
CREATE INDEX queued_runs ON runs(created_at,id) WHERE status='queued';
CREATE INDEX daily_checkin_attempts ON runs(account_id,started_at) WHERE action='checkin';
CREATE TABLE settings(id INTEGER PRIMARY KEY CHECK(id=1), config TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	settings := domain.DefaultSettings()
	settings.Version = 1
	if _, err = tx.Exec("INSERT INTO settings VALUES(1,?)", jsonText(settings)); err != nil {
		return err
	}
	for _, t := range domain.Templates() {
		site := t.Site
		if site.ID == "" {
			continue
		}
		site.Version = 1
		if _, err = tx.Exec("INSERT INTO sites(id,config) VALUES(?,?)", site.ID, jsonText(site)); err != nil {
			return err
		}
	}
	return nil
}
