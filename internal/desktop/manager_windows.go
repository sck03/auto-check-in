//go:build windows

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-check-in/internal/app"
	"auto-check-in/internal/domain"
	"github.com/energye/systray"
)

type Manager struct {
	mu                                           sync.Mutex
	prefs                                        domain.DesktopPreferences
	startup                                      Startup
	dataDir, baseURL, token, browserPath         string
	service                                      *app.Service
	ready, windowOpen, quitting                  bool
	lastError                                    string
	windowPID                                    int
	windowCancel                                 context.CancelFunc
	ctx                                          context.Context
	cancel                                       context.CancelFunc
	commands                                     chan func()
	wg                                           sync.WaitGroup
	startHidden                                  bool
	pauseMenu, autoMenu, minimizeMenu, closeMenu *systray.MenuItem
}

func NewManager(dataDir string, startHidden bool) (*Manager, error) {
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{dataDir: dir, startup: NewStartup(executable, dir), ctx: ctx, cancel: cancel, commands: make(chan func(), 32), startHidden: startHidden,
		prefs: domain.DesktopPreferences{MinimizeToTray: true, CloseToTray: true, Version: 1}}
	raw, err := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if err == nil {
		if err = json.Unmarshal(raw, &m.prefs); err != nil {
			return nil, fmt.Errorf("desktop.json 无效：%w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	m.prefs.AutoStart = m.startup.Enabled()
	return m, nil
}
func (m *Manager) Bind(url, token, browserPath string, service *app.Service) {
	m.baseURL = url
	m.token = token
	m.browserPath = browserPath
	m.service = service
}
func (m *Manager) Status() domain.DesktopStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefs := m.prefs
	prefs.AutoStart = m.startup.Enabled()
	return domain.DesktopStatus{Preferences: prefs, TrayReady: m.ready, WindowOpen: m.windowOpen, LastError: m.lastError, Menu: []domain.TrayMenuItem{
		{Action: "show", Label: "显示主窗口"}, {Action: "run_all", Label: "立即签到全部"}, {Action: "pause", Label: "暂停 / 恢复自动计划"},
		{Action: "autostart", Label: "登录 Windows 后启动", Checked: prefs.AutoStart},
		{Action: "minimize_to_tray", Label: "最小化到托盘", Checked: prefs.MinimizeToTray},
		{Action: "close_to_tray", Label: "关闭窗口后继续运行", Checked: prefs.CloseToTray},
		{Action: "open_data", Label: "打开数据目录"}, {Action: "quit", Label: "退出签到台"},
	}}
}
func (m *Manager) Configure(p domain.DesktopPreferences) (domain.DesktopStatus, error) {
	m.mu.Lock()
	if p.Version != m.prefs.Version {
		m.mu.Unlock()
		return domain.DesktopStatus{}, domain.ErrConflict
	}
	before := m.startup.Enabled()
	if before != p.AutoStart {
		if err := m.startup.Set(p.AutoStart); err != nil {
			m.mu.Unlock()
			return domain.DesktopStatus{}, fmt.Errorf("无法更新当前用户的开机运行设置：%w", err)
		}
	}
	p.Version++
	raw, _ := json.MarshalIndent(p, "", "  ")
	temp := filepath.Join(m.dataDir, "desktop.json.tmp")
	err := os.WriteFile(temp, raw, 0600)
	if err == nil {
		err = os.Rename(temp, filepath.Join(m.dataDir, "desktop.json"))
	}
	if err != nil {
		_ = m.startup.Set(before)
		m.mu.Unlock()
		return domain.DesktopStatus{}, err
	}
	m.prefs = p
	m.mu.Unlock()
	m.enqueue(m.syncMenus)
	return m.Status(), nil
}
func (m *Manager) enqueue(fn func()) bool {
	select {
	case <-m.ctx.Done():
		return false
	case m.commands <- fn:
		return true
	default:
		return false
	}
}
func (m *Manager) Action(action string) error {
	var fn func()
	switch action {
	case "show":
		fn = m.show
	case "hide":
		fn = m.hide
	case "minimize":
		fn = func() { m.mu.Lock(); pid := m.windowPID; m.mu.Unlock(); minimizeWindow(findWindow(pid)) }
	case "quit":
		fn = m.quit
	case "open_data":
		fn = func() { _ = exec.Command("explorer.exe", m.dataDir).Start() }
	case "run_all":
		fn = func() {
			if _, err := m.service.RunAccounts(m.ctx, nil, false); err != nil {
				m.report(err)
			}
		}
	case "pause":
		fn = func() {
			settings, err := m.service.Repo.Settings(m.ctx)
			if err == nil {
				settings.SchedulerEnabled = !settings.SchedulerEnabled
				_, err = m.service.Repo.SaveSettings(m.ctx, settings)
				m.service.Engine.Notify()
			}
			if err != nil {
				m.report(err)
			}
			m.syncMenus()
		}
	default:
		return fmt.Errorf("不支持的桌面操作")
	}
	if !m.enqueue(fn) {
		return fmt.Errorf("桌面程序正在退出")
	}
	return nil
}
func (m *Manager) Run(icon []byte) {
	systray.Run(func() {
		systray.SetIcon(icon)
		systray.SetTitle("签到台")
		systray.SetTooltip("Auto Check-In · 每天成功一次")
		systray.SetOnClick(func(systray.IMenu) { _ = m.Action("show") })
		systray.SetOnDClick(func(systray.IMenu) { _ = m.Action("show") })
		systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
		for _, item := range m.Status().Menu {
			entry := item
			if entry.Action == "autostart" || entry.Action == "open_data" {
				systray.AddSeparator()
			}
			menu := systray.AddMenuItemCheckbox(entry.Label, entry.Label, entry.Checked)
			switch entry.Action {
			case "pause":
				m.pauseMenu = menu
			case "autostart":
				m.autoMenu = menu
			case "minimize_to_tray":
				m.minimizeMenu = menu
			case "close_to_tray":
				m.closeMenu = menu
			}
			menu.Click(func() {
				switch entry.Action {
				case "autostart", "minimize_to_tray", "close_to_tray":
					m.enqueue(func() {
						p := m.Status().Preferences
						switch entry.Action {
						case "autostart":
							p.AutoStart = !p.AutoStart
						case "minimize_to_tray":
							p.MinimizeToTray = !p.MinimizeToTray
						case "close_to_tray":
							p.CloseToTray = !p.CloseToTray
						}
						if _, err := m.Configure(p); err != nil {
							m.report(err)
						}
					})
				default:
					_ = m.Action(entry.Action)
				}
			})
		}
		m.mu.Lock()
		m.ready = true
		m.mu.Unlock()
		m.wg.Add(1)
		go func() { defer m.wg.Done(); m.loop() }()
		m.enqueue(m.syncMenus)
		if !m.startHidden {
			_ = m.Action("show")
		}
	}, func() {
		m.cancel()
		m.mu.Lock()
		if m.windowCancel != nil {
			m.windowCancel()
		}
		m.mu.Unlock()
	})
	m.wg.Wait()
}
func (m *Manager) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-m.ctx.Done():
			return
		case fn := <-m.commands:
			fn()
		case <-ticker.C:
			m.mu.Lock()
			pid, enabled, open := m.windowPID, m.prefs.MinimizeToTray, m.windowOpen
			m.mu.Unlock()
			if open && enabled && minimized(findWindow(pid)) {
				m.hide()
			}
			ticks++
			if ticks%5 == 0 {
				m.syncMenus()
			}
		}
	}
}
func (m *Manager) show() {
	m.mu.Lock()
	if m.windowOpen {
		pid := m.windowPID
		m.mu.Unlock()
		restoreWindow(findWindow(pid))
		return
	}
	if m.quitting {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.windowCancel = cancel
	m.windowOpen = true
	m.lastError = ""
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := Run(ctx, m.baseURL, m.token, m.dataDir, m.browserPath, func(pid int) { m.mu.Lock(); m.windowPID = pid; m.mu.Unlock() })
		intentional := ctx.Err() != nil
		m.enqueue(func() {
			m.mu.Lock()
			m.windowOpen = false
			m.windowPID = 0
			m.windowCancel = nil
			closeToTray := m.prefs.CloseToTray
			m.mu.Unlock()
			if err != nil && ctx.Err() == nil {
				m.report(fmt.Errorf("窗口启动失败：%w", err))
			}
			if !intentional && !closeToTray {
				m.quit()
			}
		})
	}()
}
func (m *Manager) hide() {
	m.mu.Lock()
	cancel := m.windowCancel
	m.mu.Unlock()
	// Release the whole display browser. Restoring creates a fresh window while
	// accounts, jobs and the authenticated HTTP service stay in the Go process.
	if cancel != nil {
		cancel()
	}
}
func (m *Manager) quit() { m.mu.Lock(); m.quitting = true; m.mu.Unlock(); m.cancel(); systray.Quit() }
func (m *Manager) report(err error) {
	message := strings.ReplaceAll(err.Error(), m.token, "[已隐藏]")
	m.mu.Lock()
	m.lastError = message
	m.mu.Unlock()
	slog.Error("desktop operation failed", "error", message)
}
func (m *Manager) syncMenus() {
	status := m.Status()
	for item, checked := range map[*systray.MenuItem]bool{m.autoMenu: status.Preferences.AutoStart, m.minimizeMenu: status.Preferences.MinimizeToTray, m.closeMenu: status.Preferences.CloseToTray} {
		if item == nil {
			continue
		}
		if checked && !item.Checked() {
			item.Check()
		} else if !checked && item.Checked() {
			item.Uncheck()
		}
	}
	if m.pauseMenu != nil && m.service != nil {
		if settings, err := m.service.Repo.Settings(m.ctx); err == nil {
			title := "恢复自动计划"
			if settings.SchedulerEnabled {
				title = "暂停自动计划"
			}
			m.pauseMenu.SetTitle(title)
		}
	}
}
