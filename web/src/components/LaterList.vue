<script setup lang="ts">
import type { LaterReminder } from '../types/rest'
import { computed } from 'vue'
import { AlarmClock, BellRing, Check, Hash, MessageCircle, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useLaterStore } from '../stores/later'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { presetTime } from '../lib/laterTimes'
import { previewText } from '../lib/replies'
import { dateTime } from '../lib/time'
import { locale, t } from '../i18n'
import ScheduledItem from './ScheduledItem.vue'
import StatePanel from './StatePanel.vue'

// The member's reminders and scheduled messages. A reminder that fell due
// waits here, marked as due, until it is marked done or snoozed. The server
// rechecks access on every read, so a reminder of a message the member can no
// longer see shows no text.
const emit = defineEmits<{ navigate: []; retry: [] }>()
const chatStore = useChatStore()
const later = useLaterStore()
const features = useServerFeaturesStore()
const toasts = useToastStore()

const hasReminders = computed(() => features.has('reminders'))
const hasScheduled = computed(() => features.has('messages.scheduled'))
const empty = computed(() => !later.reminders.length && !later.scheduled.length)

const when = (iso: string) => dateTime(iso, locale.value)

function openMessage(entry: LaterReminder) {
  if (!entry.message) return
  emit('navigate')
  const { id, channel_id, parent_id } = entry.message
  chatStore.goToMessage({ id, channel_id, ...(parent_id ? { parent_id } : {}) })
}

async function dismiss(entry: LaterReminder) {
  try {
    await later.cancelReminder(entry.id)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('later.removeFailed')))
  }
}

const SNOOZES = [
  { preset: 'in1h', label: 'later.preset.in1h' },
  { preset: 'tomorrow', label: 'later.preset.tomorrow' }
] as const

async function snooze(entry: LaterReminder, preset: typeof SNOOZES[number]['preset']) {
  if (!entry.message) return
  const at = presetTime(preset)
  try {
    const { cleanupFailed } = await later.snooze(entry.id, entry.message.id, at)
    if (cleanupFailed) toasts.info(t('later.snoozeNotCleared', { time: when(at.toISOString()) }))
    else toasts.success(t('later.snoozed', { time: when(at.toISOString()) }))
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('later.snoozeFailed')))
  }
}
</script>

<template>
  <div data-testid="later-list">
    <StatePanel v-if="later.loading && !later.loaded" state="loading" />
    <StatePanel v-else-if="later.failed" state="error" retryable @retry="emit('retry')">{{ $t('later.failed') }}</StatePanel>
    <div v-else-if="empty" data-testid="later-empty" class="flex flex-col items-center gap-2 px-5 py-8 text-center">
      <AlarmClock class="h-5 w-5 text-mnema-tertiary" aria-hidden="true" />
      <p class="text-sm font-medium text-mnema-text">{{ $t('later.emptyTitle') }}</p>
      <p class="max-w-[32ch] text-balance text-sm text-mnema-tertiary">{{ $t('later.emptyBody') }}</p>
      <p class="max-w-[32ch] text-balance text-xs text-mnema-tertiary">{{ $t('later.empty') }}</p>
    </div>

    <section v-if="hasReminders && later.reminders.length" :aria-label="$t('later.reminders')">
      <h3 class="px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('later.reminders') }}</h3>
      <div v-for="entry in later.reminders" :key="entry.id" data-testid="reminder-item" class="rounded-lg p-1 transition hover:bg-mnema-hover">
        <div class="flex items-start gap-1">
          <button
            v-if="entry.available && entry.message"
            type="button"
            data-testid="reminder-open"
            class="focus-ring min-w-0 flex-1 rounded-lg p-1.5 text-left"
            @click="openMessage(entry)"
          >
            <span class="mb-0.5 flex items-center gap-1.5 text-xs text-mnema-tertiary">
              <MessageCircle v-if="entry.message.dm" class="h-3 w-3" aria-hidden="true" />
              <Hash v-else class="h-3 w-3" aria-hidden="true" />
              <span class="truncate">{{ entry.message.dm ? entry.message.author : entry.message.channel_name }}</span>
              <span class="ml-auto flex shrink-0 items-center gap-1" :class="entry.delivered_at ? 'text-mnema-accent' : ''">
                <BellRing v-if="entry.delivered_at" class="h-3 w-3" aria-hidden="true" />
                {{ entry.delivered_at ? $t('later.due') : when(entry.remind_at) }}
              </span>
            </span>
            <span class="line-clamp-3 wrap-break-word text-sm text-mnema-text">{{ previewText(entry.message.snippet) || $t('chat.attachment') }}</span>
          </button>
          <p v-else data-testid="reminder-unavailable" class="min-w-0 flex-1 p-2 text-sm italic text-mnema-tertiary">
            {{ $t('later.unavailable') }}
          </p>
          <button
            type="button"
            data-testid="reminder-remove"
            class="btn btn-icon mt-1.5 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
            :aria-label="entry.delivered_at ? $t('later.done') : $t('later.cancel')"
            v-tooltip="entry.delivered_at ? $t('later.done') : $t('later.cancel')"
            @click="dismiss(entry)"
          >
            <Check v-if="entry.delivered_at" class="h-4 w-4" />
            <X v-else class="h-4 w-4" />
          </button>
        </div>
        <div v-if="entry.delivered_at && entry.available && entry.message" data-testid="reminder-snooze" class="flex flex-wrap items-center gap-1 px-2 pb-1.5">
          <span class="text-xs text-mnema-tertiary">{{ $t('later.snooze') }}</span>
          <button
            v-for="option in SNOOZES"
            :key="option.preset"
            type="button"
            :data-testid="`reminder-snooze-${option.preset}`"
            class="btn btn-ghost btn-sm max-md:min-h-11"
            @click="snooze(entry, option.preset)"
          >{{ $t(option.label) }}</button>
        </div>
      </div>
    </section>

    <section v-if="hasScheduled && later.scheduled.length" :aria-label="$t('later.scheduled')">
      <h3 class="px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('later.scheduled') }}</h3>
      <ScheduledItem v-for="item in later.scheduled" :key="item.id" :item="item" />
    </section>
  </div>
</template>
