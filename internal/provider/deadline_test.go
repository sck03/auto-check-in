package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type expiringSession struct {
	port.Session
	requests int
}

func (s *expiringSession) Request(ctx context.Context, method, _ string, _ any) (port.Response, error) {
	s.requests++
	if method == "POST" {
		<-ctx.Done()
		return port.Response{}, port.ErrUncertain
	}
	return port.Response{Status: 200, Body: []byte(`{"success":true,"can_spin":true,"data":{"stats":{"checked_in_today":false}}}`)}, nil
}

func TestDeadlineDoesNotStartAnotherSiteRequest(t *testing.T) {
	for _, index := range []int{2, 5} {
		site := domain.Templates()[index].Site
		t.Run(site.Adapter, func(t *testing.T) {
			session := &expiringSession{}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := DefaultRegistry()[site.Adapter].CheckIn(ctx, session, site, port.Response{})
			if !errors.Is(err, port.ErrUncertain) || session.requests != 2 {
				t.Fatalf("deadline started an extra verification request: requests=%d err=%v", session.requests, err)
			}
		})
	}
}
