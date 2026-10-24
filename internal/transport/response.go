package transport

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const ResponseBodyLimit = 1 << 20

// ParseRetryAfter is shared by HTTP and browser sessions. Round dates up so a
// request never runs before the server's deadline; cap untrusted values.
func ParseRetryAfter(value string, now time.Time) int {
	value = strings.TrimSpace(value)
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err == nil {
		return int(min(seconds, 86400))
	}
	if deadline, err := http.ParseTime(value); err == nil {
		return int(max(0, min(86400, math.Ceil(deadline.Sub(now).Seconds()))))
	}
	return 0
}
