<script setup lang="ts">
defineProps<{ name: string; size?: number }>()
const paths: Record<string, string> = {
  check: 'M5 12l4 4L19 6',
  grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  globe: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M3 12h18 M12 3c5 5 5 13 0 18-5-5-5-13 0-18',
  clock: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M12 7v5l3 2',
  settings: 'M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6',
  plus: 'M12 5v14 M5 12h14',
  play: 'M8 5l11 7-11 7z',
  refresh: 'M20 7v5h-5 M4 17v-5h5 M6.1 7a7 7 0 0 1 11.5-2L20 8 M4 16l2.4 3a7 7 0 0 0 11.5-2',
  search: 'M11 18a7 7 0 1 1 0-14 7 7 0 0 1 0 14 M16 16l5 5',
  edit: 'M15 5l4 4 M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z',
  login: 'M10 17l5-5-5-5 M3 12h12 M14 3h6v18h-6',
  trash: 'M3 6h18 M9 6V3h6v3 M5 6l1 15h12l1-15 M10 10v7 M14 10v7',
  close: 'M6 6l12 12 M18 6L6 18',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  down: 'M12 3v12 M7 10l5 5 5-5 M4 16v5h16v-5',
  up: 'M12 16V4 M7 9l5-5 5 5 M4 17v4h16v-4',
  shield: 'M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M8 12l3 3 5-6',
  info: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M12 11v6 M12 7h.01',
  external: 'M14 3h7v7 M21 3l-11 11 M10 3H3v18h18v-7',
}
</script>
<template>
  <svg
    :width="size || 18"
    :height="size || 18"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[name] || paths.info" />
  </svg>
</template>
