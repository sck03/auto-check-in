<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { request } from '../api'
import type { Account, AccountInput, AuthMethod, CredentialPatch, Site, Template } from '../types'
import Modal from './Modal.vue'
import ScheduleFields from './ScheduleFields.vue'
import Icon from './Icon.vue'
const props = defineProps<{ account: Account | null; sites: Site[]; templates: Template[] }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const selected = props.sites.find((s) => s.id === props.account?.site_id) || props.sites[0]
const defaults = props.templates.find((t) => t.id === selected?.template_id) || props.templates[0]
const form = reactive({
  name: props.account?.name || '',
  site_id: selected?.id || '',
  enabled: props.account?.enabled ?? true,
  method:
    props.account?.auth.method ||
    ((selected?.adapter === 'agentrouter' ? 'github' : 'cookie') as AuthMethod),
  username: props.account?.auth.username || '',
  api_user: props.account?.auth.api_user || '',
  password: '',
  cookie_header: '',
  token: '',
  clear_session: false,
  schedule: { ...(props.account?.schedule || defaults.schedule) },
})
const site = computed(() => props.sites.find((s) => s.id === form.site_id))
const busy = ref(false),
  error = ref('')
function changeSite() {
  const template = props.templates.find((t) => t.id === site.value?.template_id)
  if (template) form.schedule = { ...template.schedule }
  form.method = site.value?.adapter === 'agentrouter' ? 'github' : 'cookie'
}
async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const credentials: CredentialPatch = { method: form.method }
    if (form.username !== (props.account?.auth.username || '')) credentials.username = form.username
    if (form.api_user !== (props.account?.auth.api_user || '')) credentials.api_user = form.api_user
    if (form.password) credentials.password = form.password
    if (form.cookie_header || form.clear_session) credentials.cookie_header = form.cookie_header
    if (form.token || form.clear_session) credentials.token = form.token
    if (form.clear_session && credentials.api_user === undefined) credentials.api_user = ''
    const body: AccountInput = {
      name: form.name,
      site_id: form.site_id,
      enabled: form.enabled,
      schedule: form.schedule,
      credentials,
      version: props.account?.version || 0,
    }
    await request(
      '/accounts' + (props.account ? '/' + props.account.id : ''),
      props.account ? 'PUT' : 'POST',
      body,
    )
    emit('saved')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <Modal :title="account ? '编辑账号' : '添加账号'" :busy="busy" @close="emit('close')">
    <form id="account-form" class="form-stack" @submit.prevent="save">
      <fieldset class="form-fields" :disabled="busy">
        <div v-if="error" role="alert" class="notice error">{{ error }}</div>
        <div class="field-grid">
          <label
            >账号名称<input
              v-model="form.name"
              required
              maxlength="60"
              placeholder="例如：我的主账号"
          /></label>
          <label
            >所属站点<select
              v-model="form.site_id"
              :disabled="!!account"
              required
              @change="changeSite"
            >
              <option v-for="s in sites" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select></label
          >
        </div>
        <label
          >登录方式<select v-model="form.method">
            <option value="cookie">浏览器会话 / Cookie</option>
            <option v-if="site?.adapter !== 'x666'" value="password">用户名与密码</option>
            <option v-if="site?.transport === 'browser' && site.adapter !== 'x666'" value="github">
              GitHub 授权登录
            </option>
            <option v-if="site?.transport === 'browser' && site.adapter !== 'x666'" value="linuxdo">
              Linux DO 授权登录
            </option>
          </select></label
        >
        <div v-if="site?.adapter === 'x666'" class="notice info">
          薄荷每日签到在 up.x666.me 完成。保存账号后点击“浏览器登录”，在该页面用 LinuxDO
          登录并保存会话；也可在更多凭据选项填入该页面的
          userToken。每日免费抽奖奖励直接到账，无需手动兑换。
        </div>
        <div v-if="site?.adapter === 'agentrouter'" class="notice info">
          AgentRouter 通过重新登录领取签到奖励，请使用密码或 OAuth 登录。首次 OAuth
          登录需要在“浏览器登录”中完成授权。
        </div>
        <div v-if="form.method === 'password'" class="field-grid">
          <label
            >用户名或邮箱<input v-model="form.username" required autocomplete="off" maxlength="320"
          /></label>
          <label
            >密码<input
              v-model="form.password"
              type="password"
              :required="!account?.auth.has_password"
              autocomplete="new-password"
              :placeholder="account?.auth.has_password ? '已保存，留空保留' : '输入登录密码'"
          /></label>
        </div>
        <p v-if="form.method === 'password'" class="field-help">
          一般无需先手动登录。程序优先复用有效会话，失效时使用密码登录；遇到验证码或二次验证，请点击“浏览器登录”完成后保存。OAuth
          注册账号需先确认已设置本站密码。
        </p>
        <template v-if="form.method === 'cookie'">
          <p class="field-help">
            可以先保存账号，再点击“浏览器登录”绑定会话；也可以粘贴已有 Cookie。
          </p>
          <label
            >Cookie <span class="optional">选填</span
            ><textarea
              v-model="form.cookie_header"
              rows="3"
              spellcheck="false"
              :placeholder="
                account?.auth.has_session ? '已有加密会话；留空保留' : 'session=...; other=value'
              "
            />
          </label>
          <label
            >API 用户 ID <span class="optional">浏览器登录可自动获取</span
            ><input
              v-model="form.api_user"
              autocomplete="off"
              placeholder="例如 12345"
              maxlength="64"
          /></label>
        </template>
        <p v-if="form.method === 'github' || form.method === 'linuxdo'" class="field-help">
          保存后点击“浏览器登录”，在独立会话中选择
          {{ form.method === 'github' ? 'GitHub' : 'LinuxDo' }} 完成授权。以后按此方式自动登录；
          无需提前在平时使用的浏览器中登录。
        </p>
        <details class="advanced">
          <summary>更多凭据选项</summary>
          <label
            >{{ site?.adapter === 'x666' ? '签到页面 userToken' : '用户访问令牌' }}
            <span class="optional">可选，非模型 API Key</span
            ><input
              v-model="form.token"
              type="password"
              autocomplete="off"
              :placeholder="
                account?.auth.has_token ? '已保存，留空保留' : '仅适用于支持此认证方式的站点'
              "
          /></label>
          <label v-if="account" class="check-label"
            ><input v-model="form.clear_session" type="checkbox" />清除旧会话，重新建立登录</label
          >
        </details>
        <div class="form-section">
          <h3>每日计划</h3>
          <ScheduleFields v-model="form.schedule" />
        </div>
        <label class="check-label"
          ><input v-model="form.enabled" type="checkbox" />启用此账号的自动签到</label
        >
        <p class="privacy-note">
          <Icon name="shield" :size="15" />凭据加密保存在运行程序的设备中，列表不会显示密码或
          Cookie。
        </p>
      </fieldset>
    </form>
    <template #footer
      ><button class="button secondary" :disabled="busy" @click="emit('close')">取消</button
      ><button class="button primary" form="account-form" type="submit" :disabled="busy">
        {{ busy ? '保存中…' : '保存账号' }}
      </button></template
    >
  </Modal>
</template>
