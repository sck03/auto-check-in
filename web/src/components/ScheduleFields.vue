<script setup lang="ts">
import type { Schedule } from '../types'
const props = defineProps<{ modelValue: Schedule }>()
const emit = defineEmits<{ 'update:modelValue': [Schedule] }>()
function update(key: keyof Schedule, value: string | number) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
</script>
<template>
  <div class="field-grid">
    <label
      >随机窗口开始<input
        type="time"
        required
        :value="modelValue.window_start"
        @input="update('window_start', ($event.target as HTMLInputElement).value)"
    /></label>
    <label
      >随机窗口结束<input
        type="time"
        required
        :value="modelValue.window_end"
        @input="update('window_end', ($event.target as HTMLInputElement).value)"
    /></label>
    <label
      >失败补签截止<input
        type="time"
        required
        :value="modelValue.retry_until"
        @input="update('retry_until', ($event.target as HTMLInputElement).value)"
    /></label>
  </div>
  <p class="field-help">
    北京时间，每天成功一次。首次按账号分别随机安排；明确失败才有限重试，成功或已签到后当天停止。
  </p>
</template>
