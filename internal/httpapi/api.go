package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"auto-check-in/internal/app"
	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type Config struct {
	DesktopControl port.DesktopIntegration
	Desktop        bool
	Version        string
	DataDir        string
	AllowedOrigins []string
}
type API struct {
	service   *app.Service
	tokenHash [32]byte
	assets    fs.FS
	config    Config
	mux       *http.ServeMux
}

func New(service *app.Service, token string, assets fs.FS, config Config) http.Handler {
	a := &API{service: service, tokenHash: sha256.Sum256([]byte(token)), assets: assets, config: config, mux: http.NewServeMux()}
	a.routes()
	return a
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	defer func() {
		if recover() != nil {
			slog.Error("HTTP handler panic")
			writeError(w, 500, "internal", "服务暂时无法处理请求")
		}
	}()
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !a.originAllowed(origin, r.Host) {
				writeError(w, 403, "origin", "不允许此页面来源")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || len(header) > 600 {
			writeError(w, 401, "unauthorized", "请填写正确的管理访问令牌")
			return
		}
		got := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
		if subtle.ConstantTimeCompare(got[:], a.tokenHash[:]) != 1 {
			writeError(w, 401, "unauthorized", "请填写正确的管理访问令牌")
			return
		}
	}
	a.mux.ServeHTTP(w, r)
}
func (a *API) originAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == host && u.Path == "" {
		return true
	}
	for _, allowed := range a.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func readJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	if media := strings.Split(r.Header.Get("Content-Type"), ";")[0]; strings.TrimSpace(media) != "application/json" {
		writeError(w, 415, "content_type", "请求须使用 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, 400, "invalid_json", "JSON 格式或字段无效，或请求超过大小限制")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, 400, "invalid_json", "只允许一个 JSON 对象")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, 404, "not_found", err.Error())
	case errors.Is(err, domain.ErrBusy), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrCooldown), errors.Is(err, domain.ErrDisabled), errors.Is(err, domain.ErrDailyLimit):
		writeError(w, 409, "conflict", err.Error())
	case errors.Is(err, port.ErrBrowserUnavailable), errors.Is(err, port.ErrBrowserBusy):
		writeError(w, 503, "browser_unavailable", err.Error())
	default:
		writeError(w, 400, "operation_failed", err.Error())
	}
}

