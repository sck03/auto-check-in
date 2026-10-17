//go:build windows

package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"auto-check-in/internal/app"
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type countingStartup struct{ reads atomic.Int32 }

func (s *countingStartup) Enabled() bool { s.reads.Add(1); return false }
func (*countingStartup) Set(bool) error  { return nil }

type menuRepository struct{ port.Repository }

func (*menuRepository) SaveSettings(_ context.Context, settings domain.Settings) (domain.Settings, error) {
	return settings, nil
}

func TestHiddenDesktopSleepsUntilSettingsChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		startup := &countingStartup{}
		service := app.New(ctx, &menuRepository{}, nil, nil, true)
		defer service.CloseLogins()
		manager := &Manager{ctx: ctx, startup: startup, service: service, commands: make(chan func(), 1), prefs: domain.DesktopPreferences{MinimizeToTray: true}}
		done := make(chan struct{})
		go func() { manager.loop(); close(done) }()
		synctest.Wait()
		time.Sleep(14 * time.Hour)
		synctest.Wait()
		if startup.reads.Load() != 1 {
			t.Fatal("hidden tray kept reading settings")
		}
		if _, err := service.SaveSettings(ctx, domain.DefaultSettings()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if startup.reads.Load() != 2 {
			t.Fatal("committed settings did not refresh the tray")
		}
		service.Engine.Notify()
		synctest.Wait()
		if startup.reads.Load() != 2 {
			t.Fatal("task activity unnecessarily refreshed the tray")
		}
		cancel()
		<-done
	})
}
