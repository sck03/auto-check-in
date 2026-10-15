import { ref } from 'vue'
import { ApiError, request, setDesktopMode } from '../api'
import type { Account, Meta, Settings, Site, Template } from '../types'

export function useWorkspace() {
  const accounts = ref<Account[]>([])
  const sites = ref<Site[]>([])
  const settings = ref<Settings | null>(null)
  const templates = ref<Template[]>([])
  const meta = ref<Meta | null>(null)
  const error = ref('')
  const authorized = ref(false)
  const fetching = ref(false)
  const revision = ref(0)
  async function refresh(full = true) {
    if (fetching.value) return
    fetching.value = true
    try {
      if (!full && authorized.value) {
        const [a, config] = await Promise.all([request<Account[]>('/accounts'), request<Settings>('/settings')])
        accounts.value = a
        settings.value = config
        error.value = ''
        revision.value++
        return
      }
      const [a, s, config, t, m] = await Promise.all([
        request<Account[]>('/accounts'),
        request<Site[]>('/sites'),
        request<Settings>('/settings'),
        request<Template[]>('/templates'),
        request<Meta>('/meta'),
      ])
      accounts.value = a
      sites.value = s
      settings.value = config
      templates.value = t
      meta.value = m
      setDesktopMode(m.desktop)
      authorized.value = true
      error.value = ''
      revision.value++
    } catch (e) {
      error.value = e instanceof Error ? e.message : '无法读取数据'
      if (e instanceof ApiError && e.status === 401) authorized.value = false
      throw e
    } finally {
      fetching.value = false
    }
  }
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
  }
}