func (a *API) routes() {
	m := a.mux
	s := a.service
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok", "version": a.config.Version})
	})
	m.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"version": a.config.Version, "go_version": runtime.Version(), "server_time": time.Now().UTC(), "browser_available": s.Browser.Available(), "allow_http": s.AllowHTTP, "desktop": a.config.Desktop, "data_dir": a.config.DataDir})
	})
	m.HandleFunc("GET /api/v1/templates", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, domain.Templates()) })
	m.HandleFunc("GET /api/v1/desktop", func(w http.ResponseWriter, r *http.Request) {
		if a.config.DesktopControl == nil {
			writeError(w, 404, "desktop_only", "仅 Windows 桌面模式提供此功能")
			return
		}
		respond(w, 200, a.config.DesktopControl.Status())
	})
	m.HandleFunc("PUT /api/v1/desktop", func(w http.ResponseWriter, r *http.Request) {
		if a.config.DesktopControl == nil {
			writeError(w, 404, "desktop_only", "仅 Windows 桌面模式提供此功能")
			return
		}
		var p domain.DesktopPreferences
		if !readJSON(w, r, &p, 1<<16) {
			return
		}
		status, err := a.config.DesktopControl.Configure(p)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, status)
	})
	m.HandleFunc("POST /api/v1/desktop/action", func(w http.ResponseWriter, r *http.Request) {
		if a.config.DesktopControl == nil {
			writeError(w, 404, "desktop_only", "仅 Windows 桌面模式提供此功能")
			return
		}
		var body struct {
			Action string `json:"action"`
		}
		if !readJSON(w, r, &body, 1<<12) {
			return
		}
		if err := a.config.DesktopControl.Action(body.Action); err != nil {
			fail(w, err)
			return
		}
		respond(w, 202, map[string]bool{"accepted": true})
	})
	m.HandleFunc("GET /api/v1/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		data := map[string]any{"go_heap_bytes": memory.HeapAlloc, "goroutines": runtime.NumGoroutine()}
		if reporter, ok := s.Browser.(interface{ Diagnostics() any }); ok {
			data["browser_pool"] = reporter.Diagnostics()
		}
		respond(w, 200, data)
	})
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Repo.Sites(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	})
	m.HandleFunc("POST /api/v1/sites", func(w http.ResponseWriter, r *http.Request) { a.saveSite(w, r, "") })
	m.HandleFunc("PUT /api/v1/sites/{id}", func(w http.ResponseWriter, r *http.Request) { a.saveSite(w, r, r.PathValue("id")) })
	m.HandleFunc("DELETE /api/v1/sites/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Repo.DeleteSite(r.Context(), r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("GET /api/v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Repo.Accounts(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	})
	m.HandleFunc("POST /api/v1/accounts", func(w http.ResponseWriter, r *http.Request) { a.saveAccount(w, r, "") })
	m.HandleFunc("PUT /api/v1/accounts/{id}", func(w http.ResponseWriter, r *http.Request) { a.saveAccount(w, r, r.PathValue("id")) })
	m.HandleFunc("DELETE /api/v1/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Repo.DeleteAccount(r.Context(), r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		s.Engine.Notify()
		w.WriteHeader(204)
	})
	m.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Repo.Settings(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	})
	m.HandleFunc("PUT /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var v domain.Settings
		if !readJSON(w, r, &v, 1<<16) {
			return
		}
		if e := v.Validate(); e != nil {
			fail(w, e)
			return
		}
		v, e := s.Repo.SaveSettings(r.Context(), v)
		if e != nil {
			fail(w, e)
			return
		}
		s.Engine.Notify()
		respond(w, 200, v)
	})
	m.HandleFunc("POST /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs   []string `json:"account_ids"`
			Retry bool     `json:"retry"`
		}
		if !readJSON(w, r, &body, 1<<20) {
			return
		}
		result, e := s.RunAccounts(r.Context(), body.IDs, body.Retry)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 202, result)
	})
	m.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		result, e := s.Repo.Runs(r.Context(), domain.RunFilter{AccountID: q.Get("account_id"), Status: q.Get("status"), Action: q.Get("action"), Limit: limit, Offset: offset})
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, result)
	})
	m.HandleFunc("DELETE /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Repo.CancelQueued(r.Context(), r.PathValue("id"), time.Now().UTC()); e != nil {
			fail(w, e)
			return
		}
		s.Engine.Notify()
		w.WriteHeader(204)
	})
	m.HandleFunc("POST /api/v1/accounts/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		info, e := s.StartLogin(r.Context(), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 201, info)
	})
	m.HandleFunc("GET /api/v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		info, e := s.LoginInfo(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, info)
	})
	m.HandleFunc("GET /api/v1/sessions/{id}/screenshot", func(w http.ResponseWriter, r *http.Request) {
		image, e := s.Screenshot(r.Context(), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	})
	m.HandleFunc("POST /api/v1/sessions/{id}/input", func(w http.ResponseWriter, r *http.Request) {
		var event port.Interaction
		if !readJSON(w, r, &event, 1<<16) {
			return
		}
		if e := s.Interact(r.Context(), r.PathValue("id"), event); e != nil {
			fail(w, e)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("POST /api/v1/sessions/{id}/save", func(w http.ResponseWriter, r *http.Request) {
		if e := s.SaveLogin(r.Context(), r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("DELETE /api/v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.CancelLogin(r.PathValue("id")); e != nil {
			fail(w, e)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("POST /api/v1/backup/export", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if !readJSON(w, r, &body, 1<<16) {
			return
		}
		archive, e := s.Export(r.Context(), body.Password)
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="auto-check-in.backup"`)
		_, _ = w.Write(archive)
	})
	m.HandleFunc("POST /api/v1/backup/restore", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
			Archive  []byte `json:"archive"`
		}
		if !readJSON(w, r, &body, 64<<20) {
			return
		}
		if e := s.Restore(r.Context(), body.Archive, body.Password); e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, map[string]bool{"restored": true})
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "API 路径不存在") })
	m.HandleFunc("/", a.static)
}
func (a *API) saveSite(w http.ResponseWriter, r *http.Request, id string) {
	var site domain.Site
	if !readJSON(w, r, &site, 1<<16) {
		return
	}
	v, e := a.service.SaveSite(r.Context(), id, site)
	if e != nil {
		fail(w, e)
		return
	}
	status := 200
	if id == "" {
		status = 201
	}
	respond(w, status, v)
}
func (a *API) saveAccount(w http.ResponseWriter, r *http.Request, id string) {
	var input domain.AccountInput
	if !readJSON(w, r, &input, 1<<20) {
		return
	}
	v, e := a.service.SaveAccount(r.Context(), id, input)
	if e != nil {
		fail(w, e)
		return
	}
	status := 200
	if id == "" {
		status = 201
	}
	respond(w, status, v)
}
func (a *API) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(a.assets, name); err != nil {
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}
	http.FileServer(http.FS(a.assets)).ServeHTTP(w, r)
}
