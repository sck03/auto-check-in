<script setup lang="ts">
import { ref, watch } from 'vue'
import { base64, blob, downloadBackup, isDesktop, request } from '../api'
import type { Meta, Settings } from '../types'
import Icon from '../components/Icon.vue'
import DesktopSettings from '../components/DesktopSettings.vue'
const props = defineProps<{ settings: Settings; meta: Meta; accountCount: number }>()
const emit = defineEmits<{ refresh: []; notify: [string]; logout: [] }>()
const form = ref({ ...props.settings }),
  dirty = ref(false),
  busy = ref(false),
  error = ref('')
const password = ref(''),
  file = ref<File | null>(null),
  backupBusy = ref(false)
watch(
  () => props.settings,
  (value) => {
    if (!dirty.value) form.value = { ...value }
  },
)
function toggleScheduler() {
  form.value.scheduler_enabled = !form.value.scheduler_enabled
  dirty.value = true
}
async function save() {
  busy.value = true
  error.value = ''
  try {
    form.value = await request<Settings>('/settings', 'PUT', form.value)
    dirty.value = false
    emit('refresh')
    emit('notify', '设置已保存')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
async function exportBackup() {
  backupBusy.value = true
  error.value = ''
  try {
    await downloadBackup(await blob('/backup/export', 'POST', { password: password.value }))
    password.value = ''
    emit('notify', '已导出加密备份')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    backupBusy.value = false
  }
}
async function restore() {
  if (!file.value) return
  backupBusy.value = true
  error.value = ''
  try {
    if (file.value.size > 48 * 1024 * 1024) throw new Error('备份文件不能超过 48 MiB')
    await request('/backup/restore', 'POST', {
      password: password.value,
      archive: await base64(file.value),
    })
    file.value = null
    password.value = ''
    emit('refresh')
    emit('notify', '备份已恢复，自动计划处于暂停状态，请检查后启用')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    backupBusy.value = false
  }
}
</script>
<template>
  <div class="section-heading">
    <div>
      <h2>设置</h2>
      <p>控制执行节奏，妥善保管账号与备份。</p>
    </div>
  </div>
  <div v-if="error" role="alert" class="notice error">{{ error }}</div>
  <div class="settings-layout">
    <form
      class="panel settings-card"
      @submit.prevent="save"
      @input="dirty = true"
      @change="dirty = true"
    >
      <div class="card-heading">
        <Icon name="clock" />
        <h3>自动执行</h3>
      </div>
      <div class="setting-row">
        <div>
          <strong>启用每日计划</strong>
          <p>关闭后仍可手动签到。</p>
        </div>
        <button
          type="button"
          class="toggle"
          :class="{ on: form.scheduler_enabled }"
          role="switch"
          :aria-checked="form.scheduler_enabled"
          aria-label="启用每日计划"
          @click="toggleScheduler"
        >
          <span />
        </button>
      </div>
      <div class="notice info">
        默认每天 08:00–09:00 分账号随机开始；每天确认成功一次。临时故障允许有限补签，默认截止
        09:00。
      </div>
      <div class="field-grid">
        <label
          >最多并行账号数<input
            v-model.number="form.concurrency"
            type="number"
            min="1"
            max="4"
            required
        /></label>
        <label
          >失败后最多重试次数<input
            v-model.number="form.max_retries"
            type="number"
            min="0"
            max="3"
            required
        /></label>
        <label
          >首次重试等待（秒）<input
            v-model.number="form.retry_base_seconds"
            type="number"
            min="60"
            max="1800"
            required
        /></label>
        <label
          >同站点最小间隔（秒）<input
            v-model.number="form.site_gap_seconds"
            type="number"
            min="0"
            max="300"
            required
        /></label>
        <label
          >单次任务超时（秒）<input
            v-model.number="form.task_timeout_seconds"
            type="number"
            min="20"
            max="600"
            required
        /></label>
        <label
          >记录保留（天）<input
            v-model.number="form.retention_days"
            type="number"
            min="7"
            max="3650"
            required
        /></label>
      </div>
      <p class="field-help">
        下一次重试的等待时间为上一次的 3
        倍，并略微错峰。验证码、凭据错误和结果不明会暂停，不计入自动重试。
      </p>
      <div class="form-actions">
        <button class="button primary" type="submit" :disabled="busy || !dirty">
          {{ busy ? '保存中…' : '保存设置' }}
        </button>
      </div>
    </form>
    <div class="settings-side">
      <DesktopSettings v-if="meta.desktop" />
      <section class="panel settings-card">
        <div class="card-heading">
          <Icon name="shield" />
          <h3>加密备份</h3>
        </div>
        <p class="muted">备份包含站点、账号凭据、计划和已结束的记录。请保存好备份口令。</p>
        <label
          >备份口令<input
            v-model="password"
            type="password"
            minlength="12"
            maxlength="1024"
            autocomplete="new-password"
            placeholder="至少 12 个字符"
        /></label>
        <button
          class="button secondary full-width"
          :disabled="password.length < 12 || backupBusy"
          @click="exportBackup"
        >
          <Icon name="down" />{{ backupBusy ? '处理中…' : '导出加密备份' }}
        </button>
        <div class="divider" />
        <label
          >恢复文件<input
            type="file"
            accept=".backup,application/octet-stream,application/json"
            :disabled="accountCount > 0 || backupBusy"
            @change="file = ($event.target as HTMLInputElement).files?.[0] || null"
        /></label>
        <p class="field-help">
          {{
            accountCount
              ? '为保护现有账号，恢复请使用没有账号的新数据目录。'
              : '恢复后自动计划会暂停，检查配置后再启用。'
          }}
        </p>
        <button
          class="button secondary full-width"
          :disabled="!file || password.length < 12 || backupBusy || accountCount > 0"
          @click="restore"
        >
          <Icon name="up" />恢复备份
        </button>
      </section>
      <section class="panel settings-card">
        <div class="card-heading">
          <Icon name="info" />
          <h3>运行环境</h3>
        </div>
        <dl class="details-list compact">
          <dt>版本</dt>
          <dd>{{ meta.version }}</dd>
          <dt>Go 版本</dt>
          <dd>{{ meta.go_version }}</dd>
          <dt>浏览器</dt>
          <dd :class="meta.browser_available ? 'positive' : 'warning-text'">
            {{ meta.browser_available ? '已就绪' : '未检测到' }}
          </dd>
          <dt>运行方式</dt>
          <dd>{{ isDesktop() ? 'Windows 桌面' : '网页管理' }}</dd>
        </dl>
        <p v-if="!meta.browser_available" class="field-help">
          安装 Chrome / Edge / Chromium，或配置浏览器路径后重启，即可使用浏览器登录。
        </p>
        <p class="field-help">
          桌面版可驻留托盘运行；是否在关闭窗口后继续运行由桌面开关控制。常驻服务关闭管理网页后仍会运行。
        </p>
        <button v-if="!isDesktop()" class="text-button" @click="emit('logout')">
          退出管理界面
        </button>
      </section>
    </div>
  </div>
</template>
