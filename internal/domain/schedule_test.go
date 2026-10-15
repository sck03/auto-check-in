package domain

import (
	"fmt"
	"testing"
	"time"
)

func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func TestDailyWindowDrawIsStableAndIndependent(t *testing.T) {
	s := DefaultSchedule()
	day := instant("2026-10-15T00:00:00Z")
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("account-%d", i)
		at := s.ScheduledTime(day, id)
		if !s.WithinWindow(at) {
			t.Fatalf("outside Beijing 08–09: %v", at)
		}
		if at != s.ScheduledTime(day.Add(30*time.Minute), id) {
			t.Fatal("restart or clock tick redrew time")
		}
		seen[at.Unix()] = true
	}
	if len(seen) < 90 {
		t.Fatalf("accounts not independently spread: %d", len(seen))
	}
	if s.ScheduledTime(day, "a") == s.ScheduledTime(day.AddDate(0, 0, 1), "a") {
		t.Fatal("daily draw did not change")
	}
}
func TestDailySuccessConsumesDay(t *testing.T) {
	s := DefaultSchedule()
	now := instant("2026-10-15T00:01:00Z")
	next := s.Next(now, "a", true)
	if next.Before(instant("2026-10-16T00:00:00Z")) || !s.WithinWindow(next) {
		t.Fatal(next)
	}
	last := instant("2026-10-14T16:01:00Z")
	if !s.InCompletedCycle(now, &last) {
		t.Fatal("Beijing day boundary ignored")
	}
	if s.InCompletedCycle(instant("2026-10-15T16:00:00Z"), &last) {
		t.Fatal("next Beijing day blocked")
	}
}
func TestWindowAndRetryBoundaries(t *testing.T) {
	s := DefaultSchedule()
	for _, tc := range []struct {
		at           string
		first, retry bool
	}{
		{"2026-10-14T23:59:59Z", false, false},
		{"2026-10-15T00:00:00Z", true, true},
		{"2026-10-15T01:00:00Z", false, true},
		{"2026-10-15T01:29:59Z", false, true},
		{"2026-10-15T01:30:00Z", false, false},
	} {
		if s.WithinWindow(instant(tc.at)) != tc.first || s.WithinRetryWindow(instant(tc.at)) != tc.retry {
			t.Fatal(tc)
		}
	}
}
func TestInvalidSchedule(t *testing.T) {
	for _, s := range []Schedule{{Timezone: "UTC", WindowStart: "08:00", WindowEnd: "09:00", RetryUntil: "09:30"}, {Timezone: "Asia/Shanghai", WindowStart: "99:00", WindowEnd: "09:00", RetryUntil: "09:30"}, {Timezone: "Asia/Shanghai", WindowStart: "09:00", WindowEnd: "08:00", RetryUntil: "09:30"}, {Timezone: "Asia/Shanghai", WindowStart: "08:00", WindowEnd: "09:00", RetryUntil: "08:30"}} {
		if s.Validate() == nil {
			t.Fatalf("accepted invalid schedule %+v", s)
		}
	}
}
func TestSiteRejectsCredentialRedirects(t *testing.T) {
	for _, base := range []string{"https://user:pass@example.com", "https://example.com/path", "javascript:alert(1)", "http://example.com"} {
		s := Templates()[2].Site
		s.Name = "test"
		s.BaseURL = base
		if s.Validate(false) == nil {
			t.Fatal(base)
		}
	}
	for _, path := range []string{"//evil.test", "https://evil.test", "/\\evil.test", "/%2f%2fevil.test"} {
		if ValidatePath(path) == nil {
			t.Fatal(path)
		}
	}
}
