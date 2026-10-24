import { onMounted, onScopeDispose, ref } from 'vue'
import { request } from '../api'
import type { DesktopPreferences, DesktopStatus } from '../types'

export function useDesktopSettings() {
  const status = ref<DesktopStatus | null>(null)
  const busy = ref(false)
  const error = ref('')
  const lifetime = new AbortController()
  let read: AbortController | undefined
  let timer = 0

  function stopRead() {
    clearTimeout(timer)
    read?.abort()
    read = undefined
  }

  function schedule() {
    clearTimeout(timer)
    if (!lifetime.signal.aborted && document.visibilityState === 'visible')
      timer = window.setTimeout(load, 5000)
  }

  async function load() {
    if (busy.value || lifetime.signal.aborted || document.visibilityState !== 'visible') return
    stopRead()
    const current = new AbortController()
    read = current
    try {
      const value = await request<DesktopStatus>('/desktop', 'GET', undefined, {
        signal: current.signal,
      })
      if (!current.signal.aborted) {
        status.value = value
        error.value = ''
      }
    } catch (e) {
      if (!current.signal.aborted) error.value = (e as Error).message
    } finally {
      if (read === current) {
        read = undefined
        schedule()
      }
    }
  }

  async function toggle(key: 'autostart' | 'minimize_to_tray' | 'close_to_tray') {
    if (!status.value || busy.value) return
    stopRead() // An older GET must not overwrite the committed PUT response.
    busy.value = true
    error.value = ''
    const prefs: DesktopPreferences = {
      ...status.value.preferences,
      [key]: !status.value.preferences[key],
    }
    try {
      status.value = await request<DesktopStatus>('/desktop', 'PUT', prefs, {
        signal: lifetime.signal,
      })
    } catch (e) {
      if (!lifetime.signal.aborted) error.value = (e as Error).message
    } finally {
      busy.value = false
      schedule()
    }
  }

  async function hide() {
    try {
      await request('/desktop/action', 'POST', { action: 'hide' }, { signal: lifetime.signal })
    } catch (e) {
      if (!lifetime.signal.aborted) error.value = (e as Error).message
    }
  }

  function visibility() {
    stopRead()
    if (document.visibilityState === 'visible') void load()
  }
  onMounted(() => {
    document.addEventListener('visibilitychange', visibility)
    void load()
  })
  onScopeDispose(() => {
    lifetime.abort()
    stopRead()
    document.removeEventListener('visibilitychange', visibility)
  })
  return { status, busy, error, toggle, hide }
}
