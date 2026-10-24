package app

import (
	"context"
	"strings"
	"testing"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type backupRepository struct {
	port.Repository
	reads  int
	backup domain.Backup
}

func (r *backupRepository) Export(context.Context) (domain.Backup, error) {
	r.reads++
	return r.backup, nil
}

func TestExportValidatesBeforeReadingAndUsesRestorePolicy(t *testing.T) {
	repo := &backupRepository{backup: domain.Backup{Version: domain.BackupVersion, Sites: make([]domain.Site, domain.MaxSites+1)}}
	service := &Service{Repo: repo}
	if _, err := service.Export(context.Background(), "short"); err == nil || repo.reads != 0 {
		t.Fatal("invalid password read all secrets", repo.reads, err)
	}
	if data, err := service.Export(context.Background(), "long-fixture-password"); err == nil || data != nil || !strings.Contains(err.Error(), "站点数量") || repo.reads != 1 {
		t.Fatal("unrestorable backup was encrypted", err)
	}
}
