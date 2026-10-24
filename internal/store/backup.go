package store

import (
	"auto-check-in/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) Export(ctx context.Context) (domain.Backup, error) {
	out := domain.Backup{Version: domain.BackupVersion, CreatedAt: time.Now().UTC(), Sites: []domain.Site{}, Accounts: []domain.BackupAccount{}, Runs: []domain.Run{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var sites, accounts, runs int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sites),
(SELECT COUNT(*) FROM accounts),(SELECT COUNT(*) FROM runs WHERE finished_at>0)`).Scan(&sites, &accounts, &runs); err != nil {
		return out, err
	}
	if err = domain.ValidateBackupCounts(sites, accounts, runs); err != nil {
		return out, err
	}
	out.Settings, err = getSettings(ctx, tx)
	if err != nil {
		return out, err
	}
	budget := backupBudget(domain.MaxBackupBytes)
	if err = budget.add(out); err != nil {
		return out, err
	}
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
		if err = budget.add(site); err != nil {
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
	// Read each encrypted secret with its public fields in one snapshot query.
	// No N+1 credential queries or second full account slice is needed.
	rows, err = tx.QueryContext(ctx, "SELECT "+accountColumns+",a.secret"+accountFrom+" ORDER BY a.id")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var sealed []byte
		a, e := scanAccount(rows, &sealed)
		if e != nil {
			rows.Close()
			return out, e
		}
		c, e := s.decodeCredentials(sealed, a.ID)
		if e != nil {
			rows.Close()
			return out, e
		}
		if e = c.ValidateSession(); e != nil {
			rows.Close()
			return out, fmt.Errorf("账号 %s 的会话无法恢复：%w", a.Name, e)
		}
		if a.ActiveRunID != "" {
			a.NextRunAt = nil
			a.LastStatus = domain.Interrupted
			a.LastMessage = "备份时存在未结束的操作，请核对站点状态后手动执行"
		}
		a.ActiveRunID = ""
		a.ActiveStatus = ""
		entry := domain.BackupAccount{Account: a, Credentials: c}
		if err = budget.add(entry); err != nil {
			rows.Close()
			return out, err
		}
		out.Accounts = append(out.Accounts, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Export terminal records only. Active work belongs to the running instance.
	rows, err = tx.QueryContext(ctx, "SELECT "+runColumns+" FROM runs WHERE finished_at>0 ORDER BY created_at,id")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		if err = budget.add(r); err != nil {
			rows.Close()
			return out, err
		}
		out.Runs = append(out.Runs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// Count serialized entries while collecting them, before retaining arbitrarily
// many decrypted sessions. One extra comma per entry is a conservative bound.
type backupBudget int

func (b *backupBudget) add(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	*b -= backupBudget(len(encoded) + 1)
	if *b < 0 {
		return fmt.Errorf("备份超出 32 MiB 限制，请先整理会话或历史记录")
	}
	return nil
}

// Restore is deliberately limited to an empty instance. It never merges or
// silently overwrites an account with a different credential or schedule.
func (s *Store) Restore(ctx context.Context, b domain.Backup) error {
	if err := domain.ValidateBackupCounts(len(b.Sites), len(b.Accounts), len(b.Runs)); err != nil {
		return err
	}
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
		if a.NextRunAt != nil {
			next := a.RespectCooldown(*a.NextRunAt)
			a.NextRunAt = &next
		}
		sealed, e := s.sealCredentials(c, a.ID)
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,site_id,name,enabled,schedule,auth,secret,identity,version,created_at,updated_at,next_run_at,last_run_at,last_success_at,last_status,last_message,retry_not_before) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.SiteID, a.Name, a.Enabled, jsonText(a.Schedule), jsonText(c.Summary()), sealed, identity(c), a.Version, millis(a.CreatedAt), millis(a.UpdatedAt), ptrMillis(a.NextRunAt), ptrMillis(a.LastRunAt), ptrMillis(a.LastSuccessAt), a.LastStatus, a.LastMessage, ptrMillis(a.RetryNotBefore))
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
	// Older v1 archives have no account cooldown field. Recover any live limits
	// from their terminal records without weakening a newer explicit deadline.
	if err = recoverCooldowns(tx, time.Now().UTC()); err != nil {
		return err
	}
	b.Settings.Version = 1
	// Restored schedules remain paused until the owner reviews the new machine.
	b.Settings.SchedulerEnabled = false
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET config=? WHERE id=1", jsonText(b.Settings)); err != nil {
		return err
	}
	return tx.Commit()
}
