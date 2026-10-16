<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError, blob, request } from '../api'
import { inputQueue, type BrowserInput } from '../inputQueue'
import type { LoginInfo } from '../types'
import { time } from '../format'
import Modal from './Modal.vue'
import Icon from './Icon.vue'

const props = defineProps<{ info: LoginInfo }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const screenshot = ref(''),
  error = ref(''),
  frameError = ref(''),
  text = ref('')
const showText = ref(false),
  busy = ref(false),
  closing = ref(false),
  pending = ref(0),
  ended = ref(false)
const keyboard = ref<HTMLTextAreaElement>()
const lifetime = new AbortController()
let alive = true,
  timer = 0,
  wheelTimer = 0,
  expiryTimer = 0,
  wheelDelta = 0,
  capturing = false
let requestedFrame = false,
  activeUntil = 0,
  frameFailures = 0,
  composing = false
let frameController: AbortController | undefined
const queue = inputQueue(async (event) => {
  if (!alive) return
  await request('/sessions/' + props.info.id + '/input', 'POST', event, {
    signal: lifetime.signal,
    timeoutMs: 7000,
  })
  refreshSoon()
})

function refreshSoon() {
  if (!alive || props.info.native_window) return
  activeUntil = Date.now() + 1800
  clearTimeout(timer)
  if (capturing) requestedFrame = true
  else timer = window.setTimeout(capture, 30)
}
async function capture() {
  if (!alive || capturing || props.info.native_window) return
  if (document.visibilityState !== 'visible') return
  capturing = true
  requestedFrame = false
  frameController = new AbortController()
  try {
    const data = await blob('/sessions/' + props.info.id + '/screenshot', 'GET', undefined, {
      signal: AbortSignal.any([lifetime.signal, frameController.signal]),
      timeoutMs: 4000,
    })
    if (!alive) return
    const next = URL.createObjectURL(data)
    const decoded = new Image()
    decoded.src = next
    try {
      await decoded.decode()
    } catch (e) {
      URL.revokeObjectURL(next)
      throw e
    }
    if (!alive) {
      URL.revokeObjectURL(next)
      return
    }
    const previous = screenshot.value
    screenshot.value = next
    if (previous) URL.revokeObjectURL(previous)
    frameFailures = 0
    frameError.value = ''
  } catch (e) {
    if (alive && e instanceof ApiError && e.status === 404) expire()
    else if (alive && !frameController.signal.aborted && ++frameFailures >= 3)
      frameError.value = (e as Error).message
  } finally {
    capturing = false
    frameController = undefined
    if (alive && document.visibilityState === 'visible')
      timer = window.setTimeout(capture, requestedFrame ? 0 : Date.now() < activeUntil ? 200 : 1500)
  }
}
async function input(event: BrowserInput) {
  if (busy.value || !alive) return false
  error.value = ''
  pending.value++
  try {
    await queue.push(event)
    return alive
  } catch (e) {
    if (alive) error.value = (e as Error).message
    return false
  } finally {
    pending.value--
  }
}
function click(event: MouseEvent) {
  const img = event.currentTarget as HTMLImageElement
  const rect = img.getBoundingClientRect()
  void input({
    type: 'click',
    x: ((event.clientX - rect.left) * img.naturalWidth) / rect.width,
    y: ((event.clientY - rect.top) * img.naturalHeight) / rect.height,
  })
  keyboard.value?.focus({ preventScroll: true })
}
function flushKeyboard() {
  const entry = keyboard.value
  if (!entry || composing || !entry.value) return
  const value = entry.value
  entry.value = ''
  void input({ type: 'text', text: value })
}
function directText(event: Event) {
  if (!(event as InputEvent).isComposing) flushKeyboard()
}
function compositionEnd() {
  composing = false
  queueMicrotask(flushKeyboard)
}
function key(event: KeyboardEvent) {
  if (event.isComposing || composing) return
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') {
    event.preventDefault()
    void input({ type: 'key', key: 'SelectAll' })
  } else if (
    [
      'Enter',
      'Tab',
      'Backspace',
      'Escape',
      'ArrowUp',
      'ArrowDown',
      'ArrowLeft',
      'ArrowRight',
      'Delete',
    ].includes(event.key)
  ) {
    event.preventDefault()
    void input({ type: 'key', key: event.key })
  }
}
function scroll(event: WheelEvent) {
  wheelDelta = Math.max(-1500, Math.min(1500, wheelDelta + event.deltaY))
  if (wheelTimer) return
  wheelTimer = window.setTimeout(() => {
    wheelTimer = 0
    const delta = wheelDelta
    wheelDelta = 0
    void input({ type: 'scroll', delta_y: delta })
  }, 60)
}
async function sendText() {
  if (!text.value) return
  const value = text.value
  text.value = ''
  if (!(await input({ type: 'text', text: value })) && !text.value && alive) text.value = value
}
async function save() {
  flushKeyboard()
  busy.value = true
  error.value = ''
  try {
    await queue.drain()
    await request('/sessions/' + props.info.id + '/save', 'POST', undefined, {
      signal: lifetime.signal,
      timeoutMs: 15000,
    })
    if (alive) emit('saved')
  } catch (e) {
    if (alive) error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
function stop() {
  alive = false
  lifetime.abort()
  clearTimeout(timer)
  clearTimeout(wheelTimer)
  clearTimeout(expiryTimer)
}
function expire() {
  ended.value = true
  error.value = '登录会话已结束或过期，请关闭后重新打开。'
  stop()
}
function visibility() {
  clearTimeout(timer)
  if (document.visibilityState === 'visible') refreshSoon()
  else frameController?.abort()
}
async function close() {
  if (closing.value) return
  closing.value = true
  stop()
  try {
    await request('/sessions/' + props.info.id, 'DELETE', undefined, { timeoutMs: 12000 })
  } catch {
    // A closed or expired session has already been cleaned up by the service.
  } finally {
    emit('close')
  }
}
onMounted(() => {
  document.addEventListener('visibilitychange', visibility)
  expiryTimer = window.setTimeout(
    expire,
    Math.max(0, Date.parse(props.info.expires_at) - Date.now()),
  )
  if (!props.info.native_window) void capture()
})
onBeforeUnmount(() => {
  stop()
  document.removeEventListener('visibilitychange', visibility)
  if (screenshot.value) URL.revokeObjectURL(screenshot.value)
})
</script>
<template>
  <Modal title="浏览器登录" :wide="!info.native_window" :busy="closing" @close="close">
    <div class="browser-intro">
      <p>
        {{
          info.native_window
            ? '在独立浏览器窗口完成登录，随后回到这里保存会话。'
            : '点击下方页面后可直接输入、粘贴和滚动，完成登录后保存会话。'
        }}
      </p>
      <span class="muted">会话有效至 {{ time(info.expires_at) }}</span>
    </div>
    <div v-if="error || frameError" class="notice error" role="alert">
      {{ error || frameError }}
    </div>
    <div v-if="info.native_window" class="native-login">
      <Icon name="globe" :size="36" />
      <h3>独立登录窗口已打开</h3>
      <p>选择 LinuxDo 或 GitHub 后，在授权页面完成登录和确认。</p>
      <p class="field-help">
        若真人验证反复出现，可取消后重新打开；也可在日常浏览器登录后，在账号编辑中填入本站 Cookie 和
        API 用户 ID。AgentRouter 的每日重新登录仍需密码或 OAuth。
      </p>
      <button
        class="button secondary"
        :disabled="busy || closing || ended"
        @click="input({ type: 'focus' })"
      >
        显示登录窗口
      </button>
    </div>
    <template v-else>
      <div class="browser-controls">
        <button class="small-button" @click="input({ type: 'reload' })">
          <Icon name="refresh" :size="14" />刷新网页
        </button>
        <button
          v-for="keyName in ['Tab', 'Enter', 'Backspace', 'SelectAll']"
          :key="keyName"
          class="small-button"
          @click="input({ type: 'key', key: keyName })"
        >
          {{ { Tab: '下一输入框', Enter: '回车', Backspace: '退格', SelectAll: '全选' }[keyName] }}
        </button>
        <span v-if="pending" class="muted" role="status">正在发送操作…</span>
      </div>
      <form class="browser-text-entry" @submit.prevent="sendText">
        <label
          >向选中的网页输入框填写文字
          <input
            v-model="text"
            :type="showText ? 'text' : 'password'"
            autocomplete="off"
            placeholder="也可在这里填写，再点击“输入”"
          />
        </label>
        <button class="button secondary" type="submit" :disabled="!text || busy">输入</button>
        <label class="check-label"><input v-model="showText" type="checkbox" />显示</label>
      </form>
      <textarea
        ref="keyboard"
        class="browser-keyboard"
        aria-label="登录页面键盘输入"
        autocomplete="off"
        autocapitalize="off"
        :spellcheck="false"
        @input="directText"
        @keydown="key"
        @compositionstart="composing = true"
        @compositionend="compositionEnd"
      ></textarea>
      <div class="browser-viewport">
        <img
          v-if="screenshot"
          :src="screenshot"
          alt="独立登录浏览器，可点击页面进行操作"
          tabindex="0"
          @click="click"
          @focus="keyboard?.focus({ preventScroll: true })"
          @wheel.prevent="scroll"
        />
        <div v-else class="browser-loading">
          <Icon name="refresh" :size="26" class="spinning" />
          <p>正在读取浏览器画面…</p>
        </div>
      </div>
    </template>
    <template #footer>
      <span class="footer-note">此会话仅属于当前账号，结束后清理临时浏览器。</span>
      <button class="button secondary" :disabled="closing" @click="close">取消登录</button>
      <button class="button primary" :disabled="busy || closing || ended" @click="save">
        {{ busy ? '验证并保存中…' : '保存会话' }}
      </button>
    </template>
  </Modal>
</template>
