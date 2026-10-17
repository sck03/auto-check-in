import { test, expect, type Page } from '@playwright/test'
import { createServer } from 'node:http'
import { spawn, type ChildProcess } from 'node:child_process'
import { mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const token = 'local-e2e-fixture-token-32-characters-only'
const auth = { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }
let url = '',
  siteURL = '',
  directory = '',
  server: ChildProcess
let fixture: ReturnType<typeof createServer>
const checked = new Set<string>()
const calls = new Map<string, number>()
let errors: string[] = []

async function api(route: string, method = 'GET', data?: unknown) {
  const response = await fetch(url + '/api/v1' + route, {
    method,
    headers: auth,
    body: data === undefined ? undefined : JSON.stringify(data),
  })
  if (!response.ok) throw new Error(await response.text())
  return response.status === 204 ? null : response.json()
}
async function start() {
  checked.clear()
  calls.clear()
  errors = []
  fixture = createServer(async (req, res) => {
    const route = new URL(req.url || '/', 'http://fixture').pathname
    const send = (data: unknown, status = 200) => {
      res.writeHead(status, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(data))
    }
    if (route === '/' || route === '/login') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
      res.end(fixtureHTML)
      return
    }
    if (route === '/api/user/login') {
      let raw = ''
      for await (const chunk of req) raw += chunk
      const data = JSON.parse(raw)
      if (data.password !== 'fixture-password') {
        send({ success: false, message: '用户名或密码不正确' })
        return
      }
      res.setHeader('Set-Cookie', 'session=' + data.username + '; HttpOnly; Path=/; SameSite=Lax')
      send({ success: true, data: { id: data.username, checked_in: false } })
      return
    }
    const name = /(?:^|;\s*)session=([^;]+)/.exec(req.headers.cookie || '')?.[1]
    if (!name || req.headers['new-api-user'] !== name) {
      send({ success: false }, 401)
      return
    }
    if (route === '/api/user/self') {
      send({ success: true, data: { id: name, quota: checked.has(name) ? 750000 : 500000 } })
      return
    }
    if (route === '/api/user/checkin') {
      if (req.method === 'GET') {
        send({ success: true, data: { stats: { checked_in_today: checked.has(name) } } })
        return
      }
      calls.set(name, (calls.get(name) || 0) + 1)
      if (checked.has(name)) {
        send({ success: false, message: '今日已签到' })
        return
      }
      checked.add(name)
      send({ success: true, message: '签到成功', data: { quota_awarded: 250000 } })
      return
    }
    send({ success: false }, 404)
  })
  await new Promise<void>((resolve) => fixture.listen(0, '127.0.0.1', resolve))
  const address = fixture.address() as { port: number }
  siteURL = 'http://127.0.0.1:' + address.port
  await mkdir(path.join(root, 'artifacts'), { recursive: true })
  directory = await mkdtemp(path.join(root, 'artifacts', 'e2e-'))
  const executable = path.join(
    root,
    'dist',
    process.platform === 'win32' ? 'checkin-server.exe' : 'checkin-server',
  )
  server = spawn(executable, ['-listen', '127.0.0.1:0', '-data', directory], {
    env: { ...process.env, CHECKIN_ACCESS_TOKEN: token, CHECKIN_ALLOW_HTTP: 'true' },
    windowsHide: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let log = ''
  server.stdout?.on('data', (data) => (log += data.toString()))
  server.stderr?.on('data', (data) => (log += data.toString()))
  server.on('error', (e) => {
    log += String(e)
  })
  for (let i = 0; i < 150; i++) {
    try {
      url = JSON.parse(await readFile(path.join(directory, 'endpoint.json'), 'utf8')).url
      break
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
  }
  if (!url) throw new Error('server failed: ' + log)
  const settings = await api('/settings')
  await api('/settings', 'PUT', { ...settings, scheduler_enabled: false, site_gap_seconds: 0 })
  return () => writeFile(path.join(directory, 'server.log'), log)
}
let saveLog: () => Promise<void>
test.beforeEach(async ({ page }) => {
  url = ''
  saveLog = await start()
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(url + '/#token=' + token)
  await expect(page.getByRole('heading', { name: '我的账号' })).toBeVisible()
})
test.afterEach(async () => {
  await saveLog?.()
  server?.kill()
  await new Promise<void>((resolve) => fixture?.close(() => resolve()))
})
async function seedSite() {
  const templates = await api('/templates')
  return api('/sites', 'POST', {
    ...templates.find((t: any) => t.id === 'newapi').site,
    id: '',
    name: '本地测试站',
    base_url: siteURL,
  })
}
async function seedAccount(name: string, withSession = true) {
  const site = await seedSite()
  const templates = await api('/templates')
  return api('/accounts', 'POST', {
    name,
    site_id: site.id,
    enabled: true,
    version: 0,
    schedule: templates[0].schedule,
    credentials: {
      method: 'cookie',
      ...(withSession ? { api_user: name, cookie_header: 'session=' + name } : {}),
    },
  })
}
async function selectNav(page: Page, label: string) {
  await page
    .getByRole('navigation', { name: '主导航' })
    .getByRole('button', { name: label })
    .click()
}

test('站点和账号管理、签到结果、每日防重复、编辑保留凭据', async ({ page }) => {
  await selectNav(page, '站点管理')
  await page.getByRole('button', { name: '添加站点', exact: true }).click()
  await page.getByLabel('平台模板').selectOption('x666')
  await expect(page.getByLabel('站点名称', { exact: true })).toHaveValue('薄荷 API')
  await expect(page.getByLabel('站点地址', { exact: true })).toHaveValue('https://up.x666.me')
  await page.getByLabel('平台模板').selectOption('newapi')
  await expect(page.getByLabel('站点地址', { exact: true })).toHaveValue('')
  await page.getByLabel('站点名称', { exact: true }).fill('本地测试站')
  await page.getByLabel('站点地址', { exact: true }).fill(siteURL)
  await page.getByRole('button', { name: '保存站点' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '本地测试站' })).toBeVisible()
  await selectNav(page, '账号签到')
  await page.getByRole('button', { name: '添加账号', exact: true }).click()
  await page.getByLabel('账号名称', { exact: true }).fill('演示账号')
  await page.getByLabel('所属站点').selectOption({ label: '本地测试站' })
  await page.getByLabel(/^Cookie/).fill('session=alice')
  await page.getByLabel(/^API 用户 ID/).fill('alice')
  await expect(page.getByLabel('随机窗口开始')).toHaveValue('08:00')
  await expect(page.getByLabel('随机窗口结束')).toHaveValue('09:00')
  await expect(page.getByLabel('失败补签截止')).toHaveValue('09:00')
  await page.getByRole('button', { name: '保存账号' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '签到 演示账号', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '签到 演示账号', exact: true }).click()
  await expect(page.getByText('已完成', { exact: true })).toBeVisible()
  expect(calls.get('alice')).toBe(1)
  const accounts = await api('/accounts')
  const duplicate = await api('/runs', 'POST', { account_ids: [accounts[0].id], retry: true })
  expect(duplicate[0].accepted).toBe(false)
  expect(calls.get('alice')).toBe(1)
  await page.screenshot({ path: path.join(root, 'artifacts/ui-desktop.png'), fullPage: true })
  await page.getByRole('button', { name: '编辑 演示账号', exact: true }).click()
  await expect(page.getByLabel(/^Cookie/)).toHaveValue('')
  await page.getByLabel('账号名称', { exact: true }).fill('重命名账号')
  await page.getByRole('button', { name: '保存账号' }).click()
  await expect(page.getByRole('button', { name: '编辑 重命名账号', exact: true })).toBeVisible()
  expect((await api('/accounts'))[0].auth.has_session).toBe(true)
  await selectNav(page, '执行记录')
  await expect(page.getByText('$0.5', { exact: true })).toBeVisible()
  await page.screenshot({ path: path.join(root, 'artifacts/ui-history.png'), fullPage: true })
  await selectNav(page, '账号签到')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: path.join(root, 'artifacts/ui-mobile.png'), fullPage: true })
  expect(errors).toEqual([])
})

