<script setup lang="ts">
// Shown under the header while the member is timed out: the time that is
// left, counting down, and the moderator's reason. When it reaches zero the
// permissions are fetched again, which gives the member their rights back.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { Clock } from '@lucide/vue'
import { usePermissionsStore } from '../stores/permissions'
import { formatRemaining } from '../lib/moderation'

const permissions = usePermissionsStore()
const now = ref(Date.now())
let tick: ReturnType<typeof setInterval> | undefined

const remaining = computed(() => (permissions.timeoutUntil === null ? 0 : permissions.timeoutUntil - now.value))
const active = computed(() => remaining.value > 0)

function stop() {
  clearInterval(tick)
  tick = undefined
}

watch(() => permissions.timeoutUntil, until => {
  stop()
  now.value = Date.now()
  if (until === null || until <= now.value) return
  tick = setInterval(() => {
    now.value = Date.now()
    if (permissions.timeoutUntil !== null && permissions.timeoutUntil <= now.value) {
      stop()
      void permissions.load()
    }
  }, 1000)
}, { immediate: true })

onBeforeUnmount(stop)
</script>

<template>
  <div
    v-if="active"
    role="status"
    data-testid="timeout-banner"
    class="flex h-10 shrink-0 items-center gap-2.5 border-b border-mnema-warning/35 bg-mnema-warning/10 pl-4 pr-3 text-sm"
  >
    <Clock class="h-4 w-4 shrink-0 text-mnema-warning" />
    <span class="min-w-0 truncate text-mnema-text">
      {{ $t('moderation.bannerText') }}
      <span v-if="permissions.timeoutReason" class="text-mnema-muted">{{ permissions.timeoutReason }}</span>
    </span>
    <span data-testid="timeout-remaining" class="ml-auto font-mono text-xs tabular-nums text-mnema-text">{{ formatRemaining(remaining) }}</span>
  </div>
</template>
