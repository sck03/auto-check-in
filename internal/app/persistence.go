package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"auto-check-in/internal/domain"
)

// A pending write never replays a site operation. Unknown results keep the
// account reserved until the database accepts the terminal state or a restart
// invokes normal interrupted-task recovery. Credentials are not retained here.
type pendingResult struct {
	id          string
	result      domain.Result
	credentials *domain.Credentials
	settings    domain.Settings
	at          time.Time
	login       bool
}

type EngineStats struct {
	PendingResults int    `json:"pending_results"`
	SaveFailures   uint64 `json:"save_failures"`
}

func (e *Engine) Stats() EngineStats {
	e.persistMu.Lock()
	defer e.persistMu.Unlock()
	return EngineStats{PendingResults: len(e.pending), SaveFailures: e.saveFailures}
}

func (e *Engine) writeResult(ctx context.Context, p pendingResult) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	if p.login {
		err = e.repo.FinishLogin(ctx, p.id, p.credentials, p.result.Status, p.result.Message, p.at)
	} else {
		err = e.repo.Finish(ctx, p.id, p.result, p.credentials, p.settings, p.at)
	}
	if err != nil {
		e.persistMu.Lock()
		e.saveFailures++
		e.persistMu.Unlock()
	}
	return err
}

func (e *Engine) finishResult(p pendingResult) {
	if err := e.writeResult(context.Background(), p); err == nil {
		return
	} else {
		slog.Error("task result could not be saved", "run_id", p.id, "error", err)
	}
	p.credentials = nil
	p.result = domain.Result{Status: domain.Unverified, Message: "执行结束，但会话或结果保存失败；请核对站点结果", Evidence: "persistence_error", RetryAfterSeconds: p.result.RetryAfterSeconds}
	// The fallback has its own deadline, independent of the first failed write.
	if err := e.writeResult(context.Background(), p); err == nil {
		return
	}
	e.queueResult(p)
}

func (e *Engine) queueResult(p pendingResult) {
	p.credentials = nil
	e.persistMu.Lock()
	e.pending[p.id] = p
	e.persistMu.Unlock()
	select {
	case e.recoveryWake <- struct{}{}:
	default:
	}
}

func (e *Engine) retryPending(ctx context.Context) {
	e.persistMu.Lock()
	list := make([]pendingResult, 0, len(e.pending))
	for _, p := range e.pending {
		list = append(list, p)
	}
	e.persistMu.Unlock()
	for _, p := range list {
		if ctx.Err() != nil {
			return
		}
		err := e.writeResult(ctx, p)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			continue
		}
		e.persistMu.Lock()
		delete(e.pending, p.id)
		e.persistMu.Unlock()
		e.Notify()
	}
}

func (e *Engine) recoveryLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.recoveryWake:
		}
		for e.Stats().PendingResults > 0 {
			if !wait(ctx, 5*time.Second) {
				return
			}
			e.retryPending(ctx)
		}
	}
}
