<script setup lang="ts">
import { AUTO_DELETE_DAYS } from '../lib/autoDelete'
import { useServerFeaturesStore } from '../stores/serverFeatures'

// Opt-in auto-delete of a channel, in the channel settings. A field of the
// channel's draft: the dialog stores it with Save, after asking (turning it on
// destroys data, see confirmAutoDelete in lib/autoDelete).
const days = defineModel<number>({ required: true })
const features = useServerFeaturesStore()
</script>

<template>
  <div v-if="features.channelAutoDelete" class="stack-1.5" data-testid="auto-delete-field">
    <label for="edit-auto-delete" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('autoDelete.label') }}
    </label>
    <select
      id="edit-auto-delete"
      v-model.number="days"
      data-testid="auto-delete-select"
      class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
    >
      <option :value="0">{{ $t('autoDelete.off') }}</option>
      <option v-for="d in AUTO_DELETE_DAYS" :key="d" :value="d">{{ $t(`autoDelete.choices.${d}`) }}</option>
    </select>
    <p class="text-xs text-mnema-tertiary">{{ $t('autoDelete.hint') }}</p>
  </div>
</template>
