import { test, expect } from '@playwright/test'
import { createServer } from 'node:http'
import { request, blob, setToken } from '../src/api'

test('请求超时覆盖完整 JSON 和图片响应体', async () => {
  const server = createServer((_req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.write('{')
    // Send headers immediately but leave the body stalled.
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address() as { port: number }
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  globalThis.window = globalThis as unknown as Window & typeof globalThis
  globalThis.fetch = (input, options) =>
    originalFetch(`http://127.0.0.1:${address.port}${input}`, options)
  try {
    for (const read of [request, blob]) {
      const started = Date.now()
      const result = await Promise.race([
        read('/stalled', 'GET', undefined, { timeoutMs: 80 }).then(
          () => 'completed',
          () => 'aborted',
        ),
        new Promise<string>((resolve) => setTimeout(() => resolve('still reading'), 800)),
      ])
      expect(result).toBe('aborted')
      expect(Date.now() - started).toBeLessThan(800)
    }
  } finally {
    globalThis.fetch = originalFetch
    globalThis.window = originalWindow
    server.closeAllConnections()
    await new Promise<void>((resolve) => server.close(() => resolve()))
  }
})

test('条件读取复用未变化快照，更换令牌后清除缓存并隔离迟到响应', async () => {
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const originalStorage = globalThis.sessionStorage
  globalThis.window = globalThis as unknown as Window & typeof globalThis
  globalThis.sessionStorage = {
    setItem() {},
    removeItem() {},
    getItem() {
      return null
    },
  } as unknown as Storage
  const observed: { auth: string; etag: string | null }[] = []
  let release = () => {}
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  globalThis.fetch = async (_input, options) => {
    const headers = new Headers(options?.headers)
    const auth = headers.get('Authorization') || ''
    const etag = headers.get('If-None-Match')
    observed.push({ auth, etag })
    if (auth === 'Bearer old-fixture-token') await gate
    const tag = auth === 'Bearer old-fixture-token' ? '"old"' : '"current"'
    return etag === tag
      ? new Response(null, { status: 304, headers: { ETag: tag } })
      : Response.json([{ account: auth }], { headers: { ETag: tag } })
  }
  try {
    setToken('current-fixture-token')
    const first = await request('/accounts')
    expect(await request('/accounts')).toBe(first)
    expect(observed.at(-1)?.etag).toBe('"current"')
    setToken('old-fixture-token')
    const old = request('/accounts')
    setToken('current-fixture-token')
    const current = await request('/accounts')
    expect(observed.at(-1)?.etag).toBeNull()
    release()
    await old
    expect(await request('/accounts')).toBe(current)
    expect(observed.at(-1)?.etag).toBe('"current"')
  } finally {
    release()
    setToken('')
    globalThis.fetch = originalFetch
    globalThis.window = originalWindow
    globalThis.sessionStorage = originalStorage
  }
})
