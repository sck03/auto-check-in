package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"auto-check-in/internal/domain"
)

const runColumns = "id,COALESCE(account_id,''),account_name,site_name,action,trigger,status,attempt,created_at,started_at,finished_at,result"

func scanRun(row scanner) (domain.Run, error) {
	var r domain.Run
	var created, started, finished int64
	var result string
	err := row.Scan(&r.ID, &r.AccountID, &r.AccountName, &r.SiteName, &r.Action, &r.Trigger, &r.Status, &r.Attempt, &created, &started, &finished, &result)
	if err != nil {
		return r, mapError(err)
	}
	r.CreatedAt = time.UnixMilli(created).UTC()
	r.StartedAt = fromMillis(started)
	r.FinishedAt = fromMillis(finished)
	err = json.Unmarshal([]byte(result), &r.Result)
	return r, err
}
func getRun(ctx context.Context, q queryer, id string) (domain.Run, error) {
	return scanRun(q.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id=?", id))
}
func insertRun(ctx context.Context, tx *sql.Tx, r domain.Run) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runs(id,account_id,account_name,site_name,action,trigger,status,attempt,created_at,started_at,finished_at,result) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.AccountID, r.AccountName, r.SiteName, r.Action, r.Trigger, r.Status, r.Attempt, millis(r.CreatedAt), ptrMillis(r.StartedAt), ptrMillis(r.FinishedAt), jsonText(r.Result))
	return mapError(err)
}
func dailyAttempts(ctx context.Context, q queryer, id string, now time.Time) (int, error) {
	start, end := domain.DayBounds(now)
	var count int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE account_id=? AND action='checkin' AND started_at>=? AND started_at<?", id, millis(start), millis(end)).Scan(&count)
	return count, err
}
func (s *Store) Enqueue(ctx context.Context, id, trigger string, now time.Time) (domain.Run, error) {
	var r domain.Run
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	a, err := getAccount(ctx, tx, id)
	if err != nil {
		return r, err
	}
	if !a.Enabled {
		return r, domain.ErrDisabled
	}
	if a.ActiveRunID != "" {
		return r, domain.ErrBusy
	}
	if a.Schedule.InCompletedCycle(now, a.LastSuccessAt) {
		return r, domain.ErrCooldown
	}
	attempts, err := dailyAttempts(ctx, tx, id, now)
	if err != nil {
		return r, err
	}
	if attempts > 0 && trigger == "manual" {
		return r, domain.ErrDailyLimit
	}
	if trigger == "scheduled" && (a.NextRunAt == nil || a.NextRunAt.After(now)) {
		return r, domain.ErrConflict
	}
	if trigger == "scheduled" {
		settings, e := getSettings(ctx, tx)
		if e != nil {
			return r, e
		}
		if attempts > 0 {
			if a.LastStatus != domain.Failed || attempts > settings.MaxRetries {
				return r, domain.ErrDailyLimit
			}
			trigger = "scheduled_retry"
		}
		todayAt := a.Schedule.ScheduledTime(now, a.ID)
		allowed := a.Schedule.WithinWindow(now) && !todayAt.After(now)
		if trigger == "scheduled_retry" {
			allowed = a.Schedule.WithinRetryWindow(now)
		}
		if !allowed {
			next := a.Schedule.Next(now, a.ID, attempts > 0)
			if _, err = tx.ExecContext(ctx, "UPDATE accounts SET next_run_at=? WHERE id=?", millis(next), a.ID); err != nil {
				return r, err
			}
			if err = tx.Commit(); err != nil {
				return r, err
			}
			return r, domain.ErrConflict
		}
	}
	site, err := getSite(ctx, tx, a.SiteID)
	if err != nil {
		return r, err
	}
	r = domain.Run{ID: domain.NewID(), AccountID: id, AccountName: a.Name, SiteName: site.Name, Action: "checkin", Trigger: trigger, Status: domain.Queued, Attempt: attempts + 1, CreatedAt: now.UTC()}
	if err = insertRun(ctx, tx, r); err != nil {
		return r, err
	}
	return r, tx.Commit()
}
func (s *Store) Claim(ctx context.Context, allowScheduled bool, now time.Time) (*domain.Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT r.id FROM runs r JOIN accounts a ON a.id=r.account_id WHERE r.status='queued' AND a.enabled=1 AND (? OR r.trigger IN ('manual','manual_retry')) ORDER BY r.created_at,r.id LIMIT 1`, allowScheduled).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pending, err := getRun(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if pending.Trigger == "scheduled" || pending.Trigger == "scheduled_retry" {
		a, e := getAccount(ctx, tx, pending.AccountID)
		if e != nil {
			return nil, e
		}
		attempts, e := dailyAttempts(ctx, tx, a.ID, now)
		if e != nil {
			return nil, e
		}
		allowed := attempts == 0 && a.Schedule.WithinWindow(now) && !a.Schedule.ScheduledTime(now, a.ID).After(now)
		if pending.Trigger == "scheduled_retry" {
			settings, e := getSettings(ctx, tx)
			if e != nil {
				return nil, e
			}
			allowed = attempts > 0 && attempts <= settings.MaxRetries && a.LastStatus == domain.Failed && a.Schedule.WithinRetryWindow(now)
		}
		if !allowed || a.Schedule.InCompletedCycle(now, a.LastSuccessAt) {
			result := domain.Result{Status: domain.Cancelled, Message: "排队计划已过期，按每日窗口重新安排", Evidence: "schedule_deferred"}
			if _, err = tx.ExecContext(ctx, "UPDATE runs SET status='cancelled',finished_at=?,result=? WHERE id=?", millis(now), jsonText(result), id); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE accounts SET next_run_at=? WHERE id=?", millis(a.Schedule.Next(now, a.ID, attempts > 0)), a.ID); err != nil {
				return nil, err
			}
			return nil, tx.Commit()
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status='running',started_at=? WHERE id=? AND status='queued'", millis(now), id); err != nil {
		return nil, err
	}
	r, err := getRun(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) Finish(ctx context.Context, id string, result domain.Result, c *domain.Credentials, settings domain.Settings, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := getRun(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.Status != domain.Running {
		return domain.ErrConflict
	}
	a, err := getAccount(ctx, tx, r.AccountID)
	if err != nil {
		return err
	}
	next := a.Schedule.Next(now, a.ID, true)
	nextMillis := millis(next)
	lastSuccess := ptrMillis(a.LastSuccessAt)
	if result.Complete() {
		lastSuccess = millis(now)
	}
	if result.BlocksSchedule() {
		nextMillis = 0
	}
	if result.Status == domain.Failed && result.Retryable && r.Attempt <= settings.MaxRetries {
		delay := settings.RetryBaseSeconds
		for attempt := 1; attempt < r.Attempt; attempt++ {
			delay *= 3
		}
		if result.RetryAfterSeconds > delay {
			delay = result.RetryAfterSeconds
		}
		if delay > 86400 {
			delay = 86400
		}
		hash := sha256.Sum256([]byte(r.ID))
		start, end := a.Schedule.RetryBounds(now)
		retryAt := now.Add(time.Duration(delay+int(hash[0])%31) * time.Second)
		if retryAt.Before(start) {
			retryAt = start.Add(time.Duration(int(hash[0])%31) * time.Second)
		}
		if retryAt.Before(end) {
			nextMillis = millis(retryAt)
		}
	}
	if c != nil {
		if err = s.updateCredentials(ctx, tx, a.ID, *c, now); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET next_run_at=?,last_run_at=?,last_success_at=?,last_status=?,last_message=? WHERE id=?`,
		nextMillis, millis(now), lastSuccess, result.Status, result.Message, a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status=?,finished_at=?,result=? WHERE id=?", result.Status, millis(now), jsonText(result), id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) CancelQueued(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := getRun(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.Status != domain.Queued {
		return domain.ErrConflict
	}
	a, err := getAccount(ctx, tx, r.AccountID)
	if err != nil {
		return err
	}
	result := domain.Result{Status: domain.Cancelled, Message: "任务已取消"}
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status='cancelled',finished_at=?,result=? WHERE id=?", millis(now), jsonText(result), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE accounts SET next_run_at=? WHERE id=?", millis(a.Schedule.Next(now, a.ID, true)), a.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) BeginLogin(ctx context.Context, id string, now time.Time) (domain.Run, error) {
	var r domain.Run
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	a, err := getAccount(ctx, tx, id)
	if err != nil {
		return r, err
	}
	if a.ActiveRunID != "" {
		return r, domain.ErrBusy
	}
	site, err := getSite(ctx, tx, a.SiteID)
	if err != nil {
		return r, err
	}
	start := now.UTC()
	r = domain.Run{ID: domain.NewID(), AccountID: id, AccountName: a.Name, SiteName: site.Name, Action: "login", Trigger: "manual", Status: domain.WaitingUser, Attempt: 1, CreatedAt: start, StartedAt: &start}
	if err = insertRun(ctx, tx, r); err != nil {
		return r, err
	}
	return r, tx.Commit()
}
func (s *Store) FinishLogin(ctx context.Context, id string, c *domain.Credentials, status, message string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := getRun(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.Status != domain.WaitingUser {
		return domain.ErrConflict
	}
	if c != nil {
		a, err := getAccount(ctx, tx, r.AccountID)
		if err != nil {
			return err
		}
		if err = s.updateCredentials(ctx, tx, a.ID, *c, now); err != nil {
			return err
		}
		attempts, e := dailyAttempts(ctx, tx, a.ID, now)
		if e != nil {
			return e
		}
		next := a.Schedule.Next(now, a.ID, attempts > 0)
		nextAt, status, message := millis(next), "ready", ""
		if a.LastStatus == domain.Unverified || a.LastStatus == domain.Interrupted {
			nextAt = 0
			status = a.LastStatus
			message = a.LastMessage
		}
		if _, err = tx.ExecContext(ctx, "UPDATE accounts SET next_run_at=?,last_status=?,last_message=? WHERE id=?", nextAt, status, message, a.ID); err != nil {
			return err
		}
	}
	result := domain.Result{Status: status, Message: message, Evidence: "manual_session"}
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status=?,finished_at=?,result=? WHERE id=?", status, millis(now), jsonText(result), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Runs(ctx context.Context, f domain.RunFilter) (domain.RunPage, error) {
	page := domain.RunPage{Items: []domain.Run{}}
	where := " WHERE 1=1"
	args := []any{}
	if f.AccountID != "" {
		where += " AND account_id=?"
		args = append(args, f.AccountID)
	}
	if f.Status != "" {
		where += " AND status=?"
		args = append(args, f.Status)
	}
	if f.Action != "" {
		where += " AND action=?"
		args = append(args, f.Action)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs"+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, "SELECT "+runColumns+" FROM runs"+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, r)
	}
	return page, rows.Err()
}

func (s *Store) Recover(ctx context.Context, now time.Time) error {
	if err := s.checkAccountSecret(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result := domain.Result{Status: domain.Interrupted, Message: "上次进程在操作完成前退出；请核对站点结果后手动重试", Evidence: "process_recovery"}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET last_status='interrupted',last_message=?,next_run_at=0 WHERE id IN(SELECT account_id FROM runs WHERE status IN('running','waiting_user'))`, result.Message); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE runs SET status='interrupted',finished_at=?,result=? WHERE status IN('running','waiting_user')", millis(now), jsonText(result)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Prune(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM runs WHERE finished_at>0 AND finished_at<?", millis(cutoff))
	return err
}
