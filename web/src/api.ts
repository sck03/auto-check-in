let accessToken = ''
let desktopMode = false
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
  accessToken = token.trim()
  if (accessToken) sessionStorage.setItem('checkin.access', accessToken)
  else sessionStorage.removeItem('checkin.access')
}
export async function initializeConnection() {
  const incoming = new URLSearchParams(location.hash.slice(1)).get('token')
  if (incoming) {
    setToken(incoming)
    history.replaceState(null, '', location.pathname + location.search)
  } else accessToken = sessionStorage.getItem('checkin.access') || ''
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
): Promise<T> {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), options.timeoutMs ?? 75000)
  try {
    const headers: Record<string, string> = { Authorization: 'Bearer ' + accessToken }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    const res = await fetch('/api/v1' + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: options.signal
        ? AbortSignal.any([controller.signal, options.signal])
        : controller.signal,
    })
    if (!res.ok) {
      const payload = await res.json().catch(() => null)
      throw new ApiError(payload?.error?.message || '服务暂时无法处理请求', res.status)
    }
    return await read(res)
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
  return response(path, method, body, options, async (res) =>
    res.status === 204 ? (undefined as T) : res.json(),
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
