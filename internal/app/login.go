package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
)

type LoginInfo struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
}
type loginSession struct {
	mu      sync.Mutex
	info    LoginInfo
	session port.InteractiveSession
	site    domain.Site
	cancel  context.CancelFunc
	closed  bool
}

func (s *Service) StartLogin(ctx context.Context, accountID string) (LoginInfo, error) {
	var info LoginInfo
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return info, fmt.Errorf("程序正在退出")
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	if !s.Browser.Available() {
		return info, port.ErrBrowserUnavailable
	}
	a, err := s.Repo.Account(ctx, accountID)
	if err != nil {
		return info, err
	}
	site, err := s.Repo.Site(ctx, a.SiteID)
	if err != nil {
		return info, err
	}
	c, err := s.Repo.Credentials(ctx, a.ID)
	if err != nil {
		return info, err
	}
	run, err := s.Repo.BeginLogin(ctx, accountID, time.Now().UTC())
	if err != nil {
		return info, err
	}
	expires := time.Now().Add(10 * time.Minute).UTC()
	life, cancel := context.WithDeadline(s.ctx, expires)
	session, err := s.Browser.OpenInteractive(life, site, c)
	if err != nil {
		cancel()
		finishCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = s.Repo.FinishLogin(finishCtx, run.ID, nil, domain.Failed, "浏览器启动失败，请检查浏览器路径与运行环境", time.Now().UTC())
		if errors.Is(err, port.ErrBrowserBusy) {
			return info, err
		}
		return info, fmt.Errorf("浏览器启动失败，请检查浏览器路径或网络")
	}
	info = LoginInfo{ID: run.ID, AccountID: accountID, ExpiresAt: expires, Width: 1200, Height: 800}
	entry := &loginSession{info: info, session: session, site: site, cancel: cancel}
	s.mu.Lock()
	s.logins[run.ID] = entry
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-life.Done()
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if !entry.closed {
			_ = s.closeLogin(entry, nil, domain.Cancelled, "人工登录会话已结束或过期")
		}
	}()
	return info, nil
}
func (s *Service) login(id string) (*loginSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.logins[id]
	if entry == nil {
		return nil, domain.ErrNotFound
	}
	return entry, nil
}
func (s *Service) LoginInfo(id string) (LoginInfo, error) {
	entry, err := s.login(id)
	if err != nil {
		return LoginInfo{}, err
	}
	return entry.info, nil
}
func (s *Service) Screenshot(ctx context.Context, id string) ([]byte, error) {
	entry, err := s.login(id)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return nil, domain.ErrNotFound
	}
	return entry.session.Screenshot(ctx)
}
func (s *Service) Interact(ctx context.Context, id string, event port.Interaction) error {
	entry, err := s.login(id)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return domain.ErrNotFound
	}
	return entry.session.Interact(ctx, event)
}
func (s *Service) SaveLogin(ctx context.Context, id string) error {
	entry, err := s.login(id)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return domain.ErrNotFound
	}
	if entry.site.Adapter != "browser" {
		response, err := entry.session.Request(ctx, "GET", entry.site.UserInfoPath, nil)
		if err != nil {
			return fmt.Errorf("请先在站点完成登录并返回站点页面")
		}
		payload, err := provider.Decode(response.Body)
		if err != nil || response.Status < 200 || response.Status >= 300 || payload["success"] != true {
			return fmt.Errorf("登录尚未通过站点验证，请完成登录后再保存")
		}
	}
	c, err := entry.session.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("无法读取浏览器会话，请重试")
	}
	if len(c.Cookies) == 0 && c.Token == "" && len(c.Storage[entry.site.BaseURL]) == 0 {
		return fmt.Errorf("没有可保存的登录会话")
	}
	if err = c.Validate(entry.site); err != nil {
		return err
	}
	return s.closeLogin(entry, &c, domain.Succeeded, "登录会话已保存")
}
func (s *Service) CancelLogin(id string) error {
	entry, err := s.login(id)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return domain.ErrNotFound
	}
	return s.closeLogin(entry, nil, domain.Cancelled, "人工登录已取消")
}

// entry.mu must be held. Persist first so a transient storage error can be
// retried without losing the interactive session.
func (s *Service) closeLogin(entry *loginSession, c *domain.Credentials, status, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.Repo.FinishLogin(ctx, entry.info.ID, c, status, message, time.Now().UTC())
	if err != nil && c != nil {
		return err
	}
	entry.closed = true
	entry.cancel()
	entry.session.Close()
	s.mu.Lock()
	delete(s.logins, entry.info.ID)
	s.mu.Unlock()
	return err
}
func (s *Service) CloseLogins() {
	s.mu.Lock()
	s.closing = true
	s.loginCancel()
	list := make([]*loginSession, 0, len(s.logins))
	for _, entry := range s.logins {
		list = append(list, entry)
	}
	s.mu.Unlock()
	for _, entry := range list {
		entry.mu.Lock()
		if !entry.closed {
			_ = s.closeLogin(entry, nil, domain.Cancelled, "程序已退出")
		}
		entry.mu.Unlock()
	}
	s.wg.Wait()
}
