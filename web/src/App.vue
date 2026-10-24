<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { initializeConnection, onWorkspaceMutation, request, setToken } from './api'
import { useWorkspace } from './composables/useWorkspace'
import { time, today } from './format'
import type { LoginInfo } from './types'
import Icon from './components/Icon.vue'
import BrowserDialog from './components/BrowserDialog.vue'
import AccountsView from './views/AccountsView.vue'
import SitesView from './views/SitesView.vue'
import RunsView from './views/RunsView.vue'
import SettingsView from './views/SettingsView.vue'

const workspace = useWorkspace()
const { accounts, sites, settings, templates, meta, error, authorized, fetching, revision } =
  workspace
const view = ref('accounts'),
  booting = ref(true),
  token = ref(''),
  signingIn = ref(false),
  toast = ref('')
const browserInfo = ref<LoginInfo | null>(null)
const nav = [
  { id: 'accounts', label: '账号签到', icon: 'grid' },
  { id: 'sites', label: '站点管理', icon: 'globe' },
  { id: 'runs', label: '执行记录', icon: 'clock' },
  { id: 'settings', label: '设置', icon: 'settings' },
]
const completed = computed(() => accounts.value.filter((a) => today(a.last_success_at)).length)
const attention = computed(
  () =>
    accounts.value.filter((a) =>
      ['needs_attention', 'unverified', 'interrupted'].includes(a.last_status),
    ).length,
)
const activeCount = computed(() => accounts.value.filter((a) => a.active_run_id).length)
const siteCount = computed(() => new Set(accounts.value.map((a) => a.site_id)).size)
const next = computed(() => {
  let earliest: string | undefined
  for (const account of accounts.value) {
    if (account.enabled && account.next_run_at && (!earliest || account.next_run_at < earliest)) {
      earliest = account.next_run_at
    }
  }
  return earliest
})
let pollTimer = 0,
  toastTimer = 0
