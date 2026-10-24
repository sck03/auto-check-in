package app

import (
	"context"
	"time"

	"auto-check-in/internal/domain"
)

type originState struct {
	busy bool
	next time.Time
}

// Claim and origin reservation are serialized; waiting work stays queued and
// available worker slots can serve other origins. No storage mutex spans I/O
// to a remote site, and idle workers wait on notifications or the nearest gap.
func (e *Engine) claim(ctx context.Context, settings domain.Settings) (*domain.Run, string, time.Time, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now().UTC()
	var excluded []string
	var next time.Time
	for origin, state := range e.gates {
		if !state.busy && !state.next.After(now) {
			delete(e.gates, origin)
			continue
		}
		excluded = append(excluded, origin)
		if !state.busy && (next.IsZero() || state.next.Before(next)) {
			next = state.next
		}
	}
	run, err := e.repo.Claim(ctx, settings.SchedulerEnabled, now, excluded...)
	if err != nil || run == nil {
		return run, "", next, err
	}
	a, err := e.repo.Account(ctx, run.AccountID)
	if err != nil {
		return run, "", next, err
	}
	site, err := e.repo.Site(ctx, a.SiteID)
	if err != nil {
		return run, "", next, err
	}
	e.gates[site.BaseURL] = originState{busy: true}
	return run, site.BaseURL, next, nil
}

func (e *Engine) releaseOrigin(origin string, gapSeconds int) {
	e.mu.Lock()
	if origin != "" {
		e.gates[origin] = originState{next: time.Now().Add(time.Duration(gapSeconds) * time.Second)}
	}
	e.mu.Unlock()
	e.Notify()
}
