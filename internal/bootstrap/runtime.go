// Package bootstrap is the only place that assembles concrete implementations.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-check-in/internal/app"
	"auto-check-in/internal/browser"
	"auto-check-in/internal/buildinfo"
	"auto-check-in/internal/httpapi"
	"auto-check-in/internal/port"
	"auto-check-in/internal/provider"
	"auto-check-in/internal/security"
	"auto-check-in/internal/store"
	"auto-check-in/internal/transport"
	"auto-check-in/web"
)

type Config struct {
	NewDesktop                                                     func(dataDir string) (port.DesktopIntegration, error)
	Desktop                                                        bool
	DataDir, Listen, AccessToken, MasterKey, BrowserPath, ProxyURL string
	AllowHTTP, NoSandbox                                           bool
	AllowedOrigins                                                 []string
}

// DataDirectory is independent of the working directory, including at login.
func (c Config) DataDirectory() (string, error) {
	dir := c.DataDir
	if dir == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("无法确定程序目录，请使用 -data 指定数据目录：%w", err)
		}
		dir = filepath.Join(filepath.Dir(executable), "data")
	}
	return filepath.Abs(dir)
}

func EnvConfig() Config {
	listen := os.Getenv("CHECKIN_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:18990"
	}
	origins := []string{}
	for _, v := range strings.Split(os.Getenv("CHECKIN_ALLOWED_ORIGINS"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			origins = append(origins, v)
		}
	}
	return Config{
		DataDir: os.Getenv("CHECKIN_DATA_DIR"), Listen: listen, AccessToken: os.Getenv("CHECKIN_ACCESS_TOKEN"), MasterKey: os.Getenv("CHECKIN_MASTER_KEY"),
		BrowserPath: os.Getenv("CHECKIN_BROWSER_PATH"), ProxyURL: os.Getenv("CHECKIN_PROXY_URL"),
		AllowHTTP: os.Getenv("CHECKIN_ALLOW_HTTP") == "true", NoSandbox: os.Getenv("CHECKIN_BROWSER_NO_SANDBOX") == "true", AllowedOrigins: origins,
	}
}

type Runtime struct {
	URL       string
	Token     string
	DataDir   string
	Service   *app.Service
	browsers  *browser.Factory
	server    *http.Server
	cancel    context.CancelFunc
	closeOnce sync.Once
	release   func()
	Errors    chan error
}

func Start(config Config) (rt *Runtime, err error) {
	if config.ProxyURL != "" {
		p, e := url.Parse(config.ProxyURL)
		if e != nil || p.Host == "" || (p.Scheme != "http" && p.Scheme != "https" && p.Scheme != "socks5") || p.User != nil {
			return nil, fmt.Errorf("代理地址应为不含用户名密码的 http、https 或 socks5 地址")
		}
	}
	dir, err := config.DataDirectory()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	release, err := security.LockDirectory(dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			release()
		}
	}()
	key, err := security.LoadKey(dir, config.MasterKey)
	if err != nil {
		return nil, err
	}
	vault, err := security.NewVault(key)
	if err != nil {
		return nil, err
	}
	token, err := security.AccessToken(dir, config.AccessToken)
	if err != nil {
		return nil, err
	}
	repo, err := store.Open(filepath.Join(dir, "checkin.db"), vault)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = repo.Close()
		}
	}()
	if err = repo.Recover(context.Background(), time.Now().UTC()); err != nil {
		return nil, err
	}
	var desktopControl port.DesktopIntegration
	if config.NewDesktop != nil {
		// Storage and credentials are ready before desktop preferences are loaded.
		desktopControl, err = config.NewDesktop(dir)
		if err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return nil, fmt.Errorf("无法监听 %s：%w", config.Listen, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	browsers := browser.New(browser.Config{Executable: config.BrowserPath, TempDir: filepath.Join(dir, "temporary"), ProxyURL: config.ProxyURL, NoSandbox: config.NoSandbox, InteractiveVisible: config.Desktop})
	executor := &provider.Runner{Factory: transport.Factory{Browser: browsers, ProxyURL: config.ProxyURL}}
	service := app.New(ctx, repo, executor, browsers, config.AllowHTTP)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	rt = &Runtime{URL: "http://" + net.JoinHostPort(host, port), Token: token, DataDir: dir, Service: service, browsers: browsers, cancel: cancel, release: release, Errors: make(chan error, 1)}
	rt.server = &http.Server{Handler: httpapi.New(service, token, web.Assets(), httpapi.Config{Version: buildinfo.Version, DataDir: dir, AllowedOrigins: config.AllowedOrigins, Desktop: config.Desktop, DesktopControl: desktopControl}),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	endpoint, _ := json.Marshal(map[string]string{"url": rt.URL})
	if err = os.WriteFile(filepath.Join(dir, "endpoint.json"), endpoint, 0600); err != nil {
		cancel()
		listener.Close()
		return nil, err
	}
	service.Engine.Start(ctx)
	go func() {
		if e := rt.server.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
			rt.Errors <- e
		}
	}()
	slog.Info("Auto Check-In started", "version", buildinfo.Version, "url", rt.URL, "data_dir", dir, "browser_available", browsers.Available())
	return rt, nil
}
func (rt *Runtime) Close() {
	rt.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rt.server.Shutdown(ctx); err != nil {
			_ = rt.server.Close()
		}
		rt.cancel()
		rt.Service.CloseLogins()
		rt.Service.Engine.Wait()
		rt.browsers.Close()
		_ = rt.Service.Repo.Close()
		rt.release()
	})
}
