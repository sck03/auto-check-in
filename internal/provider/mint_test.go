package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/transport"
)

func TestMintDailySpin(t *testing.T) {
	for _, mode := range []string{"available", "already", "ambiguous", "missing_record", "expired"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			spins := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer reward-token" {
					t.Error("missing reward-site token")
				}
				if mode == "expired" {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"success":false,"message":"token expired"}`)
					return
				}
				if r.URL.Path == "/api/checkin/status" {
					switch {
					case mode == "already" || spins > 0:
						fmt.Fprint(w, `{"success":true,"can_spin":false,"today_record":{"quota_amount":75000}}`)
					case mode == "missing_record":
						fmt.Fprint(w, `{"success":true,"can_spin":false,"today_record":null}`)
					default:
						fmt.Fprint(w, `{"success":true,"can_spin":true}`)
					}
				} else if r.URL.Path == "/api/checkin/spin" && r.Method == "POST" {
					spins++
					if mode == "ambiguous" {
						fmt.Fprint(w, "<html>response missing</html>")
					} else {
						fmt.Fprint(w, `{"success":true,"quota":75000,"message":"恭喜获得 150次！"}`)
					}
				} else {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			site := domain.Templates()[5].Site
			site.BaseURL, site.Transport = server.URL, "http"
			r, _ := (&Runner{Factory: transport.Factory{}}).Execute(context.Background(), site, domain.Credentials{Method: "cookie", Token: "reward-token"})
			mu.Lock()
			defer mu.Unlock()
			switch mode {
			case "expired", "missing_record":
				if r.Complete() || spins != 0 {
					t.Fatalf("unconfirmed/expired session triggered or confirmed a spin: %+v spins=%d", r, spins)
				}
			default:
				if !r.Complete() || r.Reward == nil || *r.Reward != 75000 || r.QuotaPerUnit != 500 || r.Unit != "次" {
					t.Fatalf("wrong spin result or reward units: %+v", r)
				}
				if (mode == "already" && spins != 0) || (mode != "already" && spins != 1) {
					t.Fatalf("spin was duplicated: %d", spins)
				}
			}
		})
	}
}
