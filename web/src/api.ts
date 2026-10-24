let accessToken = ''
let desktopMode = false
let connectionEpoch = 0
const snapshots = new Map<string, { etag: string; value: unknown }>()
const snapshotPaths = new Set(['/accounts', '/sites', '/settings'])
const mutationListeners = new Set<() => void>()

// Mutation completion outlives the view that initiated it. The application owns
// refreshes, so navigating away during a slow POST cannot lose the notification.
export function onWorkspaceMutation(listener: () => void) {
  mutationListeners.add(listener)
  return () => mutationListeners.delete(listener)
}

function changesWorkspace(path: string, method: string) {
  if (method === 'GET' || method === 'HEAD') return false
  return (
    /^\/(accounts|sites|runs)(\/|$)/.test(path) ||
    /^\/(settings|backup\/restore|desktop\/action)$/.test(path) ||
    /^\/sessions\/[^/]+(\/save)?$/.test(path)
  )
}
export const isDesktop = () => desktopMode
export function setDesktopMode(value: boolean) {
  desktopMode = value
}

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}
export function setToken(token: string) {
  connectionEpoch++
  snapshots.clear()
  accessToken = token.trim()
  if (accessToken) sessionStorage.setItem('checkin.access', accessToken)
  else sessionStorage.removeItem('checkin.access')
}
export async function initializeConnection() {
  const incoming = new URLSearchParams(location.hash.slice(1)).get('token')
  if (incoming) {
    setToken(incoming)
    history.replaceState(null, '', location.pathname + location.search)
  } else setToken(sessionStorage.getItem('checkin.access') || '')
  return Boolean(accessToken)
}
interface RequestOptions {
  signal?: AbortSignal
  timeoutMs?: number
}
async function response<T>(
  path: string,
  method: string,
  body: unknown,
  options: RequestOptions,
  read: (res: Response) => Promise<T>,
  conditional = false,
): Promise<T> {
  const epoch = connectionEpoch
  const cached = conditional ? snapshots.get(path) : undefined
  const controller = new AbortController()
  const signal = options.signal
    ? AbortSignal.any([controller.signal, options.signal])
    : controller.signal
  const timer = window.setTimeout(() => controller.abort(), options.timeoutMs ?? 75000)
  try {
    const headers: Record<string, string> = { Authorization: 'Bearer ' + accessToken }
    if (cached) headers['If-None-Match'] = cached.etag
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    const res = await fetch('/api/v1' + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    })
    if (
      (res.ok || res.status === 409) &&
      changesWorkspace(path, method) &&
      epoch === connectionEpoch &&
      !signal.aborted
    ) {
      for (const listener of mutationListeners) listener()
    }
    if (res.status === 304 && cached) return cached.value as T
    if (!res.ok) {
      const payload = await res.json().catch(() => null)
      throw new ApiError(payload?.error?.message || '服务暂时无法处理请求', res.status)
    }
    const value = await read(res)
    // A late read from an older login must never repopulate its snapshots.
    if (conditional && epoch === connectionEpoch && !signal.aborted) {
      const etag = res.headers.get('ETag')
      if (etag) snapshots.set(path, { etag, value })
      else snapshots.delete(path)
    }
    return value
  } catch (error) {
    if (options.signal?.aborted) throw error
    if (error instanceof ApiError) throw error
    throw new Error('连接已中断或请求超时，请确认程序仍在运行')
  } finally {
    clearTimeout(timer)
  }
}
export function request<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  return response(
    path,
    method,
    body,
    options,
    async (res) => (res.status === 204 ? (undefined as T) : res.json()),
    method === 'GET' && snapshotPaths.has(path),
  )
}
export function blob(
  path: string,
  method = 'GET',
  body?: unknown,
  options: RequestOptions = {},
): Promise<Blob> {
  return response(path, method, body, options, (res) => res.blob())
}
export async function base64(data: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = reject
    reader.readAsDataURL(data)
  })
}
export async function downloadBackup(data: Blob) {
  const url = URL.createObjectURL(data)
  const link = document.createElement('a')
  link.href = url
  link.download = 'auto-check-in.backup'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 10000)
}
