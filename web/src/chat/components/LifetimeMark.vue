<script setup lang="ts">
// The small mark on a message with a lifetime: a timer and the time left
// ("5 min"). It reads the shared server clock, so only the mark updates, not
// the list; the message leaves the list at its end (stores/chat/expiry.ts).
import { computed } from 'vue'
import { Timer } from '@lucide/vue'
import { now, useServerClock } from '../../shared/lib/clock'
import { remainingLabel } from '../../shared/lib/lifetime'
import { t } from '../../i18n'

const props = defineProps({
  expiresAt: { type: String, required: true }
})
useServerClock()
const left = computed(() => remainingLabel(Date.parse(props.expiresAt) - now.value))
const spoken = computed(() => t('lifetime.markLabel', { time: left.value }))
</script>

<template>
  <span class="inline-flex shrink-0 items-center gap-0.5 text-xs tabular-nums text-mnema-tertiary" data-testid="lifetime-mark" :aria-label="spoken" v-tooltip="spoken">
    <Timer class="h-3 w-3" aria-hidden="true" />
    <span aria-hidden="true">{{ left }}</span>
  </span>
</template>
