package store

import (
	"auto-check-in/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) Export(ctx context.Context) (domain.Backup, error) {
	out := domain.Backup{Version: 1, CreatedAt: time.Now().UTC(), Sites: []domain.Site{}, Accounts: []domain.BackupAccount{}, Runs: []domain.Run{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT config FROM sites ORDER BY id")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw string
		var site domain.Site
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &site); err != nil {
			rows.Close()
			return out, err
		}
		out.Sites = append(out.Sites, site)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, accountSelect)
	if err != nil {
		return out, err
	}
	accounts := []domain.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		accounts = append(accounts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, a := range accounts {
		c, e := s.credentials(ctx, tx, a.ID)
		if e != nil {
			return out, e
		}
		if a.ActiveRunID != "" {
			a.NextRunAt = nil
			a.LastStatus = domain.Interrupted
			a.LastMessage = "备份时存在未结束的操作，请核对站点状态后手动执行"
		}
		a.ActiveRunID = ""
		a.ActiveStatus = ""
		out.Accounts = append(out.Accounts, domain.BackupAccount{Account: a, Credentials: c})
	}
	out.Settings, err = getSettings(ctx, tx)
	if err != nil {
		return out, err
	}
	// Export terminal records only. Active work belongs to the running instance.
	rows, err = tx.QueryContext(ctx, "SELECT "+runColumns+" FROM runs WHERE finished_at>0 ORDER BY created_at,id LIMIT 100001")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Runs = append(out.Runs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Runs) > 100000 {
		return out, fmt.Errorf("历史记录超过备份上限，请先清理")
	}
	return out, tx.Commit()
}

// Restore is deliberately limited to an empty instance. It never merges or
// silently overwrites an account with a different credential or schedule.
func (s *Store) Restore(ctx context.Context, b domain.Backup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w：恢复仅支持没有账号的新数据目录", domain.ErrConflict)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM runs; DELETE FROM sites"); err != nil {
		return err
	}
	for _, site := range b.Sites {
		if _, err = tx.ExecContext(ctx, "INSERT INTO sites(id,config) VALUES(?,?)", site.ID, jsonText(site)); err != nil {
			return mapError(err)
		}
	}
	for _, entry := range b.Accounts {
		a, c := entry.Account, entry.Credentials
		sealed, e := s.sealCredentials(c, a.ID)
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,site_id,name,enabled,schedule,auth,secret,identity,version,created_at,updated_at,next_run_at,last_run_at,last_success_at,last_status,last_message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.SiteID, a.Name, a.Enabled, jsonText(a.Schedule), jsonText(c.Summary()), sealed, identity(c), a.Version, millis(a.CreatedAt), millis(a.UpdatedAt), ptrMillis(a.NextRunAt), ptrMillis(a.LastRunAt), ptrMillis(a.LastSuccessAt), a.LastStatus, a.LastMessage)
		if err != nil {
			return mapError(err)
		}
	}
	for _, r := range b.Runs {
		var accountID any = r.AccountID
		if r.AccountID == "" {
			accountID = nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO runs(id,account_id,account_name,site_name,action,trigger,status,attempt,created_at,started_at,finished_at,result) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, accountID, r.AccountName, r.SiteName, r.Action, r.Trigger, r.Status, r.Attempt, millis(r.CreatedAt), ptrMillis(r.StartedAt), ptrMillis(r.FinishedAt), jsonText(r.Result))
		if err != nil {
			return mapError(err)
		}
	}
	b.Settings.Version = 1
	// Restored schedules remain paused until the owner reviews the new machine.
	b.Settings.SchedulerEnabled = false
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET config=? WHERE id=1", jsonText(b.Settings)); err != nil {
		return err
	}
	return tx.Commit()
}
