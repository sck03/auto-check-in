// Package domain contains business values and rules without infrastructure dependencies.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound   = errors.New("记录不存在")
	ErrConflict   = errors.New("记录已变更或正在使用，请刷新后重试")
	ErrBusy       = errors.New("该账号已有进行中的任务")
	ErrCooldown   = errors.New("该账号在当前周期已完成签到")
	ErrDisabled   = errors.New("账号已停用")
	ErrDailyLimit = errors.New("该账号今天已经执行过；如需再次执行请明确选择手动重试")
)

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Selectors struct {
	LoginEntry string `json:"login_entry"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	Submit     string `json:"submit"`
	GitHub     string `json:"github"`
	LinuxDO    string `json:"linuxdo"`
	CheckIn    string `json:"checkin"`
	Success    string `json:"success"`
	Already    string `json:"already"`
}

type Site struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	BaseURL      string    `json:"base_url"`
	TemplateID   string    `json:"template_id"`
	Adapter      string    `json:"adapter"`
	Transport    string    `json:"transport"`
	LoginPath    string    `json:"login_path"`
	LoginAPIPath string    `json:"login_api_path"`
	CheckInPath  string    `json:"checkin_path"`
	UserInfoPath string    `json:"user_info_path"`
	UserHeader   string    `json:"user_header"`
	QuotaPerUnit int64     `json:"quota_per_unit"`
	Unit         string    `json:"unit"`
	Selectors    Selectors `json:"selectors"`
	Version      int64     `json:"version"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"http_only"`
	SameSite string  `json:"same_site"`
	Expires  float64 `json:"expires"`
}

type Credentials struct {
	Method       string                       `json:"method"`
	Username     string                       `json:"username"`
	Password     string                       `json:"password"`
	CookieHeader string                       `json:"cookie_header"`
	APIUser      string                       `json:"api_user"`
	Token        string                       `json:"token"`
	UserAgent    string                       `json:"user_agent,omitempty"`
	Cookies      []Cookie                     `json:"cookies,omitempty"`
	Storage      map[string]map[string]string `json:"storage,omitempty"`
}

type CredentialPatch struct {
	Method       *string `json:"method"`
	Username     *string `json:"username"`
	Password     *string `json:"password"`
	CookieHeader *string `json:"cookie_header"`
	APIUser      *string `json:"api_user"`
	Token        *string `json:"token"`
}

func (c Credentials) Apply(p *CredentialPatch) Credentials {
	if p == nil {
		return c
	}
	if (p.Username != nil && *p.Username != c.Username) || (p.Password != nil && *p.Password != c.Password) || (p.Token != nil && *p.Token != c.Token) {
		// Credentials for another identity must never reuse a verified old session.
		c.Cookies, c.Storage = nil, nil
		c.CookieHeader, c.APIUser, c.Token, c.UserAgent = "", "", "", ""
	}
	if p.Method != nil {
		c.Method = *p.Method
	}
	if p.Username != nil {
		c.Username = *p.Username
	}
	if p.Password != nil {
		c.Password = *p.Password
	}
	if p.CookieHeader != nil {
		c.CookieHeader = *p.CookieHeader
		c.Cookies = nil
		c.Storage = nil
		c.UserAgent = ""
	}
	if p.APIUser != nil {
		c.APIUser = *p.APIUser
	}
	if p.Token != nil {
		c.Token = *p.Token
	}
	return c
}

type AuthSummary struct {
	Method      string `json:"method"`
	Username    string `json:"username"`
	APIUser     string `json:"api_user"`
	HasPassword bool   `json:"has_password"`
	HasSession  bool   `json:"has_session"`
	HasToken    bool   `json:"has_token"`
}

func (c Credentials) Summary() AuthSummary {
	hasSession := c.CookieHeader != "" || len(c.Cookies) > 0
	for _, storage := range c.Storage {
		if len(storage) > 0 {
			hasSession = true
			break
		}
	}
	return AuthSummary{c.Method, c.Username, c.APIUser, c.Password != "", hasSession, c.Token != ""}
}

type Account struct {
	ID            string      `json:"id"`
	SiteID        string      `json:"site_id"`
	Name          string      `json:"name"`
	Enabled       bool        `json:"enabled"`
	Schedule      Schedule    `json:"schedule"`
	Auth          AuthSummary `json:"auth"`
	Version       int64       `json:"version"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	NextRunAt     *time.Time  `json:"next_run_at"`
	LastRunAt     *time.Time  `json:"last_run_at"`
	LastSuccessAt *time.Time  `json:"last_success_at"`
	LastStatus    string      `json:"last_status"`
	LastMessage   string      `json:"last_message"`
	ActiveRunID   string      `json:"active_run_id"`
	ActiveStatus  string      `json:"active_status"`
}

