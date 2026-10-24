<script setup lang="ts">
import type { LaterReminder } from '../types/rest'
import { computed, onMounted, ref, watch } from 'vue'
import { AlarmClock, Check, Hash, Loader2, MessageCircle, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useLaterStore } from '../stores/later'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { useDismissable } from '../composables/useDismissable'
import { caughtErrorMessage } from '../lib/api'
import { previewText } from '../lib/replies'
import { clockTimeOptions } from '../lib/time'
import { locale, t } from '../i18n'
import ScheduledItem from './ScheduledItem.vue'

// The member's reminders and scheduled messages in the channel header: one
// button with a popover. Delivered reminders wait here until dismissed, and a
// scheduled message the server refused waits as a draft. The server rechecks
// access on every read, so a reminder of a message the member can no longer
// see shows no text.
const chatStore = useChatStore()
const later = useLaterStore()
const features = useServerFeaturesStore()
const toasts = useToastStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)

const hasReminders = computed(() => features.has('reminders'))
const hasScheduled = computed(() => features.has('messages.scheduled'))
const enabled = computed(() => hasReminders.value || hasScheduled.value)
const empty = computed(() => !later.reminders.length && !later.scheduled.length)

useDismissable(root, () => { open.value = false }, { active: open })

function refresh() {
  void later.load({ reminders: hasReminders.value, scheduled: hasScheduled.value })
}

// The badge needs the lists before the popover is first opened.
onMounted(() => {
  void features.load().then(() => { if (enabled.value) refresh() })
})
watch(open, isOpen => { if (isOpen && enabled.value) refresh() })

function when(iso: string) {
  return new Date(iso).toLocaleString(locale.value, { weekday: 'short', day: 'numeric', month: 'short', ...clockTimeOptions(locale.value) })
}

function openMessage(entry: LaterReminder) {
  if (!entry.message) return
  open.value = false
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
</script>

<template>
  <div v-if="enabled" ref="root" class="relative">
    <button
      type="button"
      data-testid="later-button"
      class="btn btn-icon relative max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('later.title')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('later.title')"
      @click="open = !open"
    >
      <AlarmClock class="h-5 w-5" />
      <span
        v-if="later.attention"
        data-testid="later-badge"
        class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-mnema-accent px-1 text-center font-mono text-[10px] font-semibold leading-4 text-mnema-accent-ink"
      >{{ later.attention }}</span>
    </button>

    <div
      v-if="open"
      data-testid="later-panel"
      role="dialog"
      :aria-label="$t('later.title')"
      class="absolute right-0 top-full z-30 mt-2 flex max-h-[70vh] w-[min(24rem,calc(100vw-1.5rem))] flex-col max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:max-h-[calc(100dvh-4.5rem-env(safe-area-inset-top))] max-md:w-auto overflow-hidden rounded-xl bg-mnema-elevated shadow-popover pop-in"
    >
      <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
        <div v-if="later.loading && !later.loaded" class="flex justify-center py-8 text-mnema-accent">
          <Loader2 class="h-4 w-4 animate-spin" />
        </div>
        <p v-else-if="later.failed" role="alert" class="py-8 text-center text-sm text-mnema-tertiary">{{ $t('later.failed') }}</p>
        <p v-else-if="empty" data-testid="later-empty" class="px-4 py-8 text-center text-sm text-mnema-tertiary">{{ $t('later.empty') }}</p>

        <section v-if="hasReminders && later.reminders.length" :aria-label="$t('later.reminders')">
          <h3 class="px-2 pb-1 pt-2 font-mono text-xs font-semibold uppercase tracking-widest text-mnema-tertiary">{{ $t('later.reminders') }}</h3>
          <div v-for="entry in later.reminders" :key="entry.id" data-testid="reminder-item" class="rounded-lg p-1 transition hover:bg-mnema-hover">
            <div class="flex items-start gap-1">
              <button
                v-if="entry.available && entry.message"
                type="button"
                data-testid="reminder-open"
                class="min-w-0 flex-1 rounded-lg p-1.5 text-left"
                @click="openMessage(entry)"
              >
                <span class="mb-0.5 flex items-center gap-1.5 text-xs text-mnema-tertiary">
                  <MessageCircle v-if="entry.message.dm" class="h-3 w-3" />
                  <Hash v-else class="h-3 w-3" />
                  <span class="truncate">{{ entry.message.dm ? entry.message.author : entry.message.channel_name }}</span>
                  <span class="ml-auto shrink-0" :class="entry.delivered_at ? 'text-mnema-accent' : ''">
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
                :aria-label="entry.delivered_at ? $t('later.dismiss') : $t('later.cancel')"
                v-tooltip="entry.delivered_at ? $t('later.dismiss') : $t('later.cancel')"
                @click="dismiss(entry)"
              >
                <Check v-if="entry.delivered_at" class="h-4 w-4" />
                <X v-else class="h-4 w-4" />
              </button>
            </div>
          </div>
        </section>

        <section v-if="hasScheduled && later.scheduled.length" :aria-label="$t('later.scheduled')">
          <h3 class="px-2 pb-1 pt-2 font-mono text-xs font-semibold uppercase tracking-widest text-mnema-tertiary">{{ $t('later.scheduled') }}</h3>
          <ScheduledItem v-for="item in later.scheduled" :key="item.id" :item="item" />
        </section>
      </div>
    </div>
  </div>
</template>
