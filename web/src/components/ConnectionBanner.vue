<script setup lang="ts">
// Shown under the header while the live connection is down, after it has been
// up at least once. Turns into a short "reconnected" note afterwards.
import { ref, computed, watch, onBeforeUnmount, onMounted } from 'vue'
import { WifiOff, RefreshCw, Check } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { visibleInterval } from '../lib/pageVisibility'

const chatStore = useChatStore()

// The browser knows the network is gone long before a dead socket is noticed
// (a TCP connection can take close to a minute to fail), so its offline state
// shows the banner at once.
const browserOffline = ref(typeof navigator !== 'undefined' && navigator.onLine === false)
const goOffline = () => { browserOffline.value = true }
const goOnline = () => { browserOffline.value = false }
onMounted(() => {
  window.addEventListener('offline', goOffline)
  window.addEventListener('online', goOnline)
})

const lost = computed(() => browserOffline.value || (chatStore.wasConnected && !chatStore.isConnected))
const justReconnected = ref(false)
const now = ref(Date.now())
let tick: (() => void) | undefined
let hideTimer: ReturnType<typeof setTimeout> | undefined

const secondsLeft = computed(() => Math.max(0, Math.ceil((chatStore.nextRetryAt - now.value) / 1000)))

watch(lost, (isLost, wasLost) => {
  if (isLost) {
    justReconnected.value = false
    clearTimeout(hideTimer)
    now.value = Date.now()
    tick?.()
    tick = visibleInterval(() => { now.value = Date.now() }, 1000)
  } else {
    tick?.()
    tick = undefined
    if (wasLost && chatStore.isConnected) {
      justReconnected.value = true
      hideTimer = setTimeout(() => { justReconnected.value = false }, 2000)
    }
  }
}, { immediate: true })

onBeforeUnmount(() => {
  window.removeEventListener('offline', goOffline)
  window.removeEventListener('online', goOnline)
  tick?.()
  clearTimeout(hideTimer)
})
</script>

<template>
  <div
    v-if="lost"
    role="status"
    data-testid="connection-banner"
    class="flex h-10 shrink-0 items-center gap-2.5 border-b border-mnema-warning/35 bg-mnema-warning/10 pl-4 pr-3 text-sm"
  >
    <WifiOff class="h-4 w-4 shrink-0 text-mnema-warning" />
    <span class="min-w-0 truncate text-mnema-text">{{ $t(browserOffline ? 'connection.offline' : 'connection.lost') }}</span>
    <span v-if="chatStore.reconnectAttempt > 0" class="hidden truncate font-mono text-xs tabular-nums text-mnema-tertiary sm:inline">
      {{ $t('connection.nextTry', { seconds: secondsLeft, attempt: chatStore.reconnectAttempt }) }}
    </span>
    <span class="flex-1"></span>
    <button
      type="button"
      data-testid="connection-retry"
      class="flex h-7 shrink-0 items-center gap-2 rounded-md px-3 text-sm font-semibold text-mnema-text transition hover:bg-mnema-hover"
      @click="chatStore.retryNow()"
    >
      <RefreshCw class="h-3.5 w-3.5" />{{ $t('connection.retryNow') }}
    </button>
  </div>
  <div
    v-else-if="justReconnected"
    role="status"
    data-testid="connection-restored"
    class="flex h-8 shrink-0 items-center gap-2 bg-mnema-accent-subtle px-4 text-sm font-medium text-mnema-accent-hover"
  >
    <Check class="h-[15px] w-[15px]" />{{ $t('connection.restored') }}
  </div>
</template>