type AccountInput struct {
	Name        string           `json:"name"`
	SiteID      string           `json:"site_id"`
	Enabled     bool             `json:"enabled"`
	Schedule    Schedule         `json:"schedule"`
	Version     int64            `json:"version"`
	Credentials *CredentialPatch `json:"credentials"`
}

const (
	Queued         = "queued"
	Running        = "running"
	WaitingUser    = "waiting_user"
	Succeeded      = "succeeded"
	AlreadyDone    = "already_done"
	NeedsAttention = "needs_attention"
	Unverified     = "unverified"
	Failed         = "failed"
	Interrupted    = "interrupted"
	Cancelled      = "cancelled"
)

type Result struct {
	Status            string `json:"status"`
	Message           string `json:"message"`
	Evidence          string `json:"evidence"`
	Stage             string `json:"stage,omitempty"`
	HTTPStatus        int    `json:"http_status,omitempty"`
	Advice            string `json:"advice,omitempty"`
	Reward            *int64 `json:"reward"`
	BalanceBefore     *int64 `json:"balance_before"`
	BalanceAfter      *int64 `json:"balance_after"`
	Unit              string `json:"unit"`
	QuotaPerUnit      int64  `json:"quota_per_unit"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func (r Result) Complete() bool { return r.Status == Succeeded || r.Status == AlreadyDone }
func (r Result) BlocksSchedule() bool {
	return r.Status == NeedsAttention || r.Status == Unverified || r.Status == Interrupted
}

type Run struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"account_id"`
	AccountName string     `json:"account_name"`
	SiteName    string     `json:"site_name"`
	Action      string     `json:"action"`
	Trigger     string     `json:"trigger"`
	Status      string     `json:"status"`
	Attempt     int        `json:"attempt"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Result      Result     `json:"result"`
}

type RunFilter struct {
	AccountID, Status, Action string
	Limit, Offset             int
}
type RunPage struct {
	Items []Run `json:"items"`
	Total int   `json:"total"`
}

type Settings struct {
	SchedulerEnabled   bool  `json:"scheduler_enabled"`
	Concurrency        int   `json:"concurrency"`
	MaxRetries         int   `json:"max_retries"`
	RetryBaseSeconds   int   `json:"retry_base_seconds"`
	TaskTimeoutSeconds int   `json:"task_timeout_seconds"`
	SiteGapSeconds     int   `json:"site_gap_seconds"`
	RetentionDays      int   `json:"retention_days"`
	Version            int64 `json:"version"`
}

func DefaultSettings() Settings {
	return Settings{SchedulerEnabled: true, Concurrency: 2, MaxRetries: 2, RetryBaseSeconds: 300, TaskTimeoutSeconds: 120, SiteGapSeconds: 5, RetentionDays: 90}
}

type BackupAccount struct {
	Account     Account     `json:"account"`
	Credentials Credentials `json:"credentials"`
}
type Backup struct {
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	Sites     []Site          `json:"sites"`
	Accounts  []BackupAccount `json:"accounts"`
	Settings  Settings        `json:"settings"`
	Runs      []Run           `json:"runs"`
}
