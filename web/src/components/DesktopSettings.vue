<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { request } from '../api'
import type { DesktopPreferences, DesktopStatus } from '../types'
import Icon from './Icon.vue'
const status = ref<DesktopStatus | null>(null), busy = ref(false), error = ref('')
let timer = 0
async function load() {
  if (busy.value || document.visibilityState !== 'visible') return
  try { status.value = await request<DesktopStatus>('/desktop') }
  catch (e) { error.value = (e as Error).message }
}
async function toggle(key: 'autostart' | 'minimize_to_tray' | 'close_to_tray') {
  if (!status.value) return
  busy.value = true; error.value = ''
  const prefs: DesktopPreferences = { ...status.value.preferences, [key]: !status.value.preferences[key] }
  try { status.value = await request<DesktopStatus>('/desktop', 'PUT', prefs) }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
async function hide() {
  try { await request('/desktop/action', 'POST', { action: 'hide' }) }
  catch (e) { error.value = (e as Error).message }
}
onMounted(() => { void load(); timer = window.setInterval(load, 5000) })
onBeforeUnmount(() => clearInterval(timer))
</script>
<template>
  <section class="panel settings-card">
    <div class="card-heading"><Icon name="grid" /><h3>Windows 桌面</h3></div>
    <div v-if="error || status?.last_error" class="notice error" role="alert">{{ error || status?.last_error }}</div>
    <template v-if="status">
      <div v-for="option in [
        { key: 'autostart', label: '登录 Windows 后自动启动', hint: '启动后直接驻留托盘，按计划执行。' },
        { key: 'minimize_to_tray', label: '最小化到托盘', hint: '收起窗口并释放界面浏览器资源。' },
        { key: 'close_to_tray', label: '关闭窗口后继续运行', hint: '关闭窗口保留后台，通过托盘菜单退出。' },
      ] as const" :key="option.key" class="setting-row">
        <div><strong>{{ option.label }}</strong><p>{{ option.hint }}</p></div>
        <button type="button" class="toggle" :class="{ on: status.preferences[option.key] }" role="switch" :aria-label="option.label" :aria-checked="status.preferences[option.key]" :disabled="busy" @click="toggle(option.key)"><span /></button>
      </div>
      <div class="actions"><button class="button secondary" :disabled="!status.tray_ready" @click="hide"><Icon name="down" />收起到托盘</button><span class="field-help">{{ status.tray_ready ? '托盘已就绪' : '正在初始化托盘' }}</span></div>
      <details class="advanced tray-menu-help"><summary>托盘右键菜单</summary><ul><li v-for="item in status.menu" :key="item.action">{{ item.label }}</li></ul></details>
    </template>
  </section>
</template>
