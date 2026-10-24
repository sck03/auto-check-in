package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"auto-check-in/internal/domain"
)

func dailyAttempts(ctx context.Context, q queryer, id string, now time.Time) (int, error) {
	start, end := domain.DayBounds(now)
	var count int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE account_id=? AND action='checkin' AND started_at>=? AND started_at<? AND COALESCE(json_extract(result,'$.evidence'),'')<>'before_reset'", id, millis(start), millis(end)).Scan(&count)
	return count, err
}

func scheduleNext(ctx context.Context, tx *sql.Tx, a domain.Account, now time.Time, attempted bool) error {
	next := a.RespectCooldown(a.Schedule.Next(now, a.ID, attempted))
	_, err := tx.ExecContext(ctx, "UPDATE accounts SET next_run_at=? WHERE id=?", millis(next), a.ID)
	return err
}

func cooldownActive(a domain.Account, now time.Time) bool {
	return a.RetryNotBefore != nil && now.Before(*a.RetryNotBefore)
}

func cooldownError(a domain.Account) error {
	day, _ := domain.DayBounds(*a.RetryNotBefore)
	return fmt.Errorf("%w（北京时间 %s 后）", domain.ErrRateLimited, a.RetryNotBefore.In(day.Location()).Format("2006-01-02 15:04:05"))
}

// Retry admission and next-day placement share one persistent lower bound.
// Exhausting a daily budget or editing a schedule never erases Retry-After.
func resultSchedule(a domain.Account, run domain.Run, result domain.Result, settings domain.Settings, now time.Time) (int64, int64) {
	if seconds := min(result.RetryAfterSeconds, domain.MaxRetryAfterSeconds); seconds > 0 {
		until := now.Add(time.Duration(seconds) * time.Second)
		if a.RetryNotBefore == nil || until.After(*a.RetryNotBefore) {
			a.RetryNotBefore = &until
		}
	}
	next := a.Schedule.Next(now, a.ID, true)
	if result.Evidence == "before_reset" {
		day, _ := domain.DayBounds(now)
		next = a.Schedule.ScheduledTime(now, a.ID)
		if reset := day.Add(8 * time.Hour); next.Before(reset) {
			next = reset
		}
	}
	if result.Status == domain.Failed && result.Retryable && run.Attempt <= settings.MaxRetries {
		delay := settings.RetryBaseSeconds
		for attempt := 1; attempt < run.Attempt; attempt++ {
			delay *= 3
		}
		delay = min(max(delay, result.RetryAfterSeconds), domain.MaxRetryAfterSeconds)
		hash := sha256.Sum256([]byte(run.ID))
		jitter := time.Duration(int(hash[0])%31) * time.Second
		start, end := a.Schedule.RetryBounds(now)
		retryAt := now.Add(time.Duration(delay)*time.Second + jitter)
		if retryAt.Before(start) {
			retryAt = start.Add(jitter)
		}
		if retryAt.Before(end) {
			next = retryAt
		}
	}
	if result.BlocksSchedule() {
		return 0, ptrMillis(a.RetryNotBefore)
	}
	return millis(a.RespectCooldown(next)), ptrMillis(a.RetryNotBefore)
}

// Upgrade v5 results into durable account state, including rate limits that
// straddle the original random time. No remote operation is replayed.
func recoverCooldowns(tx *sql.Tx, now time.Time) error {
	_, err := tx.Exec(`UPDATE accounts SET retry_not_before=MAX(retry_not_before,COALESCE((
SELECT MAX(finished_at+MIN(CAST(json_extract(result,'$.retry_after_seconds') AS INTEGER),?)*1000)
FROM runs WHERE account_id=accounts.id AND action='checkin' AND finished_at>=?
AND json_extract(result,'$.retry_after_seconds')>0),0))`, domain.MaxRetryAfterSeconds, millis(now.Add(-24*time.Hour)))
	if err != nil {
		return err
	}
	rows, err := tx.Query(accountSelect + " WHERE a.next_run_at>0 AND a.next_run_at<a.retry_not_before")
	if err != nil {
		return err
	}
	var accounts []domain.Account
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return e
		}
		accounts = append(accounts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if _, err = tx.Exec("UPDATE accounts SET next_run_at=? WHERE id=?", millis(a.RespectCooldown(*a.NextRunAt)), a.ID); err != nil {
			return err
		}
	}
	return nil
}
