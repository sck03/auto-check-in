<script setup lang="ts">
import { computed, ref } from 'vue'
import { request } from '../api'
import { scheduleLabel, time, today } from '../format'
import type { Account, EnqueueResult, LoginInfo, Site, Template } from '../types'
import AccountForm from '../components/AccountForm.vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import StatusBadge from '../components/StatusBadge.vue'
const props = defineProps<{
  accounts: Account[]
  sites: Site[]
  templates: Template[]
  browserAvailable: boolean
}>()
const emit = defineEmits<{ refresh: []; notify: [string]; login: [LoginInfo] }>()
const search = ref(''),
  filter = ref('all'),
  error = ref(''),
  busy = ref(false),
  loginBusy = ref('')
const selected = ref<string[]>([])
const editing = ref<Account | null | undefined>(undefined)
const deleting = ref<Account | null>(null),
  retrying = ref<Account | null>(null)
const siteMap = computed(() => new Map(props.sites.map((s) => [s.id, s])))
const attention = (a: Account) =>
  ['needs_attention', 'unverified', 'interrupted'].includes(a.last_status)
const filtered = computed(() =>
  props.accounts.filter((a) => {
    const matches = (a.name + ' ' + (siteMap.value.get(a.site_id)?.name || ''))
      .toLowerCase()
      .includes(search.value.toLowerCase())
    return (
      matches &&
      (filter.value === 'all' ||
        (filter.value === 'attention' && attention(a)) ||
        (filter.value === 'enabled' && a.enabled))
    )
  }),
)
const hasAuth = (a: Account) => a.auth.has_password || a.auth.has_session || a.auth.has_token
async function run(ids: string[] = [], retry = false) {
  busy.value = true
  error.value = ''
  try {
    const result = await request<EnqueueResult[]>('/runs', 'POST', { account_ids: ids, retry })
    const accepted = result.filter((r) => r.accepted).length
    emit(
      'notify',
      accepted
        ? accepted + ' 个账号已加入队列' + (accepted < result.length ? '，其余账号已跳过' : '')
        : result[0]?.message || '没有可执行的账号',
    )
    retrying.value = null
    emit('refresh')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '执行失败'
  } finally {
    busy.value = false
  }
}
function runOne(a: Account) {
  if (today(a.last_run_at) && !today(a.last_success_at)) retrying.value = a
  else void run([a.id])
}
async function toggle(a: Account) {
  try {
    await request('/accounts/' + a.id, 'PUT', {
      name: a.name,
      site_id: a.site_id,
      enabled: !a.enabled,
      schedule: a.schedule,
      version: a.version,
    })
    emit('refresh')
  } catch (e) {
    error.value = (e as Error).message
  }
}
async function remove() {
  if (!deleting.value) return
  busy.value = true
  try {
    await request('/accounts/' + deleting.value.id, 'DELETE')
    deleting.value = null
    emit('refresh')
    emit('notify', '账号已删除，历史记录已保留')
  } catch (e) {
    error.value = (e as Error).message
    deleting.value = null
  } finally {
    busy.value = false
  }
}
async function login(a: Account) {
  loginBusy.value = a.id
  error.value = ''
  try {
    const info =
      a.active_status === 'waiting_user'
        ? await request<LoginInfo>('/sessions/' + a.active_run_id)
        : await request<LoginInfo>('/accounts/' + a.id + '/login', 'POST')
    emit('login', info)
    emit('refresh')
  } catch (e) {
    error.value = (e as Error).message
    emit('refresh')
  } finally {
    loginBusy.value = ''
  }
}
function accountSaved() {
  editing.value = undefined
  emit('refresh')
  emit('notify', '账号已保存')
}
function checkAll(event: Event) {
  selected.value = (event.target as HTMLInputElement).checked ? filtered.value.map((a) => a.id) : []
}
</script>
<template>
  <div class="section-heading">
    <div>
      <h2>
        我的账号 <span class="count">{{ accounts.length }}</span>
      </h2>
      <p>一次配置，把每天的签到交给计划。</p>
    </div>
    <div class="actions">
      <button class="button secondary" :disabled="busy || !accounts.length" @click="run(selected)">
        <Icon name="play" />{{
          selected.length ? '执行所选 (' + selected.length + ')' : '全部签到'
        }}</button
      ><button class="button primary" :disabled="!sites.length" @click="editing = null">
        <Icon name="plus" />添加账号
      </button>
    </div>
  </div>
  <div v-if="error" class="notice error" role="alert">
    {{ error
    }}<button class="icon-button" aria-label="关闭错误提示" @click="error = ''">
      <Icon name="close" />
    </button>
  </div>
  <div class="panel">
    <div class="table-toolbar">
      <div class="filter-tabs">
        <button :class="{ active: filter === 'all' }" @click="filter = 'all'">全部账号</button
        ><button :class="{ active: filter === 'enabled' }" @click="filter = 'enabled'">
          已启用</button
        ><button :class="{ active: filter === 'attention' }" @click="filter = 'attention'">
          需要处理
          <span v-if="accounts.filter(attention).length">{{
            accounts.filter(attention).length
          }}</span>
        </button>
      </div>
      <label class="search"
        ><Icon name="search" /><input
          v-model="search"
          aria-label="搜索账号"
          placeholder="搜索账号或站点"
      /></label>
    </div>
    <div v-if="!accounts.length" class="empty-state">
      <div class="empty-icon"><Icon name="grid" :size="32" /></div>
      <h3>从第一个账号开始</h3>
      <p>选择站点、添加账号，再绑定登录会话。<br />每天早晨，程序会按各自的随机时间执行。</p>
      <button class="button primary" :disabled="!sites.length" @click="editing = null">
        <Icon name="plus" />添加第一个账号
      </button>
    </div>
    <div v-else class="table-scroll">
      <table class="account-table">
        <thead>
          <tr>
            <th class="check-cell">
              <input
                type="checkbox"
                aria-label="选择当前列表全部账号"
                :checked="filtered.length > 0 && filtered.every((a) => selected.includes(a.id))"
                @change="checkAll"
              />
            </th>
            <th>账号 / 站点</th>
            <th>签到状态</th>
            <th>下次计划 <span class="muted">北京时间</span></th>
            <th>自动</th>
            <th class="align-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in filtered" :key="a.id" :class="{ 'row-disabled': !a.enabled }">
            <td class="check-cell">
              <input
                v-model="selected"
                type="checkbox"
                :value="a.id"
                :aria-label="'选择 ' + a.name"
              />
            </td>
            <td>
              <div class="account-identity">
                <span class="avatar" :class="siteMap.get(a.site_id)?.adapter">{{
                  a.name.charAt(0).toUpperCase()
                }}</span>
                <div>
                  <strong>{{ a.name }}</strong
                  ><span class="subline">{{ siteMap.get(a.site_id)?.name || '站点已删除' }}</span>
                </div>
              </div>
            </td>
            <td>
              <StatusBadge :status="a.active_status || a.last_status" /><span
                class="subline truncate"
                :title="a.last_message"
                >{{ !hasAuth(a) ? '等待绑定登录' : a.last_message || '计划就绪' }}</span
              >
            </td>
            <td>
              <strong class="time-value">{{
                !a.enabled ? '已暂停' : a.next_run_at ? time(a.next_run_at) : '等待人工处理'
              }}</strong
              ><span class="subline">{{ scheduleLabel(a.schedule) }} · 每日随机</span>
            </td>
            <td>
              <button
                class="toggle"
                :class="{ on: a.enabled }"
                role="switch"
                :aria-checked="a.enabled"
                :aria-label="'自动签到 ' + a.name"
                :disabled="!!a.active_run_id"
                @click="toggle(a)"
              >
                <span />
              </button>
            </td>
            <td>
              <div class="row-actions">
                <button
                  class="small-button"
                  :aria-label="'签到 ' + a.name"
                  :disabled="busy || !!a.active_run_id || !a.enabled || today(a.last_success_at)"
                  @click="runOne(a)"
                >
                  <Icon name="play" :size="14" />{{
                    today(a.last_run_at) && !today(a.last_success_at) ? '重试' : '签到'
                  }}</button
                ><button
                  class="icon-button"
                  :aria-label="'浏览器登录 ' + a.name"
                  title="浏览器登录"
                  :disabled="
                    !browserAvailable ||
                    !!loginBusy ||
                    (!!a.active_run_id && a.active_status !== 'waiting_user')
                  "
                  @click="login(a)"
                >
                  <Icon
                    :name="loginBusy === a.id ? 'refresh' : 'login'"
                    :class="{ spinning: loginBusy === a.id }"
                  /></button
                ><button
                  class="icon-button"
                  :aria-label="'编辑 ' + a.name"
                  title="编辑账号"
                  :disabled="!!a.active_run_id"
                  @click="editing = a"
                >
                  <Icon name="edit" /></button
                ><button
                  class="icon-button danger"
                  :aria-label="'删除 ' + a.name"
                  title="删除账号"
                  :disabled="!!a.active_run_id"
                  @click="deleting = a"
                >
                  <Icon name="trash" />
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!filtered.length">
            <td colspan="6" class="empty-row">没有匹配的账号</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="panel-footer">
      <span><Icon name="shield" :size="14" />账号使用独立登录会话</span
      ><span>确认成功后，当天停止执行</span>
    </div>
  </div>
  <AccountForm
    v-if="editing !== undefined"
    :account="editing"
    :sites="sites"
    :templates="templates"
    @close="editing = undefined"
    @saved="accountSaved"
  />
  <Modal v-if="deleting" title="删除账号" :busy="busy" @close="deleting = null"
    ><p>删除“{{ deleting.name }}”及其登录凭据。已生成的历史记录会保留。</p>
    <template #footer
      ><button class="button secondary" :disabled="busy" @click="deleting = null">取消</button
      ><button class="button danger-button" :disabled="busy" @click="remove">
        删除账号
      </button></template
    ></Modal
  >
  <Modal v-if="retrying" title="确认手动重试" :busy="busy" @close="retrying = null"
    ><p>
      “{{
        retrying.name
      }}”今天已经尝试过签到。请先核对站点是否已经成功；本次操作会再次尝试登录与签到。
    </p>
    <div class="notice info">
      {{ retrying.last_message || '自动重试预算之外的操作需要手动确认。' }}
    </div>
    <template #footer
      ><button class="button secondary" :disabled="busy" @click="retrying = null">取消</button
      ><button class="button primary" :disabled="busy" @click="run([retrying.id], true)">
        确认重试一次
      </button></template
    ></Modal
  >
</template>
