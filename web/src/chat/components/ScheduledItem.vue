<script setup lang="ts">
import { messageText } from '../../shared/lib/textLength'
import type { PropType } from 'vue'
import type { LaterScheduled } from '../../types/rest'
import { computed, ref } from 'vue'
import { AlertTriangle, Hash, MessageCircle, Pencil, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../../shared/lib/api'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import { earliestInputValue, fromLocalInputValue, isAcceptableTime, toLocalInputValue } from '../lib/laterTimes'
import { dateTime } from '../../shared/lib/time'
import { useLaterStore } from '../../stores/later'
import { useToastStore } from '../../stores/toast'
import { locale, t } from '../../i18n'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'

// One scheduled message in the "Reminders and scheduled" popover: its text,
// where and when it goes, and the actions to edit, reschedule or delete it. A
// message the server refused stays here as a failed draft with the reason.
const props = defineProps({
  item: { type: Object as PropType<LaterScheduled>, required: true }
})

const later = useLaterStore()
const toasts = useToastStore()
const editing = ref(false)
const removing = ref(false)

const failed = computed(() => props.item.state === 'failed')
const reasonKey = computed(() => (['slow_mode', 'not_allowed', 'unavailable'].includes(props.item.failure_reason) ? props.item.failure_reason : 'refused'))
const whenText = computed(() => dateTime(props.item.send_at, locale.value))

// The edit is a draft with its own bar. An untouched time is kept as it is and never
// read back from local wall time: in the hour that repeats when daylight saving ends,
// that would pick the other of the two equal clock times. A failed draft has no time
// that still lies ahead; the member picks one to send it again.
const form = useSettingsDraft<{ text: string; when: string }>({
  source: () => ({ text: props.item.content, when: failed.value ? '' : toLocalInputValue(new Date(props.item.send_at)) }),
  normalisers: { text: normalise.text, when: normalise.text },
  validate: d => (messageText(d.text) ? {} : { text: t('later.editText') }),
  save: async ({ changes }) => {
    const at = changes.when ? fromLocalInputValue(changes.when) : null
    if (changes.when && (!at || !isAcceptableTime(at))) throw new Error(t('later.pastTime'))
    await later.updateScheduled(props.item.id, {
      ...(changes.text !== undefined && messageText(changes.text) !== props.item.content ? { content: messageText(changes.text) } : {}),
      ...(at && (failed.value || at.getTime() !== new Date(props.item.send_at).getTime()) ? { at } : {})
    })
  },
  saved: () => {
    toasts.success(t('draft.saved'))
    editing.value = false
  }
})
const { draft } = form

function startEdit() {
  form.reset()
  editing.value = true
}

/** Cancel and Escape ask first while the edit holds unsaved changes. */
async function cancel() {
  if (!(await form.guard())) return
  form.reset()
  editing.value = false
}

async function remove() {
  if (removing.value) return
  removing.value = true
  try {
    await later.removeScheduled(props.item.id)
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('later.removeFailed')))
  } finally {
    removing.value = false
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
        :disabled="removing"
        @click="remove"
      >
        <Trash2 class="h-3.5 w-3.5" />
      </button>
    </div>
    <form v-if="editing" class="mt-2 space-y-2" @submit.prevent="form.save" @keydown="form.onKeydown" @keydown.esc.stop="cancel">
      <textarea
        v-model="draft.text"
        data-testid="scheduled-text"
        rows="3"
        v-max-chars="4000"
        :aria-label="$t('later.editText')"
        class="w-full resize-none rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
      ></textarea>
      <label class="block text-xs text-mnema-tertiary">
        {{ failed ? $t('later.rescheduleLabel') : $t('later.customLabel') }}
        <input
          v-model="draft.when"
          type="datetime-local"
          data-testid="scheduled-when"
          :min="earliestInputValue()"
          class="mt-1 block rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        >
      </label>
      <UnsavedChangesBar
        inline
        :dirty="form.dirty.value"
        :can-save="form.canSave.value"
        :saving="form.saving.value"
        :error="form.error.value"
        save-testid="scheduled-save"
        @save="form.save"
        @reset="form.reset"
      >
        <template #extra>
          <button type="button" class="btn btn-secondary h-8" @click="cancel">{{ $t('common.cancel') }}</button>
        </template>
      </UnsavedChangesBar>
    </form>
  </div>
</template>
