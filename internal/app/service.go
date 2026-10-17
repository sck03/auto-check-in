package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/security"
)

type Service struct {
	Repo        port.Repository
	Engine      *Engine
	Browser     port.BrowserFactory
	AllowHTTP   bool
	ctx         context.Context
	loginCancel context.CancelFunc
	closing     bool
	mu          sync.Mutex
	logins      map[string]*loginSession
	wg          sync.WaitGroup
}

func New(ctx context.Context, repo port.Repository, executor port.Executor, browser port.BrowserFactory, allowHTTP bool) *Service {
	loginCtx, loginCancel := context.WithCancel(ctx)
	return &Service{Repo: repo, Engine: NewEngine(repo, executor), Browser: browser, AllowHTTP: allowHTTP, ctx: loginCtx, loginCancel: loginCancel, logins: map[string]*loginSession{}}
}
func (s *Service) SaveSite(ctx context.Context, id string, site domain.Site) (domain.Site, error) {
	if err := site.Validate(s.AllowHTTP); err != nil {
		return site, err
	}
	if id == "" {
		site.ID = domain.NewID()
		site.Version = 0
	} else {
		site.ID = id
	}
	return s.Repo.SaveSite(ctx, site)
}
func (s *Service) SaveAccount(ctx context.Context, id string, input domain.AccountInput) (domain.Account, error) {
	var a domain.Account
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 {
		return a, fmt.Errorf("账号名称应为 1 至 120 个字节")
	}
	if err := input.Schedule.Validate(); err != nil {
		return a, err
	}
	site, err := s.Repo.Site(ctx, input.SiteID)
	if err != nil {
		return a, err
	}
	c := domain.Credentials{Method: "cookie"}
	if id != "" {
		a, err = s.Repo.Account(ctx, id)
		if err != nil {
			return a, err
		}
		if a.SiteID != input.SiteID {
			return a, fmt.Errorf("%w：请为其他站点新建账号", domain.ErrConflict)
		}
		c, err = s.Repo.Credentials(ctx, id)
		if err != nil {
			return a, err
		}
	} else {
		id = domain.NewID()
	}
	c = c.Apply(input.Credentials)
	if err = c.Validate(site); err != nil {
		return a, err
	}
	a.ID = id
	a.SiteID = input.SiteID
	a.Name = input.Name
	a.Enabled = input.Enabled
	a.Schedule = input.Schedule
	a.Version = input.Version
	saved, err := s.Repo.SaveAccount(ctx, a, c, time.Now().UTC())
	if err == nil {
		s.Engine.Notify()
	}
	return saved, err
}

type EnqueueResult struct {
	AccountID string `json:"account_id"`
	RunID     string `json:"run_id,omitempty"`
	Message   string `json:"message"`
	Accepted  bool   `json:"accepted"`
}

func (s *Service) RunAccounts(ctx context.Context, ids []string, retry bool) ([]EnqueueResult, error) {
	if retry && len(ids) != 1 {
		return nil, fmt.Errorf("手动重试须逐个账号确认")
	}
	if len(ids) == 0 {
		accounts, err := s.Repo.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range accounts {
			if a.Enabled {
				ids = append(ids, a.ID)
			}
		}
	}
	if len(ids) > 10000 {
		return nil, fmt.Errorf("单次最多执行 10000 个账号")
	}
	results := []EnqueueResult{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		trigger := "manual"
		if retry {
			trigger = "manual_retry"
		}
		r, err := s.Repo.Enqueue(ctx, id, trigger, time.Now().UTC())
		if err != nil {
			results = append(results, EnqueueResult{AccountID: id, Message: err.Error()})
		} else {
			results = append(results, EnqueueResult{AccountID: id, RunID: r.ID, Message: "已加入执行队列", Accepted: true})
		}
	}
	s.Engine.Notify()
	return results, nil
}
func (s *Service) Export(ctx context.Context, password string) ([]byte, error) {
	b, err := s.Repo.Export(ctx)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return security.EncryptBackup(plain, password)
}
func (s *Service) Restore(ctx context.Context, archive []byte, password string) error {
	plain, err := security.DecryptBackup(archive, password)
	if err != nil {
		return err
	}
	var b domain.Backup
	if err = json.Unmarshal(plain, &b); err != nil {
		return fmt.Errorf("备份内容无效")
	}
	if err = s.validateBackup(&b); err != nil {
		return err
	}
	if err = s.Repo.Restore(ctx, b); err == nil {
		s.Engine.Notify()
	}
	return err
}
func (s *Service) validateBackup(b *domain.Backup) error {
	if b.Version != 1 || len(b.Sites) > 1000 || len(b.Accounts) > 10000 || len(b.Runs) > 100000 {
		return fmt.Errorf("备份版本或记录数量不受支持")
	}
	if err := b.Settings.Validate(); err != nil {
		return err
	}
	sites := map[string]domain.Site{}
	for i := range b.Sites {
		site := &b.Sites[i]
		if site.ID == "" || len(site.ID) > 64 || sites[site.ID].ID != "" {
			return fmt.Errorf("备份站点 ID 无效或重复")
		}
		if err := site.Validate(s.AllowHTTP); err != nil {
			return err
		}
		if site.Version < 1 {
			site.Version = 1
		}
		sites[site.ID] = *site
	}
	accounts := map[string]bool{}
	for i := range b.Accounts {
		entry := &b.Accounts[i]
		a := &entry.Account
		site, ok := sites[a.SiteID]
		if !ok || a.ID == "" || len(a.ID) > 64 || accounts[a.ID] || a.Name == "" || len(a.Name) > 120 {
			return fmt.Errorf("备份账号引用、名称或 ID 无效")
		}
		if a.Version < 1 {
			a.Version = 1
		}
		if err := a.Schedule.Validate(); err != nil {
			return err
		}
		if err := entry.Credentials.Validate(site); err != nil {
			return err
		}
		if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
			return fmt.Errorf("备份账号时间无效")
		}
		a.LastMessage = security.Redact(a.LastMessage, entry.Credentials)
		accounts[a.ID] = true
	}
	runs := map[string]bool{}
	for _, r := range b.Runs {
		if r.ID == "" || len(r.ID) > 64 || runs[r.ID] || (r.AccountID != "" && !accounts[r.AccountID]) {
			return fmt.Errorf("备份记录引用无效")
		}
		if r.Action != "checkin" && r.Action != "login" {
			return fmt.Errorf("备份记录动作无效")
		}
		switch r.Status {
		case domain.Succeeded, domain.AlreadyDone, domain.Failed, domain.NeedsAttention, domain.Unverified, domain.Interrupted, domain.Cancelled:
		default:
			return fmt.Errorf("备份中包含未结束的任务")
		}
		if r.FinishedAt == nil || r.CreatedAt.IsZero() || len(r.Result.Message) > 4000 {
			return fmt.Errorf("备份记录内容无效")
		}
		runs[r.ID] = true
	}
	return nil
}
