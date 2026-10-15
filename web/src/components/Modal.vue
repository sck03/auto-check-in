<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Icon from './Icon.vue'
const props = defineProps<{ title: string; wide?: boolean; busy?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement>()
onMounted(() => dialog.value?.showModal())
function close() {
  if (!props.busy) emit('close')
}
</script>
<template>
  <dialog
    ref="dialog"
    class="modal"
    :class="{ wide }"
    aria-labelledby="modal-title"
    @cancel.prevent="close"
  >
    <header class="modal-header">
      <h2 id="modal-title">{{ title }}</h2>
      <button class="icon-button" aria-label="关闭对话框" :disabled="busy" @click="close">
        <Icon name="close" />
      </button>
    </header>
    <div class="modal-body"><slot /></div>
    <footer v-if="$slots.footer" class="modal-footer"><slot name="footer" /></footer>
  </dialog>
</template>
