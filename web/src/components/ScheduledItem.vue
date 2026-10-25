<script setup lang="ts">
import type { PropType } from 'vue'
import type { LaterScheduled } from '../types/rest'
import { computed, ref } from 'vue'
import { AlertTriangle, Hash, MessageCircle, Pencil, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { earliestInputValue, fromLocalInputValue, isAcceptableTime, toLocalInputValue } from '../lib/laterTimes'
import { dateTime } from '../lib/time'
import { useLaterStore } from '../stores/later'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'

// One scheduled message in the "Reminders and scheduled" popover: its text,
// where and when it goes, and the actions to edit, reschedule or delete it. A
// message the server refused stays here as a failed draft with the reason.
const props = defineProps({
  item: { type: Object as PropType<LaterScheduled>, required: true }
})

const later = useLaterStore()
const toasts = useToastStore()
const editing = ref(false)
const text = ref('')
const when = ref('')
// The input value as editing started. An untouched time is kept as it is and
// never read back from local wall time: in the hour that repeats when daylight
// saving ends, that would pick the other of the two equal clock times.
const initialWhen = ref('')
const busy = ref(false)
const error = ref('')

const failed = computed(() => props.item.state === 'failed')
const reasonKey = computed(() => (['slow_mode', 'not_allowed', 'unavailable'].includes(props.item.failure_reason) ? props.item.failure_reason : 'refused'))
const whenText = computed(() => dateTime(props.item.send_at, locale.value))

function startEdit() {
  text.value = props.item.content
  // A failed draft has no time that still lies ahead; the member picks one to send it again.
  when.value = failed.value ? '' : toLocalInputValue(new Date(props.item.send_at))
  initialWhen.value = when.value
  error.value = ''
  editing.value = true
}

async function save() {
  const content = text.value.trim()
  if (!content || busy.value) return
  const touched = failed.value || when.value !== initialWhen.value
  const at = when.value && touched ? fromLocalInputValue(when.value) : null
  if (when.value && touched && (!at || !isAcceptableTime(at))) {
    error.value = t('later.pastTime')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const timeChanged = at && (failed.value || at.getTime() !== new Date(props.item.send_at).getTime())
    await later.updateScheduled(props.item.id, {
      ...(content !== props.item.content ? { content } : {}),
      ...(timeChanged ? { at } : {})
    })
    editing.value = false
  } catch (e) {
    error.value = caughtErrorMessage(e, t('later.scheduleFailed'))
  } finally {
    busy.value = false
  }
}

async function remove() {
  try {
    await later.removeScheduled(props.item.id)
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('later.removeFailed')))
  }
}
</script>

<template>
  <div data-testid="scheduled-item" class="group rounded-lg p-2 transition hover:bg-mnema-hover">
    <div class="flex items-start gap-1">
      <div class="min-w-0 flex-1">
        <p class="mb-0.5 flex items-center gap-1.5 text-xs text-mnema-tertiary">
          <MessageCircle v-if="item.dm" class="h-3 w-3" />
          <Hash v-else class="h-3 w-3" />
          <span class="truncate">{{ item.dm ? $t('later.directMessage') : item.channel_name || $t('later.channelGone') }}</span>
          <span class="ml-auto shrink-0">{{ failed ? $t('later.draft') : whenText }}</span>
        </p>
        <p class="line-clamp-3 wrap-break-word text-sm text-mnema-text">{{ item.content }}</p>
        <p v-if="failed" data-testid="scheduled-failed" class="mt-1 flex items-start gap-1 text-xs text-mnema-danger">
          <AlertTriangle class="mt-0.5 h-3 w-3 shrink-0" />
          <span>{{ $t('later.failedAt', { time: whenText }) }} {{ $t(`later.reason.${reasonKey}`) }}</span>
        </p>
      </div>
      <button
        type="button"
        data-testid="scheduled-edit"
        class="btn btn-icon h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
        :aria-label="$t('later.edit')"
        v-tooltip="$t('later.edit')"
        @click="startEdit"
      >
        <Pencil class="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        data-testid="scheduled-delete"
        class="btn btn-icon h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
        :aria-label="$t('later.delete')"
        v-tooltip="$t('later.delete')"
        @click="remove"
      >
        <Trash2 class="h-3.5 w-3.5" />
      </button>
    </div>
    <form v-if="editing" class="mt-2 space-y-2" @submit.prevent="save">
      <textarea
        v-model="text"
        data-testid="scheduled-text"
        rows="3"
        maxlength="4000"
        :aria-label="$t('later.editText')"
        class="w-full resize-none rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
      ></textarea>
      <label class="block text-xs text-mnema-tertiary">
        {{ failed ? $t('later.rescheduleLabel') : $t('later.customLabel') }}
        <input
          v-model="when"
          type="datetime-local"
          data-testid="scheduled-when"
          :min="earliestInputValue()"
          class="mt-1 block rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        >
      </label>
      <p v-if="error" role="alert" class="text-xs text-mnema-danger">{{ error }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary h-8" @click="editing = false">{{ $t('common.cancel') }}</button>
        <button type="submit" data-testid="scheduled-save" :disabled="!text.trim() || busy" class="btn btn-primary h-8">{{ $t('common.save') }}</button>
      </div>
    </form>
  </div>
</template>
