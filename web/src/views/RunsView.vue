<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import { request } from '../api'
import { quota, statusLabels, time } from '../format'
import type { Account, Run, RunPage } from '../types'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import StatusBadge from '../components/StatusBadge.vue'
const props = defineProps<{ accounts: Account[]; revision: number }>()
const page = ref<RunPage>({ items: [], total: 0 })
const account = ref(''),
  status = ref(''),
  action = ref(''),
  offset = ref(0),
  error = ref('')
const detail = ref<Run | null>(null)
let controller: AbortController | undefined
let activeQuery = ''
let pendingRefresh = false
async function load() {
  const params = new URLSearchParams({
    account_id: account.value,
    status: status.value,
    action: action.value,
    limit: '30',
    offset: String(offset.value),
  }).toString()
  if (controller && activeQuery === params) {
    pendingRefresh = true
    return
  }
  controller?.abort()
  const current = new AbortController()
  controller = current
  activeQuery = params
  pendingRefresh = false
  try {
    const data = await request<RunPage>('/runs?' + params, 'GET', undefined, {
      signal: current.signal,
    })
    if (!current.signal.aborted) {
      page.value = data
      if (detail.value)
        detail.value = data.items.find((run) => run.id === detail.value?.id) || detail.value
      error.value = ''
    }
  } catch (e) {
    if (!current.signal.aborted) error.value = (e as Error).message
  } finally {
    if (controller === current) {
      controller = undefined
      if (pendingRefresh) void load()
    }
  }
}
async function cancel(run: Run) {
  try {
    await request('/runs/' + run.id, 'DELETE')
  } catch (e) {
    error.value = (e as Error).message
  }
}
watch(
  [account, status, action],
  () => {
    offset.value = 0
  },
  { flush: 'sync' },
)
watch([account, status, action, offset, () => props.revision], load, { immediate: true })
onScopeDispose(() => {
  controller?.abort()
  controller = undefined
  pendingRefresh = false
})
const duration = (r: Run) =>
  r.started_at && r.finished_at
    ? Math.max(0, (Date.parse(r.finished_at) - Date.parse(r.started_at)) / 1000).toFixed(1) + ' 秒'
    : '—'