test('四个平台模板与通用签到方式，修改和重新编辑后保留配置', async ({ page }) => {
  await selectNav(page, '站点管理')
  await page.getByRole('button', { name: '添加站点', exact: true }).click()
  const templates = page.getByLabel('平台模板')
  await expect(templates.locator('optgroup')).toHaveCount(2)
  await expect(templates.locator('option')).toHaveText([
    'AnyRouter',
    'AgentRouter',
    '薄荷 API',
    'New API（通用）',
  ])
  for (const [id, name, base, transport] of [
    ['anyrouter', 'AnyRouter', 'https://anyrouter.top', 'browser'],
    ['agentrouter', 'AgentRouter', 'https://agentrouter.org', 'browser'],
    ['x666', '薄荷 API', 'https://up.x666.me', 'browser'],
    ['newapi', 'New API（通用）', '', 'http'],
  ]) {
    await templates.selectOption(id)
    await expect(page.getByLabel('站点名称', { exact: true })).toHaveValue(name)
    await expect(page.getByLabel('站点地址', { exact: true })).toHaveValue(base)
    await expect(page.getByLabel('执行方式')).toHaveValue(transport)
  }
  await page.getByLabel('站点名称', { exact: true }).fill('我的 WONG 镜像')
  await page.getByLabel('站点地址', { exact: true }).fill(siteURL)
  await page.screenshot({
    path: path.join(root, 'artifacts/ui-site-templates.png'),
    fullPage: true,
  })
  await page.getByRole('button', { name: '保存站点' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.reload()
  await selectNav(page, '站点管理')
  const card = page
    .locator('.site-card')
    .filter({ has: page.getByRole('heading', { name: '我的 WONG 镜像' }) })
  await expect(card.locator('.tag')).toHaveText('New API（通用）')
  await card.getByRole('button', { name: '编辑站点' }).click()
  await expect(templates).toHaveValue('newapi')
  await expect(templates).toBeEnabled()
  await expect(page.getByLabel('站点地址', { exact: true })).toHaveValue(siteURL)
  // The generic template supports both modes without losing the site's identity.
  await page.getByRole('combobox', { name: '签到方式', exact: true }).selectOption('browser')
  await expect(page.getByLabel('站点名称', { exact: true })).toHaveValue('我的 WONG 镜像')
  await expect(page.getByLabel('站点地址', { exact: true })).toHaveValue(siteURL)
  await expect(page.getByLabel('执行方式')).toHaveValue('browser')
  await expect(page.getByLabel('执行方式')).toBeDisabled()
  await expect(page.getByLabel('签到页面', { exact: true })).toHaveValue('/')
  await page.getByLabel('签到页面', { exact: true }).fill('/daily')
  await page.getByLabel('签到按钮 CSS 选择器').fill('#checkin')
  await page.getByLabel('成功结果 CSS 选择器').fill('#success')
  await page.getByRole('button', { name: '保存站点' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.reload()
  await selectNav(page, '站点管理')
  await card.getByRole('button', { name: '编辑站点' }).click()
  await expect(templates).toHaveValue('newapi')
  await expect(page.getByRole('combobox', { name: '签到方式', exact: true })).toHaveValue('browser')
  await expect(page.getByLabel('签到页面', { exact: true })).toHaveValue('/daily')
  await expect(page.getByLabel('签到按钮 CSS 选择器')).toHaveValue('#checkin')
  await expect(page.getByLabel('成功结果 CSS 选择器')).toHaveValue('#success')
  await page.screenshot({ path: path.join(root, 'artifacts/ui-page-checkin.png'), fullPage: true })
  // Empty selectors opt into recognition and must survive saving/reopening.
  await page.getByLabel('签到按钮 CSS 选择器').fill('')
  await page.getByLabel('成功结果 CSS 选择器').fill('')
  await page.getByRole('button', { name: '保存站点' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await card.getByRole('button', { name: '编辑站点' }).click()
  await expect(page.getByLabel('签到按钮 CSS 选择器')).toHaveValue('')
  await expect(page.getByLabel('成功结果 CSS 选择器')).toHaveValue('')
  await templates.selectOption('anyrouter')
  await templates.selectOption('newapi')
  await expect(page.getByLabel('站点名称', { exact: true })).toHaveValue('我的 WONG 镜像')
  await expect(page.getByRole('combobox', { name: '签到方式', exact: true })).toHaveValue('newapi')
  await expect(page.getByLabel('执行方式')).toHaveValue('http')
  expect(errors).toEqual([])
})

test('已有 OAuth 账号的站点保留浏览器执行方式', async ({ page }) => {
  const account = await seedAccount('OAuth 账号', false)
  const site = (await api('/sites')).find((site: any) => site.id === account.site_id)
  await api('/sites/' + site.id, 'PUT', { ...site, transport: 'browser' })
  await api('/accounts/' + account.id, 'PUT', {
    name: account.name,
    site_id: account.site_id,
    enabled: true,
    schedule: account.schedule,
    version: account.version,
    credentials: { method: 'github' },
  })
  await page.getByRole('button', { name: '刷新数据' }).click()
  await selectNav(page, '站点管理')
  await page
    .locator('.site-card')
    .filter({ has: page.getByRole('heading', { name: '本地测试站' }) })
    .getByRole('button', { name: '编辑站点' })
    .click()
  await expect(page.getByLabel('平台模板')).toBeDisabled()
  await expect(page.getByRole('combobox', { name: '签到方式', exact: true })).toBeDisabled()
  await expect(page.getByLabel('执行方式')).toHaveValue('browser')
  await expect(page.getByLabel('执行方式').locator('option[value="http"]')).toHaveJSProperty(
    'disabled',
    true,
  )
  await expect(page.getByText('此站点的 OAuth 账号需要浏览器执行方式。')).toBeVisible()
  expect(errors).toEqual([])
})

test('清除旧会话会清除 Token 和用户 ID，并保留登录密码', async ({ page }) => {
  const account = await seedAccount('清理会话', false)
  await api('/accounts/' + account.id, 'PUT', {
    name: account.name,
    site_id: account.site_id,
    enabled: true,
    schedule: account.schedule,
    version: account.version,
    credentials: {
      method: 'password',
      username: 'alice',
      password: 'fixture-password',
      api_user: 'alice',
      cookie_header: 'session=alice',
      token: 'old-fixture-token',
    },
  })
  await page.getByRole('button', { name: '刷新数据' }).click()
  await page.getByRole('button', { name: '编辑 清理会话', exact: true }).click()
  await page.getByText('更多凭据选项', { exact: true }).click()
  await page.getByLabel('清除旧会话，重新建立登录').check()
  await page.getByRole('button', { name: '保存账号' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const saved = (await api('/accounts')).find((entry: any) => entry.id === account.id)
  expect(saved.auth).toMatchObject({
    has_session: false,
    has_token: false,
    api_user: '',
    has_password: true,
    username: 'alice',
  })
  expect(errors).toEqual([])
})

test('人工浏览器登录并保存独立会话', async ({ page }) => {
  await page.route('**/api/v1/sessions/*/input', async (route) => {
    const input = route.request().postDataJSON()
    if (input.type === 'text' && input.text === 'login-user')
      await new Promise((resolve) => setTimeout(resolve, 600))
    await route.continue()
  })
  const account = await seedAccount('login-user', false)
  await page.getByRole('button', { name: '刷新数据' }).click()
  await expect(page.getByRole('button', { name: '浏览器登录 login-user' })).toBeVisible()
  await page.getByRole('button', { name: '浏览器登录 login-user' }).click()
  const screenshot = page.getByAltText('独立登录浏览器，可点击页面进行操作')
  await expect(screenshot).toBeVisible()
  const bounds = await screenshot.boundingBox()
  if (!bounds) throw new Error('missing browser viewport')
  // Fixture input has explicit coordinates in its own 1200px viewport.
  await screenshot.click({
    position: { x: (100 * bounds.width) / 1200, y: (135 * bounds.width) / 1200 },
  })
  await page.getByLabel('向选中的网页输入框填写文字').fill('login-user')
  await page.getByRole('button', { name: '输入', exact: true }).click()
  await page.getByRole('button', { name: '下一输入框' }).click()
  await page.getByLabel('向选中的网页输入框填写文字').fill('fixture-password')
  await page.getByRole('button', { name: '输入', exact: true }).click()
  await page.getByRole('button', { name: '回车', exact: true }).click()
  await page.screenshot({ path: path.join(root, 'artifacts/ui-browser-login.png'), fullPage: true })
  await page.getByRole('button', { name: '保存会话', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const saved = (await api('/accounts')).find((a: any) => a.id === account.id)
  expect(saved.auth.has_session).toBe(true)
  expect(saved.auth.api_user).toBe('login-user')
  await page.getByRole('button', { name: '签到 login-user', exact: true }).click()
  await expect(page.getByText('已完成', { exact: true })).toBeVisible()
  expect(calls.get('login-user')).toBe(1)
  expect(errors).toEqual([])
})

test('手动执行失败显示具体原因和处理建议', async ({ page }) => {
  const account = await seedAccount('错误密码', false)
  await api('/accounts/' + account.id, 'PUT', {
    name: account.name,
    site_id: account.site_id,
    enabled: account.enabled,
    schedule: account.schedule,
    version: account.version,
    credentials: { method: 'password', username: 'failure-user', password: 'wrong-password' },
  })
  await page.getByRole('button', { name: '刷新数据' }).click()
  await page.getByRole('button', { name: '签到 错误密码', exact: true }).click()
  await expect(page.getByText('需要处理', { exact: true }).first()).toBeVisible()
  await selectNav(page, '执行记录')
  await page.getByRole('button', { name: '查看记录 错误密码' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('登录失败：用户名或密码不正确', { exact: true })).toBeVisible()
  await expect(dialog.getByText('提交登录 / OAuth 授权', { exact: true })).toBeVisible()
  await expect(dialog.getByText('200', { exact: true })).toBeVisible()
  await expect(dialog.getByText(/核对本站的用户名和密码/)).toBeVisible()
  await page.screenshot({
    path: path.join(root, 'artifacts/ui-failure-detail.png'),
    fullPage: true,
  })
  expect(errors).toEqual([])
})

test('访问保护、加密备份和空白实例恢复', async ({ page }) => {
  await seedAccount('backup-user')
  const denied = await fetch(url + '/api/v1/accounts')
  expect(denied.status).toBe(401)
  const crossOrigin = await fetch(url + '/api/v1/accounts', {
    headers: { ...auth, Origin: 'https://untrusted.invalid' },
  })
  expect(crossOrigin.status).toBe(403)
  const visible = JSON.stringify(await api('/accounts'))
  expect(visible).not.toContain('session=backup-user')
  await page.getByRole('button', { name: '刷新数据' }).click()
  await selectNav(page, '设置')
  await expect(page.locator('.data-directory')).toHaveText(directory)
  await page.screenshot({ path: path.join(root, 'artifacts/ui-settings.png'), fullPage: true })
  await page.getByLabel('备份口令').fill('e2e-long-backup-password')
  const downloading = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出加密备份' }).click()
  const downloaded = await downloading
  const file = await downloaded.path()
  if (!file) throw new Error('backup not downloaded')
  const archive = await readFile(file)
  expect(archive.toString()).not.toContain('backup-user')
  const all = await api('/accounts')
  await api('/accounts/' + all[0].id, 'DELETE')
  await api('/backup/restore', 'POST', {
    password: 'e2e-long-backup-password',
    archive: archive.toString('base64'),
  })
  expect((await api('/accounts'))[0].auth.has_session).toBe(true)
  expect((await api('/settings')).scheduler_enabled).toBe(false)
  expect(errors).toEqual([])
})

test('登录预览支持直接键盘输入并及时刷新', async ({ page }) => {
  const textInputs: string[] = []
  await page.route('**/api/v1/sessions/*/input', async (route) => {
    const event = route.request().postDataJSON()
    if (event.type === 'text') textInputs.push(event.text)
    await new Promise((resolve) => setTimeout(resolve, 200))
    await route.continue()
  })
  const account = await seedAccount('keyboard-user', false)
  await page.getByRole('button', { name: '刷新数据' }).click()
  await page.getByRole('button', { name: '浏览器登录 keyboard-user' }).click()
  const screenshot = page.getByAltText('独立登录浏览器，可点击页面进行操作')
  await expect(screenshot).toBeVisible()
  const bounds = await screenshot.boundingBox()
  if (!bounds) throw new Error('missing browser viewport')
  await screenshot.click({
    position: { x: (100 * bounds.width) / 1200, y: (135 * bounds.width) / 1200 },
  })
  const frame = screenshot.getAttribute('src')
  await page.keyboard.type('keyboard-user', { delay: 20 })
  await expect(screenshot).not.toHaveAttribute('src', (await frame) as string, { timeout: 1200 })
  await page.keyboard.press('Tab')
  await page.keyboard.type('fixture-password', { delay: 20 })
  await page.keyboard.press('Enter')
  await expect(page.getByText('正在发送操作…')).toHaveCount(0)
  await page.getByRole('button', { name: '保存会话', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const saved = (await api('/accounts')).find((a: any) => a.id === account.id)
  expect(saved.auth.api_user).toBe('keyboard-user')
  expect(saved.auth.has_session).toBe(true)
  expect(textInputs.join('')).toBe('keyboard-userfixture-password')
  expect(textInputs.length).toBeLessThanOrEqual(8)
  console.log(`连续输入：29 个字符合并为 ${textInputs.length} 次请求（每次模拟 200ms 延迟）`)
  expect(errors).toEqual([])
})

test('保存站点时保留进行中的刷新之后的更新', async ({ page }) => {
  await selectNav(page, '站点管理')
  await page.getByRole('button', { name: '添加站点', exact: true }).click()
  await page.getByLabel('站点名称', { exact: true }).fill('刷新期间新增站点')
  await page.getByLabel('站点地址', { exact: true }).fill(siteURL)
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  let captured!: () => void
  const started = new Promise<void>((resolve) => {
    captured = resolve
  })
  let held = false
  await page.route('**/api/v1/accounts', async (route) => {
    if (held) {
      await route.continue()
      return
    }
    held = true
    const response = await route.fetch()
    captured()
    await gate
    await route.fulfill({ response })
  })
  try {
    // A background refresh can finish with data from before the mutation.
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await started
    await page.getByRole('button', { name: '保存站点' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    release()
    await expect(page.getByRole('heading', { name: '刷新期间新增站点' })).toBeVisible({
      timeout: 2500,
    })
  } finally {
    release()
  }
  expect(errors).toEqual([])
})

async function pauseWorkspacePolling(page: Page) {
  // Isolate request counts from workspace polling.
  await page.clock.install({ time: new Date('2026-10-16T00:00:00Z') })
  await page.clock.pauseAt(new Date('2026-10-16T00:01:00Z'))
  await page.reload()
  await expect(page.getByRole('heading', { name: '我的账号' })).toBeVisible()
}

test('空闲降低轮询频率，运行中及时刷新，隐藏界面停止轮询', async ({ page }) => {
  const account = await seedAccount('轮询账号', false)
  await pauseWorkspacePolling(page)
  let count = 0
  let running = false
  await page.route('**/api/v1/accounts', async (route) => {
    count++
    await route.fulfill({
      json: [
        {
          ...account,
          active_run_id: running ? 'fixture-running' : '',
          active_status: running ? 'running' : '',
        },
      ],
    })
  })
  await page.clock.runFor(29900)
  expect(count).toBe(0)
  await page.clock.runFor(200)
  await expect.poll(() => count).toBe(1)
  await expect(page.getByRole('button', { name: '刷新数据' })).toBeEnabled()
  running = true
  await page.getByRole('button', { name: '刷新数据' }).click()
  await expect(page.getByText('1 个任务进行中')).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新数据' })).toBeEnabled()
  count = 0
  await page.clock.runFor(5001)
  await expect.poll(() => count).toBe(1)
  await expect(page.getByRole('button', { name: '刷新数据' })).toBeEnabled()
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  count = 0
  await page.clock.runFor(120000)
  expect(count).toBe(0)
  running = false
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => count).toBe(1)
  await expect(page.getByRole('button', { name: '刷新数据' })).toBeEnabled()
  await expect(page.getByText('所有任务状态已同步')).toBeVisible()
  await page.clock.runFor(29900)
  expect(count).toBe(1)
  expect(errors).toEqual([])
})

test('打开的执行详情随任务结果刷新', async ({ page }) => {
  await pauseWorkspacePolling(page)
  let finished = false
  await page.route('**/api/v1/runs?*', async (route) => {
    const status = finished ? 'succeeded' : 'running'
    await route.fulfill({
      json: {
        total: 1,
        items: [
          {
            id: 'live-detail',
            account_id: 'fixture',
            account_name: '详情账号',
            site_name: '测试站',
            action: 'checkin',
            trigger: 'manual',
            status,
            attempt: 1,
            created_at: '2026-10-16T00:00:00Z',
            started_at: '2026-10-16T00:00:00Z',
            finished_at: finished ? '2026-10-16T00:00:10Z' : null,
            result: {
              status,
              message: finished ? '签到结果已确认' : '正在执行签到',
              unit: 'USD',
              quota_per_unit: 500000,
              reward: finished ? 250000 : null,
            },
          },
        ],
      },
    })
  })
  await selectNav(page, '执行记录')
  await page.getByRole('button', { name: '查看记录 详情账号' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('正在执行签到', { exact: true })).toBeVisible()
  finished = true
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await expect(dialog.getByText('签到结果已确认', { exact: true })).toBeVisible()
  await expect(dialog.getByText('已完成', { exact: true })).toBeVisible()
  await expect(dialog.getByText('$0.5', { exact: true })).toBeVisible()
  expect(errors).toEqual([])
})

test('记录筛选重置分页时只发送一次查询', async ({ page }) => {
  await pauseWorkspacePolling(page)
  const queries: URLSearchParams[] = []
  await page.route('**/api/v1/runs?*', async (route) => {
    queries.push(new URL(route.request().url()).searchParams)
    await route.fulfill({ json: { items: [], total: 61 } })
  })
  await selectNav(page, '执行记录')
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => queries.some((query) => query.get('offset') === '30')).toBe(true)
  await page.getByRole('combobox', { name: '状态', exact: true }).selectOption('failed')
  await expect.poll(() => queries.some((query) => query.get('status') === 'failed')).toBe(true)
  // Let both Vue watchers and outgoing fetches settle; a duplicate used to be
  // sent when changing a filter while the current offset was nonzero.
  await page.waitForTimeout(150)
  const filtered = queries.filter((query) => query.get('status') === 'failed')
  expect(filtered).toHaveLength(1)
  expect(filtered[0].get('offset')).toBe('0')
  await expect(page.getByRole('button', { name: '上一页' })).toBeDisabled()
  expect(errors).toEqual([])
})

test('慢速记录查询合并连续刷新且最终显示结果', async ({ page }) => {
  await pauseWorkspacePolling(page)
  let count = 0
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/runs?*', async (route) => {
    count++
    if (count === 1) await gate
    await route.fulfill({ json: { items: [], total: count === 1 ? 31 : 61 } })
  })
  await selectNav(page, '执行记录')
  await expect.poll(() => count).toBe(1)
  try {
    await page.getByRole('button', { name: '刷新记录' }).click()
    await page.getByRole('button', { name: '刷新记录' }).click()
    await page.waitForTimeout(150)
    expect(count).toBe(1)
    release()
    await expect(page.getByText('共 61 条记录 · 北京时间')).toBeVisible()
    expect(count).toBe(2)
  } finally {
    release()
  }
  expect(errors).toEqual([])
})

test('记录查询在筛选变更和离开页面时中止', async ({ page }) => {
  await pauseWorkspacePolling(page)
  let hold = false
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/runs?*', async (route) => {
    if (hold) await gate
    await route.fulfill({ json: { items: [], total: 61 } }).catch(() => undefined)
  })
  await selectNav(page, '执行记录')
  await expect(page.getByRole('button', { name: '下一页' })).toBeEnabled()
  hold = true
  try {
    const oldRequest = page.waitForEvent(
      'request',
      (request) =>
        request.url().includes('/runs?') &&
        new URL(request.url()).searchParams.get('offset') === '30',
    )
    await page.getByRole('button', { name: '下一页' }).click()
    const old = await oldRequest
    const superseded = page.waitForEvent('requestfailed', (request) => request === old)
    const nextRequest = page.waitForEvent(
      'request',
      (request) =>
        request.url().includes('/runs?') &&
        new URL(request.url()).searchParams.get('status') === 'failed',
    )
    await page.getByRole('combobox', { name: '状态', exact: true }).selectOption('failed')
    await superseded
    const next = await nextRequest
    const abandoned = page.waitForEvent('requestfailed', (request) => request === next)
    await selectNav(page, '设置')
    await abandoned
    await expect(page.getByRole('heading', { name: '运行环境' })).toBeVisible()
  } finally {
    release()
  }
  expect(errors).toEqual([])
})

test('退出管理界面后丢弃迟到的刷新响应', async ({ page }) => {
  await selectNav(page, '设置')
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  let captured!: () => void
  const started = new Promise<void>((resolve) => {
    captured = resolve
  })
  await page.route('**/api/v1/meta', async (route) => {
    const response = await route.fetch()
    captured()
    await gate
    await route.fulfill({ response }).catch(() => undefined)
  })
  try {
    await page.getByRole('button', { name: '刷新数据' }).click()
    await started
    const aborted = page.waitForEvent('requestfailed', (request) =>
      request.url().endsWith('/api/v1/meta'),
    )
    await page.getByRole('button', { name: '退出管理界面' }).click()
    await aborted
    release()
    await expect(page.getByRole('heading', { name: '连接签到台' })).toBeVisible()
    expect(await page.evaluate(() => sessionStorage.getItem('checkin.access'))).toBeNull()
  } finally {
    release()
  }
  expect(errors).toEqual([])
})

const fixtureHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>测试登录页</title>
<style>body{margin:40px;font:18px sans-serif;background:#f4f8f6}h1{color:#297860;font-size:28px}input{position:absolute;left:60px;width:320px;height:38px;padding:4px;font:18px sans-serif}input[name=username]{top:115px}input[name=password]{top:175px}button{position:absolute;top:235px;left:60px;padding:10px 25px}#message{position:absolute;top:300px}</style>
<h1>本地模拟站点</h1><form><input name="username" placeholder="用户名"><input name="password" type="password" placeholder="密码"><button type="submit">登录</button></form><p id="message"></p>
<script>document.querySelector('form').onsubmit=async e=>{e.preventDefault();const r=await fetch('/api/user/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:document.querySelector('[name=username]').value,password:document.querySelector('[name=password]').value})});const d=await r.json();if(d.success){localStorage.setItem('user',JSON.stringify(d.data));document.querySelector('#message').textContent='登录成功，返回工具保存会话';}else document.querySelector('#message').textContent='登录失败';};</script></html>`
