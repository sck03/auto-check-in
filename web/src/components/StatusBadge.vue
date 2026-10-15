<script setup lang="ts">
import { statusLabels } from '../format'
defineProps<{ status: string }>()
</script>
<template>
  <span class="status-badge" :class="'status-' + status"
    ><span class="status-dot" />{{ statusLabels[status] || status }}</span
  >
</template>
