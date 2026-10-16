package browser

import (
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type sessionTab struct {
	ctx    context.Context
	cancel context.CancelFunc
	ready  bool
}

func (s *Session) activeContext() context.Context {
	s.tabMu.RLock()
	defer s.tabMu.RUnlock()
	if tab := s.tabs[s.activeTab]; tab != nil && tab.ready {
		return tab.ctx
	}
	return s.ctx
}

// API calls and saved local storage belong to the configured site, even if a
// user has left an identity-provider tab open after completing authorization.
func (s *Session) siteContext(ctx context.Context) context.Context {
	owner := chromedp.FromContext(s.ctx)
	result, err := cdp.Call(ctx, owner.Browser, target.GetTargets, target.GetTargetsParams{})
	if err != nil {
		return s.activeContext()
	}
	base, _ := url.Parse(s.site.BaseURL)
	siteTabs := make(map[target.ID]bool)
	for _, info := range result.TargetInfos {
		location, err := url.Parse(info.URL)
		if err == nil && (s.nativeWindow || info.BrowserContextID == owner.BrowserContextID) &&
			location.Scheme == base.Scheme && location.Host == base.Host {
			siteTabs[info.TargetID] = true
		}
	}
	s.tabMu.RLock()
	defer s.tabMu.RUnlock()
	for i := len(s.tabOrder) - 1; i >= 0; i-- {
		id := s.tabOrder[i]
		if tab := s.tabs[id]; siteTabs[id] && tab != nil && tab.ready {
			return tab.ctx
		}
	}
	return s.ctx
}

func (s *Session) runSite(ctx context.Context, actions ...cdpActionFunc) error {
	return s.runOn(s.siteContext(ctx), ctx, actions...)
}

// Every tracked tab belongs to this account's browser context. In particular,
// popups from another account sharing the background process are never attached.
func (s *Session) trackTabs() {
	id := chromedp.FromContext(s.ctx).Target.TargetID
	s.tabs = map[target.ID]*sessionTab{id: {ctx: s.ctx, ready: true}}
	s.tabOrder = []target.ID{id}
	s.activeTab = id
	created := chromedp.BrowserEvents(s.ctx, target.TargetCreated)
	changed := chromedp.BrowserEvents(s.ctx, target.TargetInfoChanged)
	destroyed := chromedp.BrowserEvents(s.ctx, target.TargetDestroyed)
	_, _ = cdp.Call(s.ctx, chromedp.FromContext(s.ctx).Browser, target.SetDiscoverTargets, target.SetDiscoverTargetsParams{Discover: true})
	go func() {
		for event, err := range created {
			if err != nil {
				return
			}
			s.attachTab(event.TargetInfo)
		}
	}()
	go func() {
		for event, err := range changed {
			if err != nil {
				return
			}
			s.attachTab(event.TargetInfo)
		}
	}()
	go func() {
		for event, err := range destroyed {
			if err != nil {
				return
			}
			s.removeTab(event.TargetID)
		}
	}()
}

func (s *Session) attachTab(info *target.Info) {
	if info.Type != "page" || (!s.nativeWindow && info.BrowserContextID != chromedp.FromContext(s.ctx).BrowserContextID) {
		return
	}
	s.tabMu.Lock()
	if _, exists := s.tabs[info.TargetID]; exists {
		s.tabMu.Unlock()
		return
	}
	ctx, cancel := chromedp.NewContext(s.ctx, chromedp.WithTargetID(info.TargetID), chromedp.WithDetachOnCancel())
	tab := &sessionTab{ctx: ctx, cancel: cancel}
	s.tabs[info.TargetID] = tab
	s.tabMu.Unlock()
	timer := time.AfterFunc(8*time.Second, cancel)
	defer timer.Stop()
	err := chromedp.Do(ctx)
	if err == nil {
		_, err = cdp.Call(ctx, chromedp.FromContext(ctx).Target, network.Enable, network.EnableParams{})
	}
	if err == nil && s.creds.UserAgent != "" && !s.nativeWindow {
		_, err = cdp.Call(ctx, chromedp.FromContext(ctx).Target, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{UserAgent: s.creds.UserAgent})
	}
	if err != nil {
		s.removeTab(info.TargetID)
		return
	}
	s.listenLoginResponses(ctx)
	s.tabMu.Lock()
	if s.tabs[info.TargetID] == tab {
		tab.ready = true
		s.tabOrder = append(s.tabOrder, info.TargetID)
		s.activeTab = info.TargetID
	}
	s.tabMu.Unlock()
}

func (s *Session) removeTab(id target.ID) {
	s.tabMu.Lock()
	tab := s.tabs[id]
	delete(s.tabs, id)
	s.tabOrder = slices.DeleteFunc(s.tabOrder, func(candidate target.ID) bool { return candidate == id })
	if s.activeTab == id {
		for i := len(s.tabOrder) - 1; i >= 0; i-- {
			candidate := s.tabOrder[i]
			if current := s.tabs[candidate]; current != nil && current.ready {
				s.activeTab = candidate
				break
			}
		}
	}
	s.tabMu.Unlock()
	if tab != nil && tab.cancel != nil {
		tab.cancel()
	}
}
