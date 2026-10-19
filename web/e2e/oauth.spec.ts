import { test, expect, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'

const script = await readFile(new URL('../../internal/browser/oauth.js', import.meta.url), 'utf8')
const origin = 'https://agentrouter.org'
const defaults = {
  github: 'a[href*="/oauth/github"],button[data-provider="github"]',
  linuxdo: 'a[href*="/oauth/linuxdo"],button[data-provider="linuxdo"]',
}
function driver(method: string, selectors = defaults) {
  return `(${script})(${JSON.stringify(selectors)},${JSON.stringify(method)},${JSON.stringify(origin)},'/login')`
}
async function load(page: Page, url: string, html: string) {
  await page.route('**/*', (route) =>
    route.fulfill({ contentType: 'text/html; charset=utf-8', body: html }),
  )
  await page.goto(url)
}

for (const method of ['github', 'linuxdo']) {
  test(`OAuth 入口按 ${method} 选择，忽略页脚与隐藏、禁用按钮`, async ({ page }) => {
    await load(
      page,
      origin + '/login',
      `<a href="https://github.com/example/project">GitHub</a>
      <button hidden>使用 ${method} 继续</button><button disabled>使用 ${method} 继续</button>
      <button onclick="document.body.dataset.chosen='github'">使用 GitHub 继续</button>
      <button onclick="document.body.dataset.chosen='linuxdo'">使用 LinuxDO 继续</button>
      <button onclick="document.body.dataset.chosen='password'">使用 邮箱或用户名 登录</button>`,
    )
    expect(await page.evaluate(driver(method) + ".inspect('entry')")).toBe('ready')
    expect(await page.evaluate('document.body.dataset.chosen')).toBeUndefined()
    expect(await page.evaluate(driver(method) + ".click('entry')")).toBe('clicked')
    expect(await page.evaluate('document.body.dataset.chosen')).toBe(method)
  })

  test(`OAuth ${method} 授权只提交一次到本站回调`, async ({ page }) => {
    const url =
      method === 'github'
        ? 'https://github.com/login/oauth/authorize'
        : 'https://connect.linux.do/oauth2/authorize'
    const label = method === 'github' ? 'Authorize AgentRouter' : '允许'
    await load(
      page,
      url + '?redirect_uri=' + encodeURIComponent(origin + '/oauth/' + method),
      `<form method="post" onsubmit="event.preventDefault();this.dataset.calls=Number(this.dataset.calls||0)+1">
      <button name="deny" value="1">拒绝</button><button id="js-oauth-authorize-btn" name="authorize" value="1">${label}</button></form>`,
    )
    const expression = driver(method)
    expect(await page.evaluate(expression + ".inspect('consent')")).toBe('ready')
    expect(await page.evaluate('document.querySelector("form").dataset.calls')).toBeUndefined()
    expect(await page.evaluate(expression + ".click('consent')")).toBe('clicked')
    expect(await page.evaluate('document.querySelector("form").dataset.calls')).toBe('1')
    expect(
      await page.evaluate(
        driver(method === 'github' ? 'linuxdo' : 'github') + ".inspect('consent')",
      ),
    ).toBe('')
  })
}

test('OAuth 遇到多入口、无效选择器、其他站点时不点击', async ({ page }) => {
  await load(
    page,
    origin + '/login',
    '<button>使用 GitHub 继续</button><button>使用 GitHub 继续</button>',
  )
  expect(await page.evaluate(driver('github') + ".click('entry')")).toBe('ambiguous')
  expect(
    await page.evaluate(driver('github', { ...defaults, github: '[' }) + ".click('entry')"),
  ).toBe('invalid')
  await page.goto('https://example.org/login')
  expect(await page.evaluate(driver('github') + ".click('entry')")).toBe('left')
})

test('GitHub 默认回调须匹配 OAuth 事务，外站回调和不完整授权保持人工处理', async ({ page }) => {
  const url =
    'https://github.com/login/oauth/authorize?client_id=fixture-client&state=fixture-state'
  await load(
    page,
    url,
    `<form method="post" onsubmit="event.preventDefault()">
    <input name="client_id" value="fixture-client" type="hidden"><input name="state" value="fixture-state" type="hidden">
    <button id="js-oauth-authorize-btn">Authorize AgentRouter</button></form>`,
  )
  expect(await page.evaluate(driver('github') + ".inspect('consent')")).toBe('ready')
  await page.evaluate("document.querySelector('[name=state]').value='different'")
  expect(await page.evaluate(driver('github') + ".click('consent')")).toBe('')
  await page.goto(url + '&redirect_uri=https://example.org/callback')
  expect(await page.evaluate(driver('github') + ".click('consent')")).toBe('')
  await page.goto('https://github.com/login/oauth/authorize')
  expect(await page.evaluate(driver('github') + ".click('consent')")).toBe('')
})
