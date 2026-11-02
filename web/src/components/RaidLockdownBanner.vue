<script setup lang="ts">
// Shown to the Owner and Administrators under the header while a lockdown is
// on, so nobody forgets that the door is shut. It looks at the server when
// the app opens and every minute while the page is visible; ending the
// lockdown is one click here or in the Raid protection tab.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ShieldAlert } from '@lucide/vue'
import { usePermissionsStore } from '../stores/permissions'
import { useRaidStore } from '../stores/raid'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { visibleInterval } from '../lib/pageVisibility'
import { locale, t } from '../i18n'

const permissions = usePermissionsStore()
const raid = useRaidStore()
const toasts = useToastStore()
const busy = ref(false)
const allowed = computed(() => permissions.can('administrator'))
let stop: (() => void) | undefined
let expiry: ReturnType<typeof setTimeout> | undefined
const MAX_DELAY_MS = 2 ** 31 - 1

const until = computed(() => {
  const at = raid.lockdown?.until
  return at ? new Date(at).toLocaleTimeString(locale.value, { timeStyle: 'short' }) : ''
})

function start() {
  stop?.()
  stop = undefined
  if (!allowed.value) return
  void raid.load()
  stop = visibleInterval(() => { void raid.load() }, 60_000, { catchUp: false })
}

async function end() {
  busy.value = true
  try {
    await raid.setLockdown({ active: false })
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('raid.lockdown.failed')))
  } finally {
    busy.value = false
  }
}

// A lockdown that ends by itself: look again just after its time, so the banner does not wait for the minute.
watch(() => raid.lockdown?.until, at => {
  clearTimeout(expiry)
  const left = at ? Date.parse(at) - Date.now() : NaN
  if (left > 0) expiry = setTimeout(() => { void raid.load() }, Math.min(left + 500, MAX_DELAY_MS))
}, { immediate: true })

watch(allowed, start)
onMounted(start)
onBeforeUnmount(() => { stop?.(); clearTimeout(expiry) })
</script>

<template>
  <div
    v-if="allowed && raid.lockdownActive"
    role="status"
    data-testid="lockdown-banner"
    class="flex min-h-10 shrink-0 items-center gap-2.5 border-b border-mnema-warning/35 bg-mnema-warning/10 py-1.5 pl-4 pr-3 text-sm"
  >
    <ShieldAlert class="h-4 w-4 shrink-0 text-mnema-warning" />
    <span class="min-w-0 flex-1 text-mnema-text">{{ until ? $t('raid.lockdown.bannerUntil', { time: until }) : $t('raid.lockdown.banner') }}</span>
    <button type="button" class="btn btn-secondary btn-sm shrink-0" data-testid="lockdown-banner-end" :disabled="busy" @click="end">{{ $t('raid.lockdown.end') }}</button>
  </div>
</template>
