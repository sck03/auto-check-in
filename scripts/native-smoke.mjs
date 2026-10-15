import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
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
const app = spawn(process.env.SMOKE_DESKTOP_EXE || path.join(root, 'dist/AutoCheckIn.exe'), [], {
  env: { ...process.env, CHECKIN_DATA_DIR: data, CHECKIN_ACCESS_TOKEN: token,
    CHECKIN_DESKTOP_TEST_PORT: String(port), CHECKIN_TEST_STARTUP_FILE: path.join(data, 'test-startup') },
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
  return r.json()
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
}
