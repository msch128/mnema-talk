<script setup lang="ts">
// A channel's auto-delete, in the channel settings: the community default,
// off, or a period of hours, days or weeks of the messages or only their
// files. A field of the channel's draft: the dialog stores it with Save, after
// a dry run counted what it would remove at once (EditNameDialog). An older
// server knows whole days only and gets the four choices it takes.
import { computed, ref, watch } from 'vue'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import {
  AUTO_DELETE_DAYS, MAX_AUTO_DELETE_HOURS, UNIT_HOURS, decodeSetting, effectLine, encodeSetting,
  type AutoDeleteScope, type AutoDeleteUnit
} from '../../shared/lib/autoDelete'
import { locale, t } from '../../i18n'
import { timeShort } from '../../shared/lib/time'
import { serverOffset } from '../../shared/lib/clock'

defineProps({ channelId: { type: String, default: '' } })
const value = defineModel<string>({ required: true })
const features = useServerFeaturesStore()
const v2 = computed(() => features.has('channels.auto_delete.v2'))
const setting = computed(() => decodeSetting(value.value))
const mode = computed({
  get: () => setting.value.mode,
  set: (m: string) => {
    if (m === 'period') value.value = encodeSetting({ mode: 'period', hours: 7 * 24, scope: 'all' })
    else value.value = m === 'default' ? 'default' : 'off'
  }
})
const hours = computed(() => (setting.value.mode === 'period' ? setting.value.hours : 0))
const scope = computed(() => (setting.value.mode === 'period' ? setting.value.scope : 'all'))
// The unit is the member's choice once made; it is only worked out from the
// stored period when the field opens or the period comes from elsewhere.
const unitOf = (h: number): AutoDeleteUnit => (h > 0 && h % 168 === 0 ? 'weeks' : h > 0 && h % 24 === 0 ? 'days' : 'hours')
const chosenUnit = ref<AutoDeleteUnit>(unitOf(hours.value))
watch(hours, h => { if (h % UNIT_HOURS[chosenUnit.value] !== 0) chosenUnit.value = unitOf(h) })
const unit = computed<AutoDeleteUnit>(() => chosenUnit.value)
const amount = computed(() => hours.value / UNIT_HOURS[unit.value])

function setPeriod(nextAmount: number, nextUnit: AutoDeleteUnit, nextScope: AutoDeleteScope) {
  chosenUnit.value = nextUnit
  const h = Math.round(nextAmount) * UNIT_HOURS[nextUnit]
  value.value = encodeSetting({ mode: 'period', hours: Math.min(Math.max(h, 1), MAX_AUTO_DELETE_HOURS), scope: nextScope })
}
const summary = computed(() => {
  if (setting.value.mode === 'default') return t('autoDelete.followsDefault')
  if (setting.value.mode === 'off') return t('autoDelete.offSummary')
  return effectLine({ hours: hours.value, scope: scope.value })
})
// Days the older server takes, as a select of its own.
const legacyDays = computed({
  get: () => (setting.value.mode === 'period' ? Math.round(hours.value / 24) : 0),
  set: (d: number) => { value.value = d > 0 ? encodeSetting({ mode: 'period', hours: d * 24, scope: 'all' }) : 'off' }
})
// The sweeper runs on whole multiples of ten minutes of the server's clock.
const nextRun = computed(() => {
  const at = Math.ceil((Date.now() + serverOffset.value) / 600_000) * 600_000
  return timeShort(new Date(at - serverOffset.value), locale.value)
})
const field = 'bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent'
</script>

<template>
  <div v-if="features.channelAutoDelete" class="stack-1.5" data-testid="auto-delete-field">
    <label for="edit-auto-delete" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('autoDelete.label') }}
    </label>
    <template v-if="v2">
      <select id="edit-auto-delete" v-model="mode" data-testid="auto-delete-mode" :class="[field, 'w-full']">
        <option value="default">{{ $t('autoDelete.useDefault') }}</option>
        <option value="off">{{ $t('autoDelete.off') }}</option>
        <option value="period">{{ $t('autoDelete.custom') }}</option>
      </select>
      <div v-if="setting.mode === 'period'" class="flex flex-wrap items-center gap-2" data-testid="auto-delete-period">
        <label class="sr-only" for="auto-delete-amount">{{ $t('autoDelete.amount') }}</label>
        <input
          id="auto-delete-amount"
          type="number"
          min="1"
          step="1"
          inputmode="numeric"
          :value="amount"
          :class="[field, 'w-24 tabular-nums']"
          @change="setPeriod(Number(($event.target as HTMLInputElement).value), unit, scope)"
        />
        <label class="sr-only" for="auto-delete-unit">{{ $t('autoDelete.unit') }}</label>
        <select id="auto-delete-unit" :value="unit" :class="field" @change="setPeriod(amount, ($event.target as HTMLSelectElement).value as AutoDeleteUnit, scope)">
          <option value="hours">{{ $t('autoDelete.unitNames.hours') }}</option>
          <option value="days">{{ $t('autoDelete.unitNames.days') }}</option>
          <option value="weeks">{{ $t('autoDelete.unitNames.weeks') }}</option>
        </select>
        <label class="sr-only" for="auto-delete-scope">{{ $t('autoDelete.scope') }}</label>
        <select id="auto-delete-scope" :value="scope" data-testid="auto-delete-scope" :class="field" @change="setPeriod(amount, unit, ($event.target as HTMLSelectElement).value as AutoDeleteScope)">
          <option value="all">{{ $t('autoDelete.scopeAll') }}</option>
          <option value="attachments">{{ $t('autoDelete.scopeFiles') }}</option>
        </select>
      </div>
      <p class="text-xs text-mnema-text" data-testid="auto-delete-summary">{{ summary }}</p>
      <p v-if="setting.mode === 'period'" class="text-xs text-mnema-tertiary">{{ $t('autoDelete.nextRun', { time: nextRun }) }}</p>
    </template>
    <select v-else id="edit-auto-delete" v-model.number="legacyDays" data-testid="auto-delete-select" :class="[field, 'w-full']">
      <option :value="0">{{ $t('autoDelete.off') }}</option>
      <option v-for="d in AUTO_DELETE_DAYS" :key="d" :value="d">{{ $t(`autoDelete.choices.${d}`) }}</option>
    </select>
    <p v-if="!v2" class="text-xs text-mnema-tertiary">{{ $t('autoDelete.hint') }}</p>
  </div>
</template>
