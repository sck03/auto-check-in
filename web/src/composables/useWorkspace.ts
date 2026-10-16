import { onScopeDispose, ref, shallowRef } from 'vue'
import { ApiError, request, setDesktopMode } from '../api'
import type { Account, Meta, Settings, Site, Template } from '../types'

export function useWorkspace() {
  // Server snapshots are replaced as a whole; forms own their editable copies.
  const accounts = shallowRef<Account[]>([])
  const sites = shallowRef<Site[]>([])
  const settings = shallowRef<Settings | null>(null)
  const templates = shallowRef<Template[]>([])
  const meta = shallowRef<Meta | null>(null)
  const error = ref('')
  const authorized = ref(false)
  const fetching = ref(false)
  const revision = ref(0)
  let active: Promise<void> | undefined
  let controller: AbortController | undefined
  let pendingFull = false

  async function load(full: boolean, signal: AbortSignal) {
    const [a, config, extra] = await Promise.all([
      request<Account[]>('/accounts', 'GET', undefined, { signal }),
      request<Settings>('/settings', 'GET', undefined, { signal }),
      full
        ? Promise.all([
            request<Site[]>('/sites', 'GET', undefined, { signal }),
            request<Template[]>('/templates', 'GET', undefined, { signal }),
            request<Meta>('/meta', 'GET', undefined, { signal }),
          ])
        : undefined,
    ])
    if (signal.aborted) return
    accounts.value = a
    settings.value = config
    if (extra) {
      ;[sites.value, templates.value, meta.value] = extra
      setDesktopMode(extra[2].desktop)
    }
    authorized.value = true
    error.value = ''
    revision.value++
  }

  function refresh(full = true): Promise<void> {
    if (active) {
      // Mutations need a fresh read after any older request already in flight.
      pendingFull ||= full
      return active
    }
    const current = new AbortController()
    controller = current
    fetching.value = true
    active = (async () => {
      let next = full || !authorized.value
      do {
        pendingFull = false
        try {
          await load(next, current.signal)
        } catch (e) {
          if (!pendingFull || current.signal.aborted) throw e
        }
        next = pendingFull
      } while (next && !current.signal.aborted)
    })()
      .catch((e) => {
        if (!current.signal.aborted) {
          error.value = e instanceof Error ? e.message : '无法读取数据'
          if (e instanceof ApiError && e.status === 401) authorized.value = false
          throw e
        }
      })
      .finally(() => {
        if (controller !== current) return
        fetching.value = false
        active = undefined
        controller = undefined
      })
    return active
  }

  function reset() {
    controller?.abort()
    controller = undefined
    active = undefined
    pendingFull = false
    fetching.value = authorized.value = false
    accounts.value = []
    sites.value = []
    templates.value = []
    settings.value = meta.value = null
    error.value = ''
    setDesktopMode(false)
  }
  onScopeDispose(reset)
  return {
    accounts,
    sites,
    settings,
    templates,
    meta,
    error,
    authorized,
    fetching,
    revision,
    refresh,
    reset,
  }
}
