// Package testsite provides an in-process fixture. It never contacts a real site.
package testsite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

type Site struct {
	Server       *httptest.Server
	mu           sync.Mutex
	checked      map[string]bool
	balances     map[string]int64
	Calls        map[string]int
	LoginRewards bool
	FailCount    int
}

func New() *Site {
	s := &Site{checked: map[string]bool{}, balances: map[string]int64{}, Calls: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}
func (s *Site) Close()                { s.Server.Close() }
func (s *Site) Count(name string) int { s.mu.Lock(); defer s.mu.Unlock(); return s.Calls[name] }
func (s *Site) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/login" || r.URL.Path == "/" || r.URL.Path == "/console" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	if r.URL.Path == "/api/user/login" {
		var body struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Username == "" || body.Password != "test-password" {
			write(map[string]any{"success": false, "message": "invalid password"})
			return
		}
		s.Calls["login:"+body.Username]++
		http.SetCookie(w, &http.Cookie{Name: "session", Value: body.Username, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		if _, ok := s.balances[body.Username]; !ok {
			s.balances[body.Username] = 1000
		}
		checked := false
		if s.LoginRewards && !s.checked[body.Username] {
			s.checked[body.Username] = true
			s.balances[body.Username] += 500
			checked = true
		}
		write(map[string]any{"success": true, "data": map[string]any{"id": body.Username, "quota": s.balances[body.Username], "checked_in": checked}})
		return
	}
	if r.URL.Path == "/api/user/logout" {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
		write(map[string]any{"success": true})
		return
	}
	cookie, err := r.Cookie("session")
	if err != nil || r.Header.Get("New-Api-User") != cookie.Value {
		w.WriteHeader(401)
		write(map[string]any{"success": false})
		return
	}
	user := cookie.Value
	if _, ok := s.balances[user]; !ok {
		s.balances[user] = 1000
	}
	switch r.URL.Path {
	case "/api/user/self":
		write(map[string]any{"success": true, "data": map[string]any{"id": user, "quota": s.balances[user]}})
	case "/api/user/checkin", "/api/user/sign_in":
		if r.Method == "GET" {
			write(map[string]any{"success": true, "data": map[string]any{"stats": map[string]any{"checked_in_today": s.checked[user]}}})
			return
		}
		s.Calls["checkin:"+user]++
		if s.FailCount > 0 {
			s.FailCount--
			w.WriteHeader(503)
			write(map[string]any{"success": false})
			return
		}
		if s.checked[user] {
			write(map[string]any{"success": false, "message": "今日已签到"})
			return
		}
		s.checked[user] = true
		// Include concurrent consumption: the balance delta is not the reward.
		s.balances[user] += 400
		write(map[string]any{"success": true, "message": "签到成功", "data": map[string]any{"quota_awarded": 500}})
	default:
		http.NotFound(w, r)
	}
}

const loginHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Local check-in fixture</title>
<style>body{font:18px sans-serif;margin:48px;background:#f5f8fa}input,button{font:inherit;padding:10px;margin:8px;display:block}#done[hidden]{display:none}</style>
<h1>本地测试站点</h1><form id="login"><input name="username" aria-label="用户名"><input name="password" type="password" aria-label="密码"><button type="submit">登录</button></form>
<p id="message"></p><button id="check">签到</button><p id="done" hidden>签到完成</p>
<script>
document.querySelector('#login').onsubmit=async e=>{e.preventDefault();const r=await fetch('/api/user/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:document.querySelector('[name=username]').value,password:document.querySelector('[name=password]').value})});const data=await r.json();if(data.success){localStorage.setItem('user',JSON.stringify(data.data));document.querySelector('#message').textContent='登录成功'}};
document.querySelector('#check').onclick=async()=>{const u=JSON.parse(localStorage.getItem('user')||'{}');const r=await fetch('/api/user/checkin',{method:'POST',headers:{'New-Api-User':u.id||'', 'Content-Type':'application/json'},body:'{}'});const data=await r.json();if(data.success||data.message==='今日已签到')document.querySelector('#done').hidden=false};
</script></html>`
