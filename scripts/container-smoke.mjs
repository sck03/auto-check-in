import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { execFileSync } from 'node:child_process'
const base = process.env.SMOKE_APP_URL || 'http://127.0.0.1:18990'
const token = process.env.SMOKE_ACCESS_TOKEN
if (!token) throw new Error('SMOKE_ACCESS_TOKEN is required')
let checked = false, logins = 0, checkins = 0
const fixture = createServer(async (req, res) => {
  const route = new URL(req.url || '/', 'http://fixture').pathname
  const send = (value, code = 200) => { res.writeHead(code, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)) }
  if (route === '/' || route === '/login') {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
    res.end('<!doctype html><title>Container fixture</title><form><input name="username"><input name="password" type="password"><button type="submit">登录</button></form><script>document.querySelector("form").onsubmit=async e=>{e.preventDefault();const r=await fetch("/api/user/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({username:document.querySelector("[name=username]").value,password:document.querySelector("[name=password]").value})});const p=await r.json();if(p.success)localStorage.setItem("user",JSON.stringify(p.data))}</script>')
    return
  }
  if (route === '/api/user/login') {
    let raw = ''; for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    if (body.username !== 'fixture' || body.password !== 'fixture-only-password') return send({ success: false })
    logins++; res.setHeader('Set-Cookie', 'session=fixture; Path=/; HttpOnly; SameSite=Lax')
    return send({ success: true, data: { id: 'fixture' } })
  }
  if (!req.headers.cookie?.includes('session=fixture') || req.headers['new-api-user'] !== 'fixture') return send({ success: false }, 401)
  if (route === '/api/user/self') return send({ success: true, data: { id: 'fixture', quota: checked ? 750000 : 500000 } })
  if (route === '/api/user/checkin') {
    if (req.method === 'GET') return send({ success: true, data: { stats: { checked_in_today: checked } } })
    checkins++; checked = true
    return send({ success: true, message: '签到成功', data: { quota_awarded: 250000 } })
  }
  send({ success: false }, 404)
})
await new Promise(resolve => fixture.listen(0, '0.0.0.0', resolve))
const port = fixture.address().port
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function api(route, method = 'GET', body) {
  const response = await fetch(base + '/api/v1' + route, {
    method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) throw new Error(await response.text())
  return response.status === 204 ? null : response.json()
}
try {
  let settings
  for (let i = 0; i < 60; i++) { try { settings = await api('/settings'); break } catch { await delay(1000) } }
  assert(settings, 'container HTTP service did not become ready')
  await api('/settings', 'PUT', { ...settings, scheduler_enabled: false, site_gap_seconds: 0, task_timeout_seconds: 300 })
  const templates = await api('/templates')
  const template = templates.find(t => t.id === 'newapi')
  const site = await api('/sites', 'POST', { ...template.site, id: '', name: 'Container fixture', transport: 'browser',
    base_url: 'http://' + (process.env.SMOKE_SITE_HOST || 'host.docker.internal') + ':' + port })
  const account = await api('/accounts', 'POST', { name: 'Container test', site_id: site.id, version: 0, enabled: true,
    schedule: template.schedule, credentials: { method: 'password', username: 'fixture', password: 'fixture-only-password' } })
  await api('/runs', 'POST', { account_ids: [account.id] })
  let run
  for (let i = 0; i < 75; i++) {
    const page = await api('/runs?account_id=' + account.id)
    run = page.items[0]
    if (run?.finished_at) break
    await delay(1000)
  }
  assert.equal(run?.status, 'succeeded', JSON.stringify(run))
  assert.equal(logins, 1); assert.equal(checkins, 1)
  assert.equal(run.result.reward, 250000)
  if (process.env.SMOKE_CONTAINER) {
    execFileSync('docker', ['restart', process.env.SMOKE_CONTAINER], { stdio: 'inherit' })
    let accounts
    for (let i=0;i<60;i++) { try { accounts = await api('/accounts'); break } catch { await delay(1000) } }
    assert(accounts, 'container did not become ready after restart')
    assert.equal(accounts[0].auth.has_session, true)
    const again = await api('/runs', 'POST', { account_ids: [account.id] })
    assert.equal(again[0].accepted, false, 'successful daily run was replayed after restart')
  }
  console.log('Container smoke passed: non-root Chromium login, isolated session, check-in reward, persistence and daily deduplication.')
} finally { await new Promise(resolve => fixture.close(resolve)) }
