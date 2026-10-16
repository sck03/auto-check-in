<script setup lang="ts">
import { computed, ref } from 'vue'
import { request } from '../api'
import type { Account, Site, Template } from '../types'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
const props = defineProps<{ sites: Site[]; templates: Template[]; accounts: Account[] }>()
const emit = defineEmits<{ refresh: []; notify: [string] }>()
const form = ref<Site | null>(null),
  isNew = ref(false),
  busy = ref(false),
  error = ref('')
const deleting = ref<Site | null>(null)
const templateMap = computed(() => new Map(props.templates.map((t) => [t.id, t])))
const templateGroups = computed(() => [
  { label: '专用站点', items: props.templates.filter((t) => t.site.base_url) },
  { label: '通用模板', items: props.templates.filter((t) => !t.site.base_url) },
])
const currentTemplate = computed(() => templateMap.value.get(form.value?.template_id || ''))
const accountCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const account of props.accounts)
    counts.set(account.site_id, (counts.get(account.site_id) || 0) + 1)
  return counts
})
const count = (id: string) => accountCounts.value.get(id) || 0
const attached = computed(() => (form.value && !isNew.value ? count(form.value.id) : 0))
const hasOAuth = computed(
  () =>
    !isNew.value &&
    props.accounts.some(
      (a) => a.site_id === form.value?.id && ['github', 'linuxdo'].includes(a.auth.method),
    ),
)
const copySite = (site: Site): Site => ({ ...site, selectors: { ...site.selectors } })
function add(template = 'newapi') {
  const source = templateMap.value.get(template)
  if (!source) return
  form.value = { ...copySite(source.site), id: '', version: 0 }
  isNew.value = true
  error.value = ''
}
function edit(site: Site) {
  form.value = copySite(site)
  isNew.value = false
  error.value = ''
}
function templateChange(event: Event) {
  const source = templateMap.value.get((event.target as HTMLSelectElement).value)
  const old = form.value
  if (!source || !old || attached.value) return
  const previous = currentTemplate.value?.site
  form.value = {
    ...copySite(source.site),
    id: old.id,
    version: old.version,
    name: old.name && old.name !== previous?.name ? old.name : source.site.name,
    base_url: source.site.base_url || (old.base_url !== previous?.base_url ? old.base_url : ''),
  }
  error.value = ''
}
async function save() {
  if (!form.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await request(
      '/sites' + (isNew.value ? '' : '/' + form.value.id),
      isNew.value ? 'POST' : 'PUT',
      form.value,
    )
    form.value = null
    emit('refresh')
    emit('notify', '站点已保存')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
async function remove() {
  if (!deleting.value) return
  busy.value = true
  try {
    await request('/sites/' + deleting.value.id, 'DELETE')
    deleting.value = null
    emit('refresh')
  } catch (e) {
    error.value = (e as Error).message
    deleting.value = null
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="section-heading">
    <div>
      <h2>站点管理</h2>
      <p>同类站点使用模板，不同账号共享站点配置。</p>
    </div>
    <button class="button primary" :disabled="!templates.length" @click="add()">
      <Icon name="plus" />添加站点
    </button>
  </div>
  <div v-if="error && !form" class="notice error" role="alert">{{ error }}</div>
  <div class="site-grid">
    <article v-for="site in sites" :key="site.id" class="panel site-card">
      <div class="site-card-top">
        <span class="site-icon" :class="site.adapter"><Icon name="globe" :size="24" /></span
        ><span class="tag">{{ templateMap.get(site.template_id)?.site.name || site.adapter }}</span>
      </div>
      <h3>{{ site.name }}</h3>
      <a :href="site.base_url" target="_blank" rel="noopener noreferrer" class="site-url"
        >{{ site.base_url }} <Icon name="external" :size="13"
      /></a>
      <div class="site-meta">
        <span>{{ site.transport === 'browser' ? '浏览器执行' : '接口执行' }}</span
        ><span>{{ count(site.id) }} 个账号</span>
      </div>
      <div class="site-card-footer">
        <button class="text-button" @click="edit(site)">
          <Icon name="edit" :size="15" />编辑站点</button
        ><button
          class="icon-button danger"
          :disabled="!!count(site.id)"
          :aria-label="'删除站点 ' + site.name"
          title="无账号引用时可删除"
          @click="deleting = site"
        >
          <Icon name="trash" :size="16" />
        </button>
      </div>
    </article>
    <button class="add-site-card" :disabled="!templates.length" @click="add()">
      <Icon name="plus" :size="26" /><strong>接入更多站点</strong
      ><span>New API 模板或自定义页面流程</span>
    </button>
  </div>
  <div class="notice info">
    <Icon
      name="info"
    />网站需要提供可用的签到功能；页面或接口变化时，可在站点配置中调整。首次接入请先用一个账号验证。
  </div>
  <Modal v-if="form" :title="isNew ? '添加站点' : '编辑站点'" :busy="busy" @close="form = null">
    <form id="site-form" class="form-stack" @submit.prevent="save">
      <div v-if="error" class="notice error" role="alert">{{ error }}</div>
      <label
        >平台模板<select :value="form.template_id" :disabled="!!attached" @change="templateChange">
          <optgroup v-for="group in templateGroups" :key="group.label" :label="group.label">
            <option v-for="template in group.items" :key="template.id" :value="template.id">
              {{ template.site.name }}
            </option>
          </optgroup>
        </select></label
      >
      <p class="field-help">{{ currentTemplate?.description }}</p>
      <label
        >站点名称<input
          v-model="form.name"
          required
          maxlength="60"
          placeholder="例如：我的 New API 站点"
      /></label>
      <label
        >站点地址<input
          v-model="form.base_url"
          type="url"
          required
          :disabled="!!attached"
          placeholder="https://example.com"
      /></label>
      <p v-if="attached" class="field-help">
        已有账号使用此站点。更换地址或平台类型时，请新增站点。
      </p>
      <label
        >执行方式<select v-model="form.transport" :disabled="form.adapter === 'browser'">
          <option value="http" :disabled="hasOAuth">HTTP 接口（轻量）</option>
          <option value="browser">浏览器（需要 Chrome / Edge / Chromium）</option>
        </select></label
      >
      <p v-if="hasOAuth" class="field-help">此站点的 OAuth 账号需要浏览器执行方式。</p>
      <details class="advanced" :open="form.adapter === 'browser'">
        <summary>接口路径与页面选择器</summary>
        <div class="field-grid">
          <label>登录页面<input v-model="form.login_path" required /></label
          ><label>登录接口<input v-model="form.login_api_path" required /></label>
          <label
            >{{ form.adapter === 'browser' ? '签到页面' : '签到接口'
            }}<input
              v-model="form.checkin_path"
              :disabled="['agentrouter', 'anyrouter'].includes(form.adapter)"
          /></label>
          <label>用户信息接口<input v-model="form.user_info_path" required /></label
          ><label>用户 ID 请求头<input v-model="form.user_header" required /></label>
        </div>
        <template v-if="form.transport === 'browser'">
          <label
            >邮箱登录入口选择器 <span class="optional">可选</span
            ><input v-model="form.selectors.login_entry"
          /></label>
          <div class="field-grid">
            <label>用户名输入框<input v-model="form.selectors.username" /></label
            ><label>密码输入框<input v-model="form.selectors.password" /></label>
          </div>
          <label>提交登录按钮<input v-model="form.selectors.submit" /></label>
          <div class="field-grid">
            <label>GitHub 登录按钮<input v-model="form.selectors.github" /></label
            ><label>Linux DO 登录按钮<input v-model="form.selectors.linuxdo" /></label>
          </div>
        </template>
        <template v-if="form.adapter === 'browser'">
          <label
            >签到按钮 CSS 选择器<input
              v-model="form.selectors.checkin"
              required
              placeholder="例如 #checkin-button"
          /></label>
          <label
            >成功结果 CSS 选择器<input
              v-model="form.selectors.success"
              required
              placeholder="例如 .checkin-success"
          /></label>
          <label>已签到标识 CSS 选择器<input v-model="form.selectors.already" /></label>
        </template>
      </details>
      <details class="advanced">
        <summary>额度显示</summary>
        <div class="field-grid">
          <label>显示单位<input v-model="form.unit" required maxlength="16" /></label
          ><label
            >每单位对应额度<input
              v-model.number="form.quota_per_unit"
              type="number"
              min="1"
              required
          /></label>
        </div>
        <p class="field-help">仅用于显示站点返回的金额；没有奖励字段时显示“未提供”。</p>
      </details>
    </form>
    <template #footer
      ><button class="button secondary" :disabled="busy" @click="form = null">取消</button
      ><button class="button primary" form="site-form" type="submit" :disabled="busy">
        {{ busy ? '保存中…' : '保存站点' }}
      </button></template
    >
  </Modal>
  <Modal v-if="deleting" title="删除站点" :busy="busy" @close="deleting = null"
    ><p>删除“{{ deleting.name }}”的站点配置。</p>
    <template #footer
      ><button class="button secondary" :disabled="busy" @click="deleting = null">取消</button
      ><button class="button danger-button" :disabled="busy" @click="remove">
        删除站点
      </button></template
    ></Modal
  >
</template>