let disposed = false
let stopMutationWatch = () => {}
function scheduleRefresh() {
  clearTimeout(pollTimer)
  if (disposed || booting.value || !authorized.value || document.visibilityState !== 'visible')
    return
  pollTimer = window.setTimeout(
    () => void refresh(false),
    activeCount.value && !error.value ? 5000 : 30000,
  )
}
async function refresh(full: unknown = true) {
  clearTimeout(pollTimer)
  try {
    await workspace.refresh(typeof full === 'boolean' ? full : true)
  } catch {
    /* Inline connection state is maintained by the workspace. */
  } finally {
    scheduleRefresh()
  }
}
async function signIn() {
  signingIn.value = true
  setToken(token.value)
  await refresh()
  signingIn.value = false
  if (authorized.value) token.value = ''
}
function notify(message: string) {
  toast.value = message
  clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = ''), 6500)
}
function closeBrowser() {
  browserInfo.value = null
  void refresh()
}
function browserSaved() {
  closeBrowser()
  notify('会话已保存，可返回账号列表查看计划或手动签到')
}
function logout() {
  clearTimeout(pollTimer)
  if (browserInfo.value)
    void request('/sessions/' + browserInfo.value.id, 'DELETE', undefined, {
      timeoutMs: 12000,
    }).catch(() => undefined)
  workspace.reset()
  setToken('')
  browserInfo.value = null
}
onMounted(async () => {
  stopMutationWatch = onWorkspaceMutation(() => {
    if (authorized.value) void refresh()
  })
  document.addEventListener('visibilitychange', visible)
  try {
    if (await initializeConnection()) await refresh()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    booting.value = false
    scheduleRefresh()
  }
})
function visible() {
  clearTimeout(pollTimer)
  if (document.visibilityState === 'visible' && authorized.value && !booting.value) void refresh()
}
onBeforeUnmount(() => {
  stopMutationWatch()
  disposed = true
  clearTimeout(pollTimer)
  clearTimeout(toastTimer)
  document.removeEventListener('visibilitychange', visible)
})
</script>
<template>
  <div v-if="booting" class="boot-screen">
    <span class="brand-mark"><Icon name="check" :size="28" /></span>
    <p>正在打开签到台…</p>
  </div>
  <main v-else-if="!authorized" class="auth-screen">
    <div class="auth-card">
      <span class="brand-mark large"><Icon name="check" :size="32" /></span>
      <div class="eyebrow">AUTO CHECK-IN</div>
      <h1>连接签到台</h1>
      <p>管理你的站点、账号与每日签到计划。</p>
      <form class="form-stack" @submit.prevent="signIn">
        <label
          >管理访问令牌<input
            v-model="token"
            type="password"
            required
            autocomplete="current-password"
            placeholder="输入此实例的访问令牌"
        /></label>
        <div v-if="error" class="notice error" role="alert">{{ error }}</div>
        <button class="button primary full-width" type="submit" :disabled="signingIn">
          {{ signingIn ? '正在连接…' : '进入管理界面' }}<Icon name="arrow" />
        </button>
      </form>
      <p class="field-help">
        令牌位于运行设备数据目录的 access.token 文件，也可由部署时的 CHECKIN_ACCESS_TOKEN 指定。
      </p>
    </div>
    <p class="auth-footer"><Icon name="shield" :size="15" />私有部署 · 凭据加密 · 账号隔离</p>
  </main>
  <div v-else class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="#" @click.prevent="view = 'accounts'"
        ><span class="brand-mark"><Icon name="check" :size="24" /></span
        ><span><strong>签到台</strong><small>AUTO CHECK-IN</small></span></a
      >
      <div class="nav-caption">工作空间</div>
      <nav aria-label="主导航">
        <button
          v-for="item in nav"
          :key="item.id"
          class="nav-item"
          :class="{ selected: view === item.id }"
          :aria-current="view === item.id ? 'page' : undefined"
          @click="view = item.id"
        >
          <Icon :name="item.icon" />{{ item.label
          }}<span v-if="item.id === 'accounts'" class="nav-count">{{ accounts.length }}</span>
        </button>
      </nav>
      <div class="sidebar-bottom">
        <div class="private-label">
          <Icon name="shield" :size="17" /><span>私有部署，安心托管</span>
        </div>
        <p>每天确认成功一次<br />让重复的事自动完成</p>
        <small>v{{ meta?.version }}</small>
      </div>
    </aside>
    <div class="main-column">
      <header class="topbar">
        <div>
          <span class="breadcrumb">工作空间</span><span class="slash">/</span
          ><strong>{{ nav.find((n) => n.id === view)?.label }}</strong>
        </div>
        <div class="topbar-right">
          <span class="run-indicator" :class="{ paused: !settings?.scheduler_enabled || !!error }"
            ><span class="status-dot" />{{
              error ? '连接异常' : settings?.scheduler_enabled ? '自动计划已开启' : '自动计划已暂停'
            }}</span
          ><button class="icon-button" aria-label="刷新数据" :disabled="fetching" @click="refresh">
            <Icon name="refresh" :class="{ spinning: fetching }" />
          </button>
        </div>
      </header>
      <main class="workspace">
        <div v-if="error" class="notice error" role="alert">
          {{ error }}，当前保留上次读取的数据。
        </div>
        <template v-if="view === 'accounts'">
          <div class="welcome-row">
            <div>
              <div class="eyebrow">DAILY, TAKEN CARE OF</div>
              <h1>每天的签到，一目了然。</h1>
              <p>首次随机执行，失败分类处理，成功后当天停止。</p>
            </div>
            <span class="timezone-label"><Icon name="clock" :size="15" />北京时间 · UTC+8</span>
          </div>
          <div class="stats-grid">
            <div class="stat-card">
              <span>管理账号</span>
              <div>
                <strong>{{ accounts.length }}</strong
                ><small>个</small><Icon name="grid" />
              </div>
              <p>分布在 {{ siteCount }} 个站点</p>
            </div>
            <div class="stat-card">
              <span>今日已完成</span>
              <div>
                <strong class="positive">{{ completed }}</strong
                ><small>/ {{ accounts.length }}</small
                ><Icon name="check" />
              </div>
              <p>含站点确认的已签到</p>
            </div>
            <button class="stat-card stat-action" @click="view = 'accounts'">
              <span>需要人工处理</span>
              <div>
                <strong :class="{ 'warning-text': attention > 0 }">{{ attention }}</strong
                ><small>个</small><Icon name="info" />
              </div>
              <p>{{ attention ? '请检查登录或确认结果' : '当前没有待处理异常' }}</p>
            </button>
            <div class="stat-card">
              <span>最近一次计划</span>
              <div>
                <strong class="next-time">{{
                  settings?.scheduler_enabled ? time(next) : '已暂停'
                }}</strong
                ><Icon name="clock" />
              </div>
              <p>首次默认在 08:00–09:00</p>
            </div>
          </div>
        </template>
        <AccountsView
          v-if="view === 'accounts'"
          :accounts="accounts"
          :sites="sites"
          :templates="templates"
          :browser-available="meta?.browser_available || false"
          @notify="notify"
          @login="browserInfo = $event"
        />
        <SitesView
          v-else-if="view === 'sites'"
          :sites="sites"
          :templates="templates"
          :accounts="accounts"
          @notify="notify"
        />
        <RunsView v-else-if="view === 'runs'" :accounts="accounts" :revision="revision" />
        <SettingsView
          v-else-if="settings && meta"
          :settings="settings"
          :meta="meta"
          :account-count="accounts.length"
          @notify="notify"
          @logout="logout"
        />
        <footer class="workspace-footer">
          <span>登录会话与执行结果保存在当前实例</span
          ><span>{{ activeCount ? activeCount + ' 个任务进行中' : '所有任务状态已同步' }}</span>
        </footer>
      </main>
    </div>
    <div v-if="toast" class="toast" role="status">
      <Icon name="info" :size="18" />{{ toast
      }}<button class="icon-button" aria-label="关闭通知" @click="toast = ''">
        <Icon name="close" :size="15" />
      </button>
    </div>
    <BrowserDialog
      v-if="browserInfo"
      :info="browserInfo"
      @close="closeBrowser"
      @saved="browserSaved"
    />
  </div>
</template>
