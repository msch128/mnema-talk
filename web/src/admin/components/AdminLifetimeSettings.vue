<script setup lang="ts">
// Messages with a lifetime: whether members may send them, and the longest
// lifetime anybody may choose (the Owner included).
import type { PropType } from 'vue'
import { api } from '../../shared/lib/api'
import { decodeChatMessageSettingsView, type ChatMessageSettingsView } from '../../types/rest'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import { UNIT_SECONDS, lifetimeLabel, validLifetime, type LifetimeUnit } from '../../shared/lib/lifetime'
import { useToastStore } from '../../stores/toast'
import { useLifetimeStore } from '../../stores/lifetime'
import { t } from '../../i18n'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'

const props = defineProps({ settings: { type: Object as PropType<ChatMessageSettingsView>, required: true } })
const emit = defineEmits<{ saved: [settings: ChatMessageSettingsView] }>()
const toasts = useToastStore()

function split(seconds: number): { amount: number; unit: LifetimeUnit } {
  if (seconds % UNIT_SECONDS.days === 0) return { amount: seconds / UNIT_SECONDS.days, unit: 'days' }
  if (seconds % UNIT_SECONDS.hours === 0) return { amount: seconds / UNIT_SECONDS.hours, unit: 'hours' }
  return { amount: Math.round(seconds / 60), unit: 'minutes' }
}
const form = useSettingsDraft<{ enabled: boolean; amount: number; unit: LifetimeUnit }>({
  source: () => ({ enabled: props.settings.message_lifetimes_enabled, ...split(props.settings.message_lifetime_max_seconds) }),
  normalisers: { amount: normalise.count },
  validate: d => (validLifetime(Number(d.amount) * UNIT_SECONDS[d.unit]) ? {} : { amount: t('admin.messages.maxInvalid') }),
  save: async ({ draft }) => {
    const json = { message_lifetimes_enabled: draft.enabled, message_lifetime_max_seconds: Number(draft.amount) * UNIT_SECONDS[draft.unit] }
    emit('saved', await api('/api/admin/message-settings/lifetimes', { method: 'PUT', json, decode: decodeChatMessageSettingsView }))
  },
  saved: () => {
    toasts.success(t('admin.messages.saved'))
    void useLifetimeStore().load()
  }
})
const { draft, errors } = form
const field = 'rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm text-mnema-text outline-hidden focus:border-mnema-accent'
</script>

<template>
  <div class="rounded-lg border border-mnema-hairline bg-mnema-surface" data-testid="lifetime-settings">
    <form @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="space-y-3 p-4">
        <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.messages.lifetimesTitle') }}</div>
        <label class="flex items-center gap-2 text-sm text-mnema-text">
          <input v-model="draft.enabled" type="checkbox" data-testid="lifetimes-enabled" />
          <span>{{ $t('admin.messages.lifetimesEnabled') }}</span>
        </label>
        <div class="flex flex-wrap items-center gap-2">
          <label for="lifetime-max" class="text-sm text-mnema-text">{{ $t('admin.messages.longest') }}</label>
          <input id="lifetime-max" v-model.number="draft.amount" type="number" min="1" step="1" inputmode="numeric" :aria-invalid="errors.amount ? 'true' : undefined" :class="[field, 'w-24 tabular-nums']" data-testid="lifetime-max" />
          <label class="sr-only" for="lifetime-max-unit">{{ $t('lifetime.unit') }}</label>
          <select id="lifetime-max-unit" v-model="draft.unit" :class="field">
            <option value="minutes">{{ $t('lifetime.units.minutes') }}</option>
            <option value="hours">{{ $t('lifetime.units.hours') }}</option>
            <option value="days">{{ $t('lifetime.units.days') }}</option>
          </select>
        </div>
        <p v-if="errors.amount" class="text-xs text-mnema-danger" role="alert">{{ errors.amount }}</p>
        <p class="text-xs text-mnema-tertiary">{{ $t('admin.messages.lifetimesHint', { max: lifetimeLabel(settings.message_lifetime_max_seconds) }) }}</p>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </div>
</template>
