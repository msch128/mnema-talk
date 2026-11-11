<script setup lang="ts">
import { useId } from 'vue'
import { FREQUENCIES } from '../lib/eventRecurrence'

// Does a new event repeat, and until when? Calm: one select, and the end date only once it repeats.
const repeat = defineModel<string>('repeat', { required: true })
const until = defineModel<string>('until', { required: true })
const ids = { repeat: useId(), until: useId() }
</script>

<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2" data-testid="event-repeat">
    <div>
      <label :for="ids.repeat" class="mb-1 block text-sm font-medium text-mnema-text">{{ $t('events.repeat.label') }}</label>
      <select
        :id="ids.repeat"
        v-model="repeat"
        data-testid="event-repeat-select"
        class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden max-md:min-h-11"
      >
        <option value="">{{ $t('events.repeat.none') }}</option>
        <option v-for="f in FREQUENCIES" :key="f" :value="f">{{ $t(`events.repeat.option.${f}`) }}</option>
      </select>
    </div>
    <div v-if="repeat">
      <label :for="ids.until" class="mb-1 block text-sm font-medium text-mnema-text">{{ $t('events.repeat.until') }}</label>
      <input
        :id="ids.until"
        v-model="until"
        type="date"
        data-testid="event-repeat-until"
        class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden max-md:min-h-11"
      />
      <p class="mt-1 text-xs text-mnema-tertiary">{{ $t('events.repeat.untilHint') }}</p>
    </div>
  </div>
</template>
