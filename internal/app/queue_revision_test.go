package app

import (
	"context"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type reschedulingRepository struct {
	port.Repository
	revision uint64
}

func (r *reschedulingRepository) QueueRevision() uint64 { return r.revision }
func (r *reschedulingRepository) Claim(context.Context, domain.ClaimOptions) (*domain.Run, error) {
	r.revision++
	return nil, nil
}

func TestImplicitQueueRescheduleRearmsScheduler(t *testing.T) {
	repo := &reschedulingRepository{}
	engine := NewEngine(repo, nil)
	wake := engine.scheduleWake
	if _, _, _, err := engine.claim(context.Background(), domain.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	default:
		t.Fatal("scheduler would sleep past a committed replacement plan")
	}
}
