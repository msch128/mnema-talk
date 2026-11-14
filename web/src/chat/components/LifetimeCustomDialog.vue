<script setup lang="ts">
// A lifetime of the member's own: a number and a unit (minutes, hours, days),
// within 1 minute and the community's ceiling.
import { computed, ref } from 'vue'
import BaseDialog from '../../shared/components/BaseDialog.vue'
import { UNIT_SECONDS, lifetimeLabel, validLifetime, type LifetimeUnit } from '../../shared/lib/lifetime'

const props = defineProps({
  initial: { type: Number, default: 0 },
  max: { type: Number, required: true }
})
const emit = defineEmits<{ pick: [seconds: number]; close: [] }>()

function start(): { amount: number; unit: LifetimeUnit } {
  const s = props.initial
  if (s && s % UNIT_SECONDS.days === 0) return { amount: s / UNIT_SECONDS.days, unit: 'days' }
  if (s && s % UNIT_SECONDS.hours === 0) return { amount: s / UNIT_SECONDS.hours, unit: 'hours' }
  return { amount: s ? Math.round(s / 60) : 10, unit: 'minutes' }
}
const first = start()
const amount = ref<number>(first.amount)
const unit = ref<LifetimeUnit>(first.unit)
const seconds = computed(() => Number(amount.value) * UNIT_SECONDS[unit.value])
const valid = computed(() => validLifetime(seconds.value, props.max))

function submit() {
  if (valid.value) emit('pick', seconds.value)
}
</script>

<template>
  <BaseDialog :title="$t('lifetime.customTitle')" panel-class="max-w-sm" @close="emit('close')">
    <form class="space-y-3 p-4" data-testid="lifetime-custom" @submit.prevent="submit">
      <div class="flex flex-wrap items-center gap-2">
        <label class="sr-only" for="lifetime-amount">{{ $t('lifetime.amount') }}</label>
        <input
          id="lifetime-amount"
          v-model.number="amount"
          type="number"
          min="1"
          step="1"
          inputmode="numeric"
          :aria-invalid="valid ? undefined : 'true'"
          aria-describedby="lifetime-limits"
          class="w-24 rounded-md border border-mnema-border-field bg-mnema-canvas px-2 py-1.5 text-sm tabular-nums text-mnema-text outline-hidden focus:border-mnema-accent"
        />
        <label class="sr-only" for="lifetime-unit">{{ $t('lifetime.unit') }}</label>
        <select id="lifetime-unit" v-model="unit" class="rounded-md border border-mnema-border-field bg-mnema-canvas px-2 py-1.5 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent">
          <option value="minutes">{{ $t('lifetime.units.minutes') }}</option>
          <option value="hours">{{ $t('lifetime.units.hours') }}</option>
          <option value="days">{{ $t('lifetime.units.days') }}</option>
        </select>
      </div>
      <p id="lifetime-limits" :class="['text-xs', valid ? 'text-mnema-tertiary' : 'text-mnema-danger']">
        {{ $t('lifetime.limits', { max: lifetimeLabel(max) }) }}
      </p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="!valid">{{ $t('lifetime.use') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
