<script setup lang="ts">
// The notices under the header about the member or the community itself (the
// administrators' lockdown banner rides along below).
// A timeout is shown while the member is timed out: the time that is
// left, counting down, and the moderator's reason. When it reaches zero the
// permissions are fetched again, which gives the member their rights back.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { Clock } from '@lucide/vue'
import { usePermissionsStore } from '../stores/permissions'
import { formatRemaining } from '../lib/moderation'
import { visibleInterval } from '../lib/pageVisibility'
import RaidLockdownBanner from './RaidLockdownBanner.vue'

const permissions = usePermissionsStore()
const now = ref(Date.now())
// Counts only while the page is visible; the catch-up tick on return notices an expiry.
let tick: (() => void) | undefined

const RETRY_MS = 5000
// The server still reports the timeout after the local clock ran past it: the
// local clock is ahead, so the server decides when the banner goes.
const serverHolds = ref(false)
let retry: ReturnType<typeof setTimeout> | undefined

const remaining = computed(() => (permissions.timeoutUntil === null ? 0 : Math.max(0, permissions.timeoutUntil - now.value)))
const active = computed(() => remaining.value > 0 || (serverHolds.value && permissions.timeoutUntil !== null))

function stop() {
  tick?.()
  tick = undefined
  clearTimeout(retry)
  retry = undefined
  serverHolds.value = false
}

async function expire() {
  const until = permissions.timeoutUntil
  await permissions.load()
  if (until === null || permissions.timeoutUntil !== until) return
  serverHolds.value = true
  retry = setTimeout(() => { void expire() }, RETRY_MS)
}

watch(() => permissions.timeoutUntil, until => {
  stop()
  now.value = Date.now()
  if (until === null || until <= now.value) return
  tick = visibleInterval(() => {
    now.value = Date.now()
    if (permissions.timeoutUntil !== null && permissions.timeoutUntil <= now.value) {
      tick?.()
      tick = undefined
      void expire()
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
  <!-- For administrators: a running lockdown -->
  <RaidLockdownBanner />
</template>
