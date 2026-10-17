package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"
	_ "time/tzdata"
)

type Schedule struct {
	Timezone    string `json:"timezone"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	RetryUntil  string `json:"retry_until"`
}

var scheduleLocation = func() *time.Location {
	// Embedded tzdata makes the schedule independent of the host.
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}()

func DefaultSchedule() Schedule {
	return Schedule{Timezone: "Asia/Shanghai", WindowStart: "08:00", WindowEnd: "09:00", RetryUntil: "09:00"}
}
func clockMinutes(value string) (int, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, fmt.Errorf("时间应为 HH:MM")
	}
	h, eh := strconv.Atoi(value[:2])
	m, em := strconv.Atoi(value[3:])
	if eh != nil || em != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("无效的时间")
	}
	return h*60 + m, nil
}
func (s Schedule) Validate() error {
	if s.Timezone != "Asia/Shanghai" {
		return fmt.Errorf("签到计划统一使用北京时间 Asia/Shanghai")
	}
	start, err := clockMinutes(s.WindowStart)
	if err != nil {
		return err
	}
	end, err := clockMinutes(s.WindowEnd)
	if err != nil {
		return err
	}
	if end <= start {
		return fmt.Errorf("每日时间窗口的结束时间必须晚于开始时间")
	}
	retryUntil, err := clockMinutes(s.RetryUntil)
	if err != nil {
		return err
	}
	if retryUntil < end {
		return fmt.Errorf("补签截止时间不能早于首次执行窗口结束时间")
	}
	return nil
}
func DayBounds(now time.Time) (time.Time, time.Time) {
	local := now.In(scheduleLocation)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, scheduleLocation)
	return start, start.AddDate(0, 0, 1)
}
func (s Schedule) Bounds(now time.Time) (time.Time, time.Time) {
	day, _ := DayBounds(now)
	start, _ := clockMinutes(s.WindowStart)
	end, _ := clockMinutes(s.WindowEnd)
	return day.Add(time.Duration(start) * time.Minute), day.Add(time.Duration(end) * time.Minute)
}
func (s Schedule) WithinWindow(now time.Time) bool {
	start, end := s.Bounds(now)
	return !now.Before(start) && now.Before(end)
}
func (s Schedule) RetryBounds(now time.Time) (time.Time, time.Time) {
	day, _ := DayBounds(now)
	start, _ := clockMinutes(s.WindowStart)
	end, _ := clockMinutes(s.RetryUntil)
	return day.Add(time.Duration(start) * time.Minute), day.Add(time.Duration(end) * time.Minute)
}
func (s Schedule) WithinRetryWindow(now time.Time) bool {
	start, end := s.RetryBounds(now)
	return !now.Before(start) && now.Before(end)
}

// ScheduledTime draws a separate second for each account and Beijing date.
// Restarting does not draw again. The end of the window is exclusive.
func (s Schedule) ScheduledTime(now time.Time, accountID string) time.Time {
	start, end := s.Bounds(now)
	span := uint64(end.Sub(start) / time.Second)
	if span == 0 {
		return start.UTC()
	}
	hash := sha256.Sum256([]byte(accountID + ":" + start.Format("2006-01-02")))
	return start.Add(time.Duration(binary.BigEndian.Uint64(hash[:8])%span) * time.Second).UTC()
}
func (s Schedule) Next(now time.Time, accountID string, consumed bool) time.Time {
	at := s.ScheduledTime(now, accountID)
	if consumed || !at.After(now) {
		_, tomorrow := DayBounds(now)
		return s.ScheduledTime(tomorrow, accountID)
	}
	return at
}
func (s Schedule) InCompletedCycle(now time.Time, last *time.Time) bool {
	if last == nil {
		return false
	}
	start, end := DayBounds(now)
	return !last.Before(start) && last.Before(end)
}
