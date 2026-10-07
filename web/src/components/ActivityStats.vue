<script setup lang="ts">
import type { PropType } from 'vue'
import type { User } from '../types/domain'
// A member's totals: time in Talks (finished stays plus the running one)
// and messages sent.
import { computed } from 'vue'
import { Headphones, MessageSquare } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { now, secondsSince, totalParts } from '../lib/clock'
import { t, locale } from '../i18n'

const props = defineProps({
  user: { type: Object as PropType<Partial<Pick<User, 'id' | 'voice_seconds' | 'message_count'>>>, required: true },
  // 'compact': two tiny lines for the member list; 'full': labelled, for the profile.
  variant: { type: String as PropType<'compact' | 'full'>, default: 'compact' }
})

const voiceStore = useVoiceStore()

const voiceSeconds = computed(() => {
  const running = voiceStore.joinedAtOf(props.user.id ?? '')
  return (props.user.voice_seconds || 0) + (running ? secondsSince(running, now.value) : 0)
})
const voiceText = computed(() => {
  const { key, params } = totalParts(voiceSeconds.value)
  return t(key, params)
})
const messagesText = computed(() => new Intl.NumberFormat(locale.value).format(props.user.message_count || 0))
</script>

<template>
  <div
    v-if="variant === 'compact'"
    class="flex flex-shrink-0 flex-col items-end gap-0.5 text-[11px] leading-[14px] text-mnema-tertiary"
    data-testid="activity-stats"
    :aria-label="$t('activity.summary', { voice: voiceText, messages: messagesText })"
    v-tooltip="$t('activity.summary', { voice: voiceText, messages: messagesText })"
  >
    <span class="flex items-center gap-1 tabular-nums"><Headphones class="h-3 w-3" />{{ voiceText }}</span>
    <span class="flex items-center gap-1 tabular-nums"><MessageSquare class="h-3 w-3" />{{ messagesText }}</span>
  </div>
  <div v-else class="grid grid-cols-2 gap-2" data-testid="activity-stats">
    <div class="rounded-lg border border-mnema-hairline bg-mnema-raised/60 p-2">
      <div class="flex items-center gap-1.5 text-xs text-mnema-tertiary"><Headphones class="h-3.5 w-3.5" />{{ $t('activity.voiceTime') }}</div>
      <div class="mt-0.5 text-sm font-semibold tabular-nums text-mnema-text">{{ voiceText }}</div>
    </div>
    <div class="rounded-lg border border-mnema-hairline bg-mnema-raised/60 p-2">
      <div class="flex items-center gap-1.5 text-xs text-mnema-tertiary"><MessageSquare class="h-3.5 w-3.5" />{{ $t('activity.messages') }}</div>
      <div class="mt-0.5 text-sm font-semibold tabular-nums text-mnema-text">{{ messagesText }}</div>
    </div>
  </div>
</template>
