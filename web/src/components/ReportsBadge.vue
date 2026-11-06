<script setup lang="ts">
// A small amber count of the open reports. It renders nothing for a member who
// may not act on reports, and nothing when none are open. The visible number
// is short (99+); screen readers get the whole sentence.
import { computed } from 'vue'
import { useReportsOpenStore } from '../stores/reportsOpen'

const reports = useReportsOpenStore()
const shown = computed(() => (reports.visible > 99 ? '99+' : String(reports.visible)))
</script>

<template>
  <span
    v-if="reports.visible > 0"
    data-testid="reports-badge"
    class="inline-flex min-w-5 shrink-0 items-center justify-center rounded-full bg-mnema-amber/20 px-1.5 text-xs font-semibold leading-5 text-mnema-amber"
  >
    <span aria-hidden="true">{{ shown }}</span>
    <span class="sr-only">{{ $t('reports.openBadge', { count: reports.visible }) }}</span>
  </span>
</template>
