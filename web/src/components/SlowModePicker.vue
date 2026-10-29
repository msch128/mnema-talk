<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { customSeconds, durationLabel } from '../lib/moderation'
import type { CustomUnit } from '../lib/moderation'
import { t } from '../i18n'
import { SLOW_MODE_PRESETS } from '../lib/presets'

// Slow mode of a channel: ready-made presets plus a free value in seconds,
// minutes or hours, at most 24 hours (the server validates the same bound).
const model = defineModel<number>({ required: true })
const valid = defineModel<boolean>('valid', { default: true })

const MAX_SECONDS = 86400
const PRESETS = SLOW_MODE_PRESETS
const UNITS: readonly CustomUnit[] = ['seconds', 'minutes', 'hours']

function split(seconds: number): { amount: string; unit: CustomUnit } {
  if (seconds > 0 && seconds % 3600 === 0) return { amount: String(seconds / 3600), unit: 'hours' }
  if (seconds > 0 && seconds % 60 === 0) return { amount: String(seconds / 60), unit: 'minutes' }
  return { amount: seconds > 0 ? String(seconds) : '', unit: 'seconds' }
}

const isPreset = (seconds: number) => (PRESETS as readonly number[]).includes(seconds)
const initial = split(isPreset(model.value) ? 0 : model.value)
const amount = ref<string | number>(initial.amount)
const amountText = computed(() => String(amount.value ?? '').trim())
const unit = ref<CustomUnit>(initial.unit)

const customSet = computed(() => amountText.value !== '')
const parsed = computed(() => customSeconds(amountText.value, unit.value))
const error = computed(() => {
  if (!customSet.value) return ''
  if (parsed.value === null) return t('moderation.slowModeInvalid')
  if (parsed.value > MAX_SECONDS) return t('moderation.slowModeTooLong')
  return ''
})
watch(error, e => { valid.value = e === '' }, { immediate: true })

function label(seconds: number): string {
  return seconds === 0 ? t('moderation.slowModeOff') : durationLabel(seconds)
}

function choose(seconds: number) {
  amount.value = ''
  model.value = seconds
}

function onCustom() {
  if (parsed.value !== null && parsed.value <= MAX_SECONDS) model.value = parsed.value
}

// A cleared field leaves no chip pressed, so what is saved must be what shows: Off.
function onAmount() {
  if (!customSet.value) model.value = 0
  else onCustom()
}

// A value that arrives from outside (the channel loads late) shows in the field or the chips.
watch(model, seconds => {
  if (customSet.value && parsed.value === seconds) return
  const next = split(isPreset(seconds) ? 0 : seconds)
  amount.value = next.amount
  unit.value = next.unit
})
</script>

<template>
  <div class="stack-1.5" data-testid="slow-mode">
    <div class="flex flex-wrap gap-1.5" role="group" :aria-label="$t('moderation.slowMode')">
      <button
        v-for="s in PRESETS"
        :key="s"
        type="button"
        :data-testid="`slow-mode-${s}`"
        :aria-pressed="!customSet && model === s ? 'true' : 'false'"
        :class="[
          'rounded-md border px-2.5 py-1 text-sm transition-colors',
          !customSet && model === s
            ? 'border-mnema-accent bg-mnema-accent/10 text-mnema-text'
            : 'border-mnema-border-field text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover'
        ]"
        @click="choose(s)"
      >{{ label(s) }}</button>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <label for="slow-mode-custom" class="text-xs text-mnema-tertiary">{{ $t('moderation.slowModeCustom') }}</label>
      <input
        id="slow-mode-custom"
        v-model="amount"
        type="number"
        min="1"
        inputmode="numeric"
        data-testid="slow-mode-custom"
        class="w-20 rounded-md border border-mnema-border-field bg-mnema-canvas px-2 py-1 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
        @input="onAmount"
      />
      <select
        v-model="unit"
        data-testid="slow-mode-unit"
        :aria-label="$t('moderation.slowModeUnit')"
        class="rounded-md border border-mnema-border-field bg-mnema-canvas px-2 py-1 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
        @change="onCustom"
      >
        <option v-for="u in UNITS" :key="u" :value="u">{{ $t(`moderation.unitNames.${u}`) }}</option>
      </select>
    </div>
    <p v-if="error" role="alert" class="text-xs text-mnema-danger" data-testid="slow-mode-error">{{ error }}</p>
    <p v-else class="text-xs text-mnema-tertiary">{{ $t('moderation.slowModeHint') }}</p>
  </div>
</template>
