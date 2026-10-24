// Package store implements the persistence ports with transactional SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/security"
	_ "modernc.org/sqlite"
)

type Store struct {
	db            *sql.DB
	vault         *security.Vault
	queueRevision atomic.Uint64
}

func (s *Store) QueueRevision() uint64 { return s.queueRevision.Load() }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scanner interface{ Scan(...any) error }

func Open(path string, vault *security.Vault) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	// These settings belong to each connection, including replacements created
	// after cancellation. A file URI also preserves spaces, #, % and ? in paths.
	dsn := url.URL{Scheme: "file", Path: uriPath, RawQuery: url.Values{
		"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"},
	}.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, vault: vault}
	if err = s.initialize(); err == nil {
		err = s.normalizeOrigins()
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func millis(t time.Time) int64 { return t.UTC().UnixMilli() }
func fromMillis(n int64) *time.Time {
	if n == 0 {
		return nil
	}
	t := time.UnixMilli(n).UTC()
	return &t
}
func ptrMillis(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return millis(*t)
}
func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil && strings.Contains(err.Error(), "constraint failed") {
		return domain.ErrConflict
	}
	return err
}

func (s *Store) Sites(ctx context.Context) ([]domain.Site, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT config FROM sites ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := []domain.Site{}
	for rows.Next() {
		var raw string
		var site domain.Site
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &site); err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	return sites, rows.Err()
}
func getSite(ctx context.Context, q queryer, id string) (domain.Site, error) {
	var raw string
	var site domain.Site
	if err := q.QueryRowContext(ctx, "SELECT config FROM sites WHERE id=?", id).Scan(&raw); err != nil {
		return site, mapError(err)
	}
	err := json.Unmarshal([]byte(raw), &site)
	return site, err
}
func (s *Store) Site(ctx context.Context, id string) (domain.Site, error) {
	return getSite(ctx, s.db, id)
}

func (s *Store) SaveSite(ctx context.Context, site domain.Site) (domain.Site, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return site, err
	}
	defer tx.Rollback()
	old, err := getSite(ctx, tx, site.ID)
	if errors.Is(err, domain.ErrNotFound) {
		if site.Version != 0 {
			return site, domain.ErrConflict
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites").Scan(&count); err != nil {
			return site, err
		}
		if err = domain.ValidateBackupCounts(count+1, 0, 0); err != nil {
			return site, err
		}
		site.Version = 1
		_, err = tx.ExecContext(ctx, "INSERT INTO sites(id,config) VALUES(?,?)", site.ID, jsonText(site))
	} else if err == nil {
		if old.Version != site.Version {
			return site, domain.ErrConflict
		}
		var active bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN accounts a ON a.id=r.account_id WHERE a.site_id=? AND r.status IN ('queued','running','waiting_user'))`, site.ID).Scan(&active); err != nil {
			return site, err
		}
		if active {
			return site, domain.ErrBusy
		}
		if old.BaseURL != site.BaseURL || old.Adapter != site.Adapter || old.TemplateID != site.TemplateID {
			var attached bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE site_id=?)", site.ID).Scan(&attached); err != nil {
				return site, err
			}
			if attached {
				return site, fmt.Errorf("%w：已有账号的站点不能更换地址、平台模板或签到方式", domain.ErrConflict)
			}
		}
		if old.Transport != site.Transport && site.Transport == "http" {
			var oauth bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE site_id=? AND json_extract(auth,'$.method') IN ('github','linuxdo'))`, site.ID).Scan(&oauth); err != nil {
				return site, err
			}
			if oauth {
				return site, fmt.Errorf("%w：此站点的 OAuth 账号需要浏览器执行方式", domain.ErrConflict)
			}
		}
		site.Version++
		_, err = tx.ExecContext(ctx, "UPDATE sites SET config=? WHERE id=?", jsonText(site), site.ID)
	}
	if err != nil {
		return site, mapError(err)
	}
	return site, tx.Commit()
}
func (s *Store) DeleteSite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sites WHERE id=?", id)
	if err != nil {
		return mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) Settings(ctx context.Context) (domain.Settings, error) { return getSettings(ctx, s.db) }
func getSettings(ctx context.Context, q queryer) (domain.Settings, error) {
	var settings domain.Settings
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT config FROM settings WHERE id=1").Scan(&raw); err != nil {
		return settings, err
	}
	err := json.Unmarshal([]byte(raw), &settings)
	return settings, err
}
func (s *Store) SaveSettings(ctx context.Context, v domain.Settings) (domain.Settings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	old, err := getSettings(ctx, tx)
	if err != nil {
		return v, err
	}
	if v.Version != old.Version {
		return v, domain.ErrConflict
	}
	v.Version++
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET config=? WHERE id=1", jsonText(v)); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
