package app

import (
	"context"
	"errors"
	"testing"

	"auto-check-in/internal/domain"
)

func TestSettingsNotificationsFollowCommittedChanges(t *testing.T) {
	service, accountID := loginService(t, &loginBrowser{})
	ctx := context.Background()
	settings, err := service.Repo.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	changed, work := service.SettingsChanged(), service.Engine.subscription()
	settings.SchedulerEnabled = !settings.SchedulerEnabled
	saved, err := service.SaveSettings(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, notification := range []<-chan struct{}{changed, work} {
		select {
		case <-notification:
		default:
			t.Fatal("committed settings did not wake subscribers")
		}
	}
	changed = service.SettingsChanged()
	if _, err := service.SaveSettings(ctx, settings); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale settings were accepted", err)
	}
	select {
	case <-changed:
		t.Fatal("failed settings write emitted a change")
	default:
	}
	service.Engine.Notify()
	select {
	case <-changed:
		t.Fatal("task notifications unnecessarily refreshed settings")
	default:
	}
	backup, err := service.Export(ctx, "test-settings-backup-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Repo.DeleteAccount(ctx, accountID); err != nil {
		t.Fatal(err)
	}
	saved.SchedulerEnabled = !saved.SchedulerEnabled
	if _, err := service.SaveSettings(ctx, saved); err != nil {
		t.Fatal(err)
	}
	changed = service.SettingsChanged()
	if err := service.Restore(ctx, backup, "test-settings-backup-password"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("restored settings did not wake subscribers")
	}
}
