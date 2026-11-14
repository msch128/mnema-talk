<script setup lang="ts">
// The community's auto-delete default: every channel without a setting of its
// own follows it (conversations never). Saving a default that would remove
// content at once first shows the dry run's counts and asks.
import type { PropType } from 'vue'
import { computed } from 'vue'
import { api } from '../../shared/lib/api'
import { decodeChatMessageSettingsView, type ChatMessageSettingsView } from '../../types/rest'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import { saveKeys } from '../../shared/composables/draftGuard'
import { MAX_AUTO_DELETE_HOURS, UNIT_HOURS, confirmAutoDelete, periodLabel, type AutoDeleteScope, type AutoDeleteUnit } from '../../shared/lib/autoDelete'
import { previewDefaultAutoDelete } from '../lib/autoDeletePreview'
import { timeShort } from '../../shared/lib/time'
import { useToastStore } from '../../stores/toast'
import { locale, t } from '../../i18n'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'

const props = defineProps({ settings: { type: Object as PropType<ChatMessageSettingsView>, required: true } })
const emit = defineEmits<{ saved: [settings: ChatMessageSettingsView] }>()
const toasts = useToastStore()

interface Draft { on: boolean; amount: number; unit: AutoDeleteUnit; scope: AutoDeleteScope }
function source(): Draft {
  const h = props.settings.auto_delete_default_hours
  const unit: AutoDeleteUnit = h && h % 168 === 0 ? 'weeks' : h && h % 24 === 0 ? 'days' : h ? 'hours' : 'days'
  return { on: h > 0, amount: h ? h / UNIT_HOURS[unit] : 30, unit, scope: props.settings.auto_delete_default_scope === 'attachments' ? 'attachments' : 'all' }
}
const hoursOf = (d: Draft) => (d.on ? Number(d.amount) * UNIT_HOURS[d.unit] : 0)
const form = useSettingsDraft<Draft>({
  source,
  normalisers: { amount: normalise.count },
  validate: d => {
    const h = hoursOf(d)
    return !d.on || (Number.isInteger(h) && h >= 1 && h <= MAX_AUTO_DELETE_HOURS) ? {} : { amount: t('admin.messages.periodInvalid') }
  },
  save: async ({ draft }) => {
    const json = { hours: hoursOf(draft), scope: draft.scope }
    emit('saved', await api('/api/admin/message-settings/auto-delete', { method: 'PUT', json, decode: decodeChatMessageSettingsView }))
  },
  saved: () => { toasts.success(t('admin.messages.saved')) }
})
const { draft, errors } = form
const summary = computed(() => {
  if (!draft.on) return t('admin.messages.defaultOff')
  return t(draft.scope === 'attachments' ? 'admin.messages.defaultFiles' : 'admin.messages.defaultAll', { period: periodLabel(hoursOf(draft)) })
})
const nextRun = computed(() => timeShort(props.settings.auto_delete_next_run_at, locale.value))

async function save() {
  if (!form.canSave.value) return
  if (draft.on) {
    const counts = await previewDefaultAutoDelete({ hours: hoursOf(draft), scope: draft.scope })
    if (!(await confirmAutoDelete('', counts, true))) return
  }
  await form.save()
}
// Enter and the save shortcut ask with the counts too, as the Save button does.
const onKeydown = saveKeys(save)
const field = 'rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm text-mnema-text outline-hidden focus:border-mnema-accent'
</script>

<template>
  <div class="rounded-lg border border-mnema-hairline bg-mnema-surface" data-testid="auto-delete-default">
    <form @submit.prevent="save" @keydown="onKeydown">
      <div class="space-y-3 p-4">
        <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.messages.defaultTitle') }}</div>
        <p class="text-xs text-mnema-tertiary">{{ $t('admin.messages.defaultHint') }}</p>
        <label class="flex items-center gap-2 text-sm text-mnema-text">
          <input v-model="draft.on" type="checkbox" data-testid="auto-delete-default-on" />
          <span>{{ $t('admin.messages.defaultOn') }}</span>
        </label>
        <div v-if="draft.on" class="flex flex-wrap items-center gap-2">
          <label class="sr-only" for="default-amount">{{ $t('autoDelete.amount') }}</label>
          <input id="default-amount" v-model.number="draft.amount" type="number" min="1" step="1" inputmode="numeric" :aria-invalid="errors.amount ? 'true' : undefined" :class="[field, 'w-24 tabular-nums']" data-testid="auto-delete-default-amount" />
          <label class="sr-only" for="default-unit">{{ $t('autoDelete.unit') }}</label>
          <select id="default-unit" v-model="draft.unit" :class="field">
            <option value="hours">{{ $t('autoDelete.unitNames.hours') }}</option>
            <option value="days">{{ $t('autoDelete.unitNames.days') }}</option>
            <option value="weeks">{{ $t('autoDelete.unitNames.weeks') }}</option>
          </select>
          <label class="sr-only" for="default-scope">{{ $t('autoDelete.scope') }}</label>
          <select id="default-scope" v-model="draft.scope" :class="field">
            <option value="all">{{ $t('autoDelete.scopeAll') }}</option>
            <option value="attachments">{{ $t('autoDelete.scopeFiles') }}</option>
          </select>
        </div>
        <p v-if="errors.amount" class="text-xs text-mnema-danger" role="alert">{{ errors.amount }}</p>
        <p class="text-xs text-mnema-text" data-testid="auto-delete-default-summary">{{ summary }}</p>
        <p v-if="draft.on" class="text-xs text-mnema-tertiary">{{ $t('autoDelete.nextRun', { time: nextRun }) }}</p>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="save" @reset="form.reset" />
    </form>
  </div>
</template>
