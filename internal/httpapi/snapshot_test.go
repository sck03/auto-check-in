package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"auto-check-in/internal/domain"
)

func TestConditionalSnapshotsChangeWithDataAndStillRequireAuthentication(t *testing.T) {
	server, service := testAPI(t)
	ctx := context.Background()
	a, err := service.SaveAccount(ctx, "", domain.AccountInput{Name: "snapshot", SiteID: "anyrouter", Enabled: true, Schedule: domain.DefaultSchedule()})
	if err != nil {
		t.Fatal(err)
	}
	get := func(etag, token string) (int, string, int) {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("If-None-Match", etag)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("snapshot became persistently cacheable")
		}
		return res.StatusCode, res.Header.Get("ETag"), len(body)
	}
	status, etag, size := get("", "test-private-token")
	if status != 200 || etag == "" || size == 0 {
		t.Fatal(status, etag, size)
	}
	if status, _, size = get("W/"+etag, "test-private-token"); status != 304 || size != 0 {
		t.Fatal("unchanged data was transferred", status, size)
	}
	if status, _, _ = get(etag, "wrong"); status != 401 {
		t.Fatal("ETag bypassed authentication", status)
	}
	_, err = service.SaveAccount(ctx, a.ID, domain.AccountInput{Name: "changed", SiteID: a.SiteID, Enabled: a.Enabled, Schedule: a.Schedule, Version: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if status, next, _ := get(etag, "test-private-token"); status != 200 || next == etag {
		t.Fatal("mutation did not invalidate snapshot", status, next)
	}
}

func TestSnapshotScopeInvalidatesUnchangedDataAtBeijingDayBoundary(t *testing.T) {
	get := func(scope, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/accounts", nil)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		respondSnapshot(w, r, []domain.Account{}, scope)
		return w
	}
	first := get("2026-10-24", "")
	etag := first.Header().Get("ETag")
	if same := get("2026-10-24", etag); same.Code != 304 {
		t.Fatal(same.Code)
	}
	if next := get("2026-10-25", etag); next.Code != 200 || next.Header().Get("ETag") == etag {
		t.Fatal("daily completion view would stay stale", next.Code)
	}
}