const evidence: Record<string, string> = {
  provider_acknowledgement: '站点业务响应确认',
  provider_already_done: '站点明确提示已签到',
  checked_in_false: '登录响应明确表示未重复发奖',
  configured_page_state: '页面结果标识',
  manual_session: '用户保存的登录会话',
  status_api: '签到状态接口复核',
  invalid_status: '签到状态响应无法确认，未提交签到',
  persistence_error: '本地结果保存失败，需要核对站点',
  process_recovery: '上次操作中断后恢复',
  recognized_page_state: '识别到明确的页面签到结果',
  authenticated_visit: '已登录访问（AnyRouter 规则）',
  fresh_login: '本次重新登录（AgentRouter 规则）',
  daily_spin_status: '今日免费抽奖记录确认',
  daily_spin_acknowledgement: '每日免费抽奖响应确认',
  manual_required: '需要人工处理登录或页面验证',
  challenge_required: '站点返回真人验证页面',
  session_invalid: '登录会话验证失败',
  two_factor_required: '站点要求二次验证',
  login_rejected: '站点拒绝登录',
  login_unverified: '登录响应无法确认',
  provider_rejected: '站点拒绝签到',
  http_status: 'HTTP 响应状态',
  invalid_response: '接口响应格式不符',
  missing_acknowledgement: '缺少业务成功字段',
  login_without_checkin_proof: '缺少登录身份或签到证据',
  page_result_missing: '未出现页面结果标识',
  uncertain_request: '请求已发出，结果未确认',
  timeout: '执行超时',
  transport_error: '网络或浏览器执行异常',
  cancelled_context: '任务被中断',
  before_reset: '尚未到每日 08:00',
  schedule_deferred: '计划已过期，已安排下次执行',
  rate_limit_deferred: '站点冷却时间尚未结束，已延后计划',
  login_required: '此站需要重新登录',
}
const stages: Record<string, string> = {
  prepare: '执行前检查',
  open_session: '打开浏览器 / 建立连接',
  session_check: '检查已保存的登录会话',
  status_check: '检查今日签到状态',
  login: '提交登录 / OAuth 授权',
  checkin: '执行并确认签到',
}
</script>
<template>
  <div class="section-heading">
    <div>
      <h2>执行记录</h2>
      <p>查看每次尝试的结果、奖励与后续处理。</p>
    </div>
    <button class="button secondary" @click="load"><Icon name="refresh" />刷新记录</button>
  </div>
  <div v-if="error" role="alert" class="notice error">{{ error }}</div>
  <div class="panel">
    <div class="history-filters">
      <label
        >账号<select v-model="account">
          <option value="">全部账号</option>
          <option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select></label
      ><label
        >状态<select v-model="status">
          <option value="">全部状态</option>
          <option v-for="(name, key) in statusLabels" :key="key" :value="key">{{ name }}</option>
        </select></label
      ><label
        >动作<select v-model="action">
          <option value="">全部动作</option>
          <option value="checkin">签到</option>
          <option value="login">人工登录</option>
        </select></label
      >
    </div>
    <div v-if="!page.items.length" class="empty-state">
      <div class="empty-icon"><Icon name="clock" :size="30" /></div>
      <h3>还没有执行记录</h3>
      <p>签到或登录后，结果会出现在这里。</p>
    </div>
    <div v-else class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>账号 / 时间</th>
            <th>动作</th>
            <th>结果</th>
            <th>确认奖励</th>
            <th>用时</th>
            <th class="align-right">详情</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="run in page.items" :key="run.id">
            <td>
              <strong>{{ run.account_name }}</strong
              ><span class="subline">{{ time(run.created_at, true) }} · {{ run.site_name }}</span>
            </td>
            <td>
              {{ run.action === 'login' ? '人工登录' : '签到'
              }}<span class="subline"
                >{{
                  run.trigger === 'scheduled_retry'
                    ? '自动重试'
                    : run.trigger === 'scheduled'
                      ? '每日计划'
                      : run.trigger === 'manual_retry'
                        ? '手动重试'
                        : '手动触发'
                }}
                · 第 {{ run.attempt }} 次</span
              >
            </td>
            <td>
              <StatusBadge :status="run.status" /><span
                class="subline truncate"
                :title="run.result.message"
                >{{ run.result.message || '等待执行' }}</span
              >
            </td>
            <td class="reward">
              {{
                run.action === 'checkin' && run.status === 'succeeded'
                  ? quota(run.result.reward, run.result.unit, run.result.quota_per_unit)
                  : '—'
              }}
            </td>
            <td class="muted">{{ duration(run) }}</td>
            <td class="align-right">
              <button v-if="run.status === 'queued'" class="text-button" @click="cancel(run)">
                取消排队</button
              ><button
                class="icon-button"
                :aria-label="'查看记录 ' + run.account_name"
                @click="detail = run"
              >
                <Icon name="arrow" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>共 {{ page.total }} 条记录 · 北京时间</span>
      <div class="actions">
        <button
          class="small-button"
          :disabled="offset === 0"
          @click="offset = Math.max(0, offset - 30)"
        >
          上一页</button
        ><span>{{ Math.floor(offset / 30) + 1 }}</span
        ><button class="small-button" :disabled="offset + 30 >= page.total" @click="offset += 30">
          下一页
        </button>
      </div>
    </div>
  </div>
  <Modal v-if="detail" title="执行详情" @close="detail = null">
    <div class="detail-heading">
      <h3>{{ detail.account_name }}</h3>
      <StatusBadge :status="detail.status" />
    </div>
    <p class="result-message" role="status">{{ detail.result.message || '任务尚未结束' }}</p>
    <div v-if="detail.result.advice" class="notice info">{{ detail.result.advice }}</div>
    <dl class="details-list">
      <dt>站点</dt>
      <dd>{{ detail.site_name }}</dd>
      <dt>开始时间</dt>
      <dd>{{ time(detail.started_at, true) }}</dd>
      <dt>结束时间</dt>
      <dd>{{ time(detail.finished_at, true) }}</dd>
      <template v-if="detail.result.stage">
        <dt>执行阶段</dt>
        <dd>{{ stages[detail.result.stage] || detail.result.stage }}</dd>
      </template>
      <template v-if="detail.result.http_status">
        <dt>HTTP 状态</dt>
        <dd>{{ detail.result.http_status }}</dd>
      </template>
      <dt>确认依据</dt>
      <dd>
        {{
          evidence[detail.result.evidence] ||
          detail.result.evidence ||
          '旧记录未提供，请重新执行获取详情'
        }}
      </dd>
      <dt>确认奖励</dt>
      <dd>{{ quota(detail.result.reward, detail.result.unit, detail.result.quota_per_unit) }}</dd>
      <dt>执行前余额</dt>
      <dd>
        {{ quota(detail.result.balance_before, detail.result.unit, detail.result.quota_per_unit) }}
      </dd>
      <dt>执行后余额</dt>
      <dd>
        {{ quota(detail.result.balance_after, detail.result.unit, detail.result.quota_per_unit) }}
      </dd>
    </dl>
    <div class="notice info">
      余额可能同时受到消费或充值影响，余额差额不会计为签到奖励。站点未提供奖励金额时保留为未知。
    </div>
  </Modal>
</template>
