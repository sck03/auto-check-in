<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { blob, request } from '../api'
import type { LoginInfo } from '../types'
import { time } from '../format'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
const props = defineProps<{ info: LoginInfo }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const screenshot = ref(''),
  error = ref(''),
  text = ref(''),
  showText = ref(false),
  busy = ref(false)
let alive = true,
  timer = 0,
  capturing = false
let actionQueue: Promise<unknown> = Promise.resolve()
async function capture() {
  if (!alive || capturing) return
  if (document.visibilityState !== 'visible') {
    timer = window.setTimeout(capture, 5000)
    return
  }
  capturing = true
  try {
    const data = await blob('/sessions/' + props.info.id + '/screenshot')
    if (!alive) return
    const previous = screenshot.value
    screenshot.value = URL.createObjectURL(data)
    if (previous) URL.revokeObjectURL(previous)
  } catch (e) {
    if (alive) error.value = (e as Error).message
  } finally {
    capturing = false
    if (alive) timer = window.setTimeout(capture, 1500)
  }
}
async function input(event: Record<string, unknown>) {
  if (busy.value || !alive) return false
  error.value = ''
  const operation = actionQueue.then(async () => {
    if (alive) await request('/sessions/' + props.info.id + '/input', 'POST', event)
  })
  actionQueue = operation.catch(() => undefined)
  try {
    await operation
    return true
  } catch (e) {
    if (alive) error.value = (e as Error).message
    return false
  }
}
function click(event: MouseEvent) {
  const img = event.currentTarget as HTMLImageElement,
    rect = img.getBoundingClientRect()
  void input({
    type: 'click',
    x: ((event.clientX - rect.left) * img.naturalWidth) / rect.width,
    y: ((event.clientY - rect.top) * img.naturalHeight) / rect.height,
  })
  img.focus()
}
async function sendText() {
  if (!text.value) return
  const value = text.value
  text.value = ''
  if (!await input({ type: 'text', text: value }) && !text.value) text.value = value
}
async function save() {
  busy.value = true
  error.value = ''
  try {
    await actionQueue
    await request('/sessions/' + props.info.id + '/save', 'POST')
    emit('saved')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
async function close() {
  busy.value = true
  try {
    await request('/sessions/' + props.info.id, 'DELETE')
  } catch {
    /* The server may already have expired this session. */
  } finally {
    emit('close')
  }
}
function key(event: KeyboardEvent) {
  if (
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
onMounted(capture)
onBeforeUnmount(() => {
  alive = false
  clearTimeout(timer)
  if (screenshot.value) URL.revokeObjectURL(screenshot.value)
})
</script>
<template>
  <Modal title="浏览器登录" wide :busy="busy" @close="close">
    <div class="browser-intro">
      <p>在下方独立浏览器中完成登录或验证码，返回站点后点击“保存会话”。</p>
      <span class="muted">会话有效至 {{ time(info.expires_at) }}</span>
    </div>
    <div v-if="error" class="notice error" role="alert">{{ error }}</div>
    <div class="browser-controls">
      <button class="small-button" @click="input({ type: 'reload' })">
        <Icon name="refresh" :size="14" />刷新网页</button
      ><button
        v-for="keyName in ['Tab', 'Enter', 'Backspace', 'SelectAll']"
        :key="keyName"
        class="small-button"
        @click="input({ type: 'key', key: keyName })"
      >
        {{ { Tab: '下一输入框', Enter: '回车', Backspace: '退格', SelectAll: '全选' }[keyName] }}
      </button>
    </div>
    <div class="browser-viewport">
      <img
        v-if="screenshot"
        :src="screenshot"
        alt="独立登录浏览器，可点击页面进行操作"
        tabindex="0"
        @click="click"
        @keydown="key"
        @wheel.prevent="
          input({ type: 'scroll', delta_y: Math.max(-1500, Math.min(1500, $event.deltaY)) })
        "
      />
      <div v-else class="browser-loading">
        <Icon name="refresh" :size="26" class="spinning" />
        <p>正在读取浏览器画面…</p>
      </div>
    </div>
    <form class="browser-text-entry" @submit.prevent="sendText">
      <label
        >向选中的网页输入框填写文字<input
          v-model="text"
          :type="showText ? 'text' : 'password'"
          autocomplete="off"
          placeholder="先点击网页中的输入框，再在这里输入" /></label
      ><button class="button secondary" type="submit" :disabled="!text || busy">输入</button
      ><label class="check-label"><input v-model="showText" type="checkbox" />显示</label>
    </form>
    <template #footer
      ><span class="footer-note">此会话仅属于当前账号，关闭后清理临时浏览器。</span
      ><button class="button secondary" :disabled="busy" @click="close">取消登录</button
      ><button class="button primary" :disabled="busy" @click="save">
        {{ busy ? '处理中…' : '保存会话' }}
      </button></template
    >
  </Modal>
</template>
