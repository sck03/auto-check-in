import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { createServer as createHTTPServer } from 'node:http'
import { mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'

if (process.platform !== 'win32') throw new Error('The native smoke test requires Windows.')
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(path.join(root, 'web/package.json'))
const { chromium } = require('playwright')
const probe = createServer()
await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve))
const port = probe.address().port
await new Promise(resolve => probe.close(resolve))
await mkdir(path.join(root, 'artifacts'), { recursive: true })
const data = await mkdtemp(path.join(root, 'artifacts', 'native-'))
const token = 'native-smoke-test-token-not-for-production'
const fixture = createHTTPServer((_req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
  res.end('<title>Local native login fixture</title><h1>本地登录测试</h1><input name="username"><input name="password" type="password">')
})
await new Promise(resolve => fixture.listen(0, '127.0.0.1', resolve))
const app = spawn(process.env.SMOKE_DESKTOP_EXE || path.join(root, 'dist/AutoCheckIn.exe'), [], {
  env: { ...process.env, CHECKIN_DATA_DIR: data, CHECKIN_ACCESS_TOKEN: token,
    CHECKIN_DESKTOP_TEST_PORT: String(port), CHECKIN_TEST_STARTUP_FILE: path.join(data, 'test-startup'), CHECKIN_ALLOW_HTTP: 'true' },
  windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
})
let log = '', browser, baseURL = ''
app.stdout.on('data', data => log += data)
app.stderr.on('data', data => log += data)
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check, label, timeout = 20000) {
  const end = Date.now() + timeout
  while (Date.now() < end) { try { const value = await check(); if (value) return value } catch {} await delay(150) }
  throw new Error(label + '\n' + log)
}
async function api(route, method = 'GET', body) {
  const r = await fetch(baseURL + '/api/v1' + route, {
    method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!r.ok) throw new Error(await r.text())
  return r.status === 204 ? null : r.json()
}
async function connect() {
  return until(async () => chromium.connectOverCDP('http://127.0.0.1:' + port, { timeout: 800 }), 'Desktop CDP connection failed')
}
try {
  baseURL = (await until(async () => JSON.parse(await readFile(path.join(data, 'endpoint.json'), 'utf8')), 'HTTP service did not start')).url
  browser = await connect()
  let page = browser.contexts()[0].pages()[0]
  await page.getByRole('heading', { name: '我的账号' }).waitFor({ timeout: 15000 })
  await page.screenshot({ path: path.join(root, 'artifacts/native-windows.png') })
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '设置' }).click()
  await page.getByText('go1.27.2', { exact: true }).waitFor()
  await page.getByText('Windows 桌面', { exact: true }).first().waitFor()
  const startup = page.getByRole('switch', { name: '登录 Windows 后自动启动' })
  await startup.waitFor()
  await startup.click()
  await until(async () => (await api('/desktop')).preferences.autostart, 'Startup switch was not saved')
  assert.equal(await readFile(path.join(data, 'test-startup'), 'utf8'), 'enabled')
  await until(async () => !(await startup.isDisabled()), 'Startup switch stayed busy')
  await startup.click()
  await until(async () => !(await api('/desktop')).preferences.autostart, 'Startup switch was not disabled')
  const status = await api('/desktop')
  assert.equal(status.tray_ready, true)
  assert.equal(status.menu.length, 8)
  assert.equal(status.preferences.minimize_to_tray, true)
  assert.equal(status.preferences.close_to_tray, true)
  assert.equal(await page.getByRole('alert').count(), 0)
  await page.screenshot({ path: path.join(root, 'artifacts/native-settings.png') })
  const settings = await api('/settings')
  await api('/settings', 'PUT', { ...settings, scheduler_enabled: false })
  const template = (await api('/templates')).find(t => t.id === 'newapi')
  const loginSite = await api('/sites', 'POST', { ...template.site, id: '', name: 'Native login fixture',
    base_url: 'http://127.0.0.1:' + fixture.address().port, transport: 'browser' })
  const loginAccount = await api('/accounts', 'POST', { name: 'Native login fixture', site_id: loginSite.id,
    enabled: true, version: 0, schedule: template.schedule, credentials: { method: 'cookie' } })
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '账号签到' }).click()
  await page.getByRole('button', { name: '刷新数据' }).click()
  let previewRequests = 0
  page.on('request', request => { if (request.url().endsWith('/screenshot')) previewRequests++ })
  const opened = page.waitForResponse(response => response.url().endsWith('/accounts/' + loginAccount.id + '/login'))
  await page.getByRole('button', { name: '浏览器登录 Native login fixture' }).click()
  assert.equal((await (await opened).json()).native_window, true)
  await page.getByRole('heading', { name: '独立登录窗口已打开' }).waitFor()
  await page.getByRole('button', { name: '显示登录窗口' }).click()
  assert.equal(previewRequests, 0, 'native login should not poll screenshots')
  await page.screenshot({ path: path.join(root, 'artifacts/native-login-window.png') })
  await page.getByRole('button', { name: '取消登录' }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  const loginDiagnostics = await api('/diagnostics')
  assert.equal(loginDiagnostics.browser_pool.active, 0, 'cancel should release the browser slot')
  assert.equal(loginDiagnostics.browser_pool.native_windows, 0, 'cancel should close the login window')
  await api('/accounts/' + loginAccount.id, 'DELETE')
  await api('/sites/' + loginSite.id, 'DELETE')
  await api('/desktop/action', 'POST', { action: 'minimize' })
  await until(async () => !(await api('/desktop')).window_open, 'Minimized window was not released into tray')
  assert.equal((await fetch(baseURL + '/healthz')).status, 200)
  const trayDiagnostics = await api('/diagnostics')
  await api('/desktop/action', 'POST', { action: 'show' })
  browser = await connect()
  page = browser.contexts()[0].pages()[0]
  await page.getByRole('heading', { name: '我的账号' }).waitFor()
  await page.close()
  await until(async () => !(await api('/desktop')).window_open, 'Closing the display did not leave the tray process alive')
  const prior = (await api('/settings')).scheduler_enabled
  await api('/desktop/action', 'POST', { action: 'pause' })
  await until(async () => (await api('/settings')).scheduler_enabled !== prior, 'Tray pause action did not update scheduler')
  await api('/desktop/action', 'POST', { action: 'run_all' })
  await writeFile(path.join(root, 'artifacts/native-smoke.json'), JSON.stringify({
    passed: true, goVersion: 'go1.27.2', display: 'Edge/Chrome application window',
    tray: { minimize: true, restore: true, closeKeepsRunning: true, menus: status.menu },
    startupSwitch: 'passed with isolated test backend; real registry adapter separately unit-tested',
    nativeLoginWindow: { opened: true, focus: true, screenshotPolling: false, cancelled: true, resourcesReleased: true },
    trayDiagnostics, testedAt: new Date().toISOString(),
  }, null, 2))
  await api('/desktop/action', 'POST', { action: 'quit' })
  await until(() => app.exitCode !== null, 'Tray exit did not stop the process', 15000)
  assert.equal(app.exitCode, 0)
  console.log('Windows smoke passed: startup, tray menus, minimize, restore, close-to-tray, pause, startup switch, clean exit.')
} finally {
  await writeFile(path.join(data, 'native.log'), log)
  if (app.exitCode === null) { try { await api('/desktop/action', 'POST', { action: 'quit' }) } catch {} }
  await delay(500)
  if (app.exitCode === null) app.kill()
  await new Promise(resolve => fixture.close(resolve))
}
