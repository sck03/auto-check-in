package app

import (
	"context"
	"errors"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type oversizedLoginSession struct{ port.InteractiveSession }

func (oversizedLoginSession) Snapshot(context.Context) (domain.Credentials, error) {
	return domain.Credentials{}, domain.ErrSessionLimit
}

func TestLoginSnapshotPreservesActionableSessionLimit(t *testing.T) {
	_, err := loginSnapshot(context.Background(), &loginSession{session: oversizedLoginSession{}})
	if !errors.Is(err, domain.ErrSessionLimit) {
		t.Fatal("session limit hidden behind retry message", err)
	}
}
