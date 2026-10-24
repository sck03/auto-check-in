package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-check-in/internal/domain"
)

const accountSelect = `SELECT a.id,a.site_id,a.name,a.enabled,a.schedule,a.auth,a.version,a.created_at,a.updated_at,a.next_run_at,a.last_run_at,a.last_success_at,a.last_status,a.last_message,
COALESCE(r.id,''),COALESCE(r.status,'')
FROM accounts a LEFT JOIN runs r ON r.account_id=a.id AND r.status IN ('queued','running','waiting_user')`

func scanAccount(row scanner) (domain.Account, error) {
	var a domain.Account
	var schedule, auth string
	var created, updated, next, last, success int64
	err := row.Scan(&a.ID, &a.SiteID, &a.Name, &a.Enabled, &schedule, &auth, &a.Version, &created, &updated, &next, &last, &success, &a.LastStatus, &a.LastMessage, &a.ActiveRunID, &a.ActiveStatus)
	if err != nil {
		return a, mapError(err)
	}
	if err = json.Unmarshal([]byte(schedule), &a.Schedule); err != nil {
		return a, err
	}
	if err = json.Unmarshal([]byte(auth), &a.Auth); err != nil {
		return a, err
	}
	a.CreatedAt = time.UnixMilli(created).UTC()
	a.UpdatedAt = time.UnixMilli(updated).UTC()
	a.NextRunAt = fromMillis(next)
	a.LastRunAt = fromMillis(last)
	a.LastSuccessAt = fromMillis(success)
	return a, nil
}
func getAccount(ctx context.Context, q queryer, id string) (domain.Account, error) {
	return scanAccount(q.QueryRowContext(ctx, accountSelect+" WHERE a.id=?", id))
}
func (s *Store) Account(ctx context.Context, id string) (domain.Account, error) {
	return getAccount(ctx, s.db, id)
}
func (s *Store) Accounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, accountSelect+" ORDER BY a.created_at,a.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
func (s *Store) Credentials(ctx context.Context, id string) (domain.Credentials, error) {
	return s.credentials(ctx, s.db, id)
}
func (s *Store) credentials(ctx context.Context, q queryer, id string) (domain.Credentials, error) {
	var c domain.Credentials
	var sealed []byte
	if err := q.QueryRowContext(ctx, "SELECT secret FROM accounts WHERE id=?", id).Scan(&sealed); err != nil {
		return c, mapError(err)
	}
	plain, err := s.vault.Open(sealed, "account:"+id)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(plain, &c)
	if err == nil && len(c.Storage) > 0 {
		storage := make(map[string]map[string]string, len(c.Storage))
		for origin, values := range c.Storage {
			key := domain.CanonicalOrigin(origin)
			// Prefer the already canonical entry if both spellings were stored.
			if storage[key] == nil || key == origin {
				storage[key] = values
			}
		}
		c.Storage = storage
	}
	return c, err
}
func identity(c domain.Credentials) string {
	if c.APIUser != "" {
		return "uid:" + c.APIUser
	}
	if c.Username != "" {
		return "username:" + strings.ToLower(c.Username)
	}
	return ""
}
func (s *Store) sealCredentials(c domain.Credentials, id string) ([]byte, error) {
	return s.vault.Seal([]byte(jsonText(c)), "account:"+id)
}
func (s *Store) updateCredentials(ctx context.Context, tx *sql.Tx, id string, c domain.Credentials, now time.Time) error {
	old, err := getAccount(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Auth.APIUser != "" && c.APIUser != old.Auth.APIUser {
		return fmt.Errorf("%w：运行或登录期间不能更换已绑定的用户身份", domain.ErrConflict)
	}
	sealed, err := s.sealCredentials(c, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET secret=?,auth=?,identity=?,version=version+1,updated_at=? WHERE id=?", sealed, jsonText(c.Summary()), identity(c), millis(now), id)
	return mapError(err)
}

func (s *Store) SaveAccount(ctx context.Context, a domain.Account, c domain.Credentials, now time.Time) (domain.Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	site, err := getSite(ctx, tx, a.SiteID)
	if err != nil {
		return a, err
	}
	// Recheck against the configuration held by this transaction. A site edit
	// may have changed transport since the service validated the request.
	if err = c.Validate(site); err != nil {
		return a, err
	}
	old, err := getAccount(ctx, tx, a.ID)
	sealed, sealErr := s.sealCredentials(c, a.ID)
	if sealErr != nil {
		return a, sealErr
	}
	next := a.Schedule.Next(now, a.ID, false)
	if errors.Is(err, domain.ErrNotFound) {
		if a.Version != 0 {
			return a, domain.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,site_id,name,enabled,schedule,auth,secret,identity,version,created_at,updated_at,next_run_at)
VALUES(?,?,?,?,?,?,?,?,1,?,?,?)`, a.ID, a.SiteID, a.Name, a.Enabled, jsonText(a.Schedule), jsonText(c.Summary()), sealed, identity(c), millis(now), millis(now), millis(next))
	} else if err == nil {
		if a.Version != old.Version || a.SiteID != old.SiteID {
			return a, domain.ErrConflict
		}
		if old.ActiveRunID != "" {
			return a, domain.ErrBusy
		}
		oldCredentials, e := s.credentials(ctx, tx, a.ID)
		if e != nil {
			return a, e
		}
		credentialsChanged := jsonText(oldCredentials) != jsonText(c)
		nextMillis := ptrMillis(old.NextRunAt)
		if old.Schedule != a.Schedule || credentialsChanged {
			attempts, e := dailyAttempts(ctx, tx, a.ID, now)
			if e != nil {
				return a, e
			}
			next = a.Schedule.Next(now, a.ID, attempts > 0)
			if nextMillis > 0 || (credentialsChanged && old.LastStatus == domain.NeedsAttention) {
				nextMillis = millis(next)
			}
		}
		status, message := old.LastStatus, old.LastMessage
		if credentialsChanged && status == domain.NeedsAttention {
			status = "ready"
			message = ""
		}
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET name=?,enabled=?,schedule=?,auth=?,secret=?,identity=?,version=version+1,updated_at=?,next_run_at=?,last_status=?,last_message=? WHERE id=?`,
			a.Name, a.Enabled, jsonText(a.Schedule), jsonText(c.Summary()), sealed, identity(c), millis(now), nextMillis, status, message, a.ID)
	}
	if err != nil {
		return a, mapError(err)
	}
	a, err = getAccount(ctx, tx, a.ID)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := getAccount(ctx, tx, id)
	if err != nil {
		return err
	}
	if a.ActiveRunID != "" {
		return domain.ErrBusy
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM accounts WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

const scheduledAccounts = ` FROM accounts a WHERE enabled=1 AND next_run_at>0
AND NOT EXISTS(SELECT 1 FROM runs r WHERE r.account_id=a.id AND r.status IN ('queued','running','waiting_user'))`

func (s *Store) NextScheduled(ctx context.Context) (*time.Time, error) {
	var next int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MIN(next_run_at),0)"+scheduledAccounts).Scan(&next)
	return fromMillis(next), err
}

func (s *Store) DueAccounts(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id"+scheduledAccounts+" AND next_run_at<=? ORDER BY next_run_at LIMIT 100", millis(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) checkAccountSecret(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id,secret FROM accounts")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var secret []byte
		if err = rows.Scan(&id, &secret); err != nil {
			return err
		}
		if _, err = s.vault.Open(secret, "account:"+id); err != nil {
			return fmt.Errorf("账号凭据无法解密：%w", err)
		}
	}
	return rows.Err()
}
