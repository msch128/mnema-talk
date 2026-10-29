<script setup lang="ts">
// Host for openLater() from lib/laterDialog: choose when to be reminded of a
// message ("Remind me") or when a composed message goes out ("Send later").
// The time is worked out here in the member's own time zone and sent as an
// absolute time.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { closeLater, pendingLater } from '../lib/laterDialog'
import {
  earliestInputValue, fromLocalInputValue, isAcceptableTime, presetTime, type LaterPreset
} from '../lib/laterTimes'
import { clockTimeOptions } from '../lib/time'
import { useLaterStore } from '../stores/later'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const request = computed(() => pendingLater.value)
const later = useLaterStore()
const toasts = useToastStore()

const REMIND_PRESETS: LaterPreset[] = ['in20m', 'in1h', 'in3h', 'tomorrow']
const SCHEDULE_PRESETS: LaterPreset[] = ['in30m', 'in1h', 'in3h', 'tomorrow']

const choice = ref<LaterPreset | 'custom'>('in1h')
const custom = ref('')
const now = ref(new Date())
const busy = ref(false)
const error = ref('')

const presets = computed(() => (request.value?.kind === 'schedule' ? SCHEDULE_PRESETS : REMIND_PRESETS))

watch(request, req => {
  now.value = new Date()
  choice.value = req?.kind === 'schedule' ? 'in1h' : 'in20m'
  custom.value = ''
  error.value = ''
}, { immediate: true })

// The preset labels follow the clock while the dialog stays open.
let clock: ReturnType<typeof setInterval> | undefined
onMounted(() => { clock = setInterval(() => { now.value = new Date() }, 30_000) })
onBeforeUnmount(() => clearInterval(clock))

function describe(date: Date): string {
  return date.toLocaleString(locale.value, { weekday: 'short', day: 'numeric', month: 'short', ...clockTimeOptions(locale.value) })
}

const chosen = computed<Date | null>(() => {
  if (choice.value === 'custom') return fromLocalInputValue(custom.value)
  return presetTime(choice.value, now.value)
})
// A custom time is checked against the real clock when the member confirms.
const valid = computed(() => !!chosen.value && isAcceptableTime(chosen.value, choice.value === 'custom' ? new Date() : now.value) )

async function confirm() {
  const req = request.value
  if (!req || busy.value) return
  // A preset counts from the moment of confirming, not from opening the dialog.
  now.value = new Date()
  const at = chosen.value
  if (!at) return
  if (!isAcceptableTime(at)) {
    error.value = t('later.pastTime')
    return
  }
  busy.value = true
  error.value = ''
  let onScheduled: (() => void) | undefined
  try {
    if (req.kind === 'remind') {
      await later.remind(req.messageId, at)
      toasts.success(t('later.reminderSet', { time: describe(at) }))
    } else {
      await later.schedule(req.channelId, req.content, at, { parentId: req.parentId ?? null, replyToId: req.replyToId ?? null })
      toasts.success(t('later.scheduledSet', { time: describe(at) }))
      onScheduled = req.onScheduled
    }
    closeLater()
  } catch (e) {
    error.value = caughtErrorMessage(e, t(req.kind === 'remind' ? 'later.reminderFailed' : 'later.scheduleFailed'))
  } finally {
    busy.value = false
  }
  // Outside the try: a throwing callback is not a failed schedule (a retry would send twice).
  if (onScheduled) {
    try {
      onScheduled()
    } catch (e) {
      console.error('onScheduled failed:', e)
    }
  }
}
</script>

<template>
  <BaseDialog v-if="request" panel-class="max-w-[420px]" @close="closeLater">
    <template #default="{ titleId }">
      <form class="space-y-3 p-5" data-testid="later-dialog" @submit.prevent="confirm">
        <h2 :id="titleId" class="text-lg font-semibold text-mnema-text">
          {{ request.kind === 'remind' ? $t('later.remindTitle') : $t('later.scheduleTitle') }}
        </h2>
        <p class="line-clamp-3 wrap-break-word rounded-md bg-mnema-raised px-3 py-2 text-sm text-mnema-muted" data-testid="later-excerpt">
          {{ request.kind === 'remind' ? request.excerpt : request.content }}
        </p>
        <p class="text-xs text-mnema-tertiary">
          {{ request.kind === 'remind' ? $t('later.remindHint') : $t('later.scheduleHint') }}
        </p>
        <ul class="space-y-0.5" role="radiogroup" :aria-label="$t('later.when')">
          <li v-for="p in presets" :key="p">
            <label class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm text-mnema-text hover:bg-mnema-hover">
              <input v-model="choice" type="radio" name="later-when" :value="p" :data-testid="`later-preset-${p}`">
              <span class="flex-1">{{ $t(`later.preset.${p}`) }}</span>
              <span class="text-xs text-mnema-tertiary">{{ describe(presetTime(p, now)) }}</span>
            </label>
          </li>
          <li>
            <label class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm text-mnema-text hover:bg-mnema-hover">
              <input v-model="choice" type="radio" name="later-when" value="custom" data-testid="later-preset-custom">
              <span class="flex-1">{{ $t('later.preset.custom') }}</span>
            </label>
            <input
              v-if="choice === 'custom'"
              v-model="custom"
              type="datetime-local"
              data-testid="later-custom-input"
              :min="earliestInputValue(now)"
              :aria-label="$t('later.customLabel')"
              class="mt-1 ml-8 rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
            >
          </li>
        </ul>
        <p v-if="error" role="alert" data-testid="later-error" class="rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</p>
        <div class="flex justify-end gap-2 border-t border-mnema-hairline pt-3">
          <button type="button" class="btn btn-secondary h-8" @click="closeLater">{{ $t('common.cancel') }}</button>
          <button type="submit" data-testid="later-confirm" :disabled="!valid || busy" class="btn btn-primary h-8">
            {{ request.kind === 'remind' ? $t('later.remindConfirm') : $t('later.scheduleConfirm') }}
          </button>
        </div>
      </form>
    </template>
  </BaseDialog>
</template>
