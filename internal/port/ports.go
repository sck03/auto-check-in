// Package port defines boundaries implemented by infrastructure.
package port

import (
	"auto-check-in/internal/domain"
	"context"
	"errors"
	"time"
)

var (
	ErrManualRequired     = errors.New("需要人工登录或处理验证")
	ErrUncertain          = errors.New("操作已发出但未收到可核验结果")
	ErrBrowserUnavailable = errors.New("未找到 Chromium / Chrome / Edge，请配置 CHECKIN_BROWSER_PATH")
	ErrBrowserBusy        = errors.New("浏览器会话已达上限（2 个），请关闭一个登录会话后再试")
)

type Response struct {
	Status            int
	Body              []byte
	RetryAfterSeconds int
}

type DesktopIntegration interface {
	Status() domain.DesktopStatus
	Configure(domain.DesktopPreferences) (domain.DesktopStatus, error)
	Action(string) error
}

type Session interface {
	Request(context.Context, string, string, any) (Response, error)
	Login(context.Context) (Response, error)
	PageCheckIn(context.Context) (domain.Result, error)
	Snapshot(context.Context) (domain.Credentials, error)
	Close()
}

type SessionFactory interface {
	Open(context.Context, domain.Site, domain.Credentials) (Session, error)
}

type Executor interface {
	Execute(context.Context, domain.Site, domain.Credentials) (domain.Result, *domain.Credentials)
}

// CapacitySource is optional: executors without scarce resources need no hook.
// Changed is a broadcast captured with the snapshot; nil means no notifications.
type Capacity struct {
	BlockedTransports []string
	Changed           <-chan struct{}
}
type CapacitySource interface{ Capacity() Capacity }

type Interaction struct {
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Text   string  `json:"text"`
	Key    string  `json:"key"`
	DeltaY float64 `json:"delta_y"`
}

type InteractiveSession interface {
	Session
	NativeWindow() bool
	ConfirmedLogin(context.Context) bool
	Done() <-chan struct{}
	Screenshot(context.Context) ([]byte, error)
	Interact(context.Context, Interaction) error
}

type BrowserFactory interface {
	SessionFactory
	OpenInteractive(context.Context, domain.Site, domain.Credentials) (InteractiveSession, error)
	Available() bool
}

type Repository interface {
	Sites(context.Context) ([]domain.Site, error)
	Site(context.Context, string) (domain.Site, error)
	SaveSite(context.Context, domain.Site) (domain.Site, error)
	DeleteSite(context.Context, string) error
	Accounts(context.Context) ([]domain.Account, error)
	Account(context.Context, string) (domain.Account, error)
	Credentials(context.Context, string) (domain.Credentials, error)
	SaveAccount(context.Context, domain.Account, domain.Credentials, time.Time) (domain.Account, error)
	DeleteAccount(context.Context, string) error
	Settings(context.Context) (domain.Settings, error)
	SaveSettings(context.Context, domain.Settings) (domain.Settings, error)
	DueAccounts(context.Context, time.Time) ([]string, error)
	NextScheduled(context.Context) (*time.Time, error)
	Enqueue(context.Context, string, string, time.Time) (domain.Run, error)
	// Claim skips unavailable origins/transports without consuming their tasks.
	Claim(context.Context, domain.ClaimOptions) (*domain.Run, error)
	Finish(context.Context, string, domain.Result, *domain.Credentials, domain.Settings, time.Time) error
	CancelQueued(context.Context, string, time.Time) error
	BeginLogin(context.Context, string, time.Time) (domain.Run, error)
	FinishLogin(context.Context, string, *domain.Credentials, string, string, time.Time) error
	Runs(context.Context, domain.RunFilter) (domain.RunPage, error)
	Recover(context.Context, time.Time) error
	Prune(context.Context, time.Time) error
	Export(context.Context) (domain.Backup, error)
	Restore(context.Context, domain.Backup) error
	Close() error
}
