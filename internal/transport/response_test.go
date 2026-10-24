package transport

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterNumbersDatesAndInvalidValues(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 500000000, time.UTC)
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"300", 300}, {" 900 ", 900}, {"999999999", 86400},
		{now.Add(10 * time.Minute).Format(http.TimeFormat), 600},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), 86400},
		{"-1", 0}, {"1.5", 0}, {"invalid", 0}, {"", 0},
	} {
		if got := ParseRetryAfter(tc.value, now); got != tc.want {
			t.Errorf("%q: got %d want %d", tc.value, got, tc.want)
		}
	}
}
