package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Conditional reads keep full-list API compatibility while eliminating unchanged
// payload transfer and client parsing. scope invalidates date-derived UI state.
func respondSnapshot(w http.ResponseWriter, r *http.Request, value any, scope string) {
	body, err := json.Marshal(value)
	if err != nil {
		writeError(w, 500, "snapshot", "无法读取当前状态")
		return
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(scope))
	_, _ = hash.Write(body)
	etag := `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
	w.Header().Set("ETag", etag)
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}
