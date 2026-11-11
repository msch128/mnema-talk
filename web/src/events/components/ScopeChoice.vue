<script setup lang="ts">
import { useId } from 'vue'
import type { Scope } from '../lib/eventRecurrence'

// For an occurrence of a repeating event: does the change apply to this event
// only, or to it and all the later ones? The earlier ones never change.
defineProps<{ hint?: string }>()
const model = defineModel<Scope>({ required: true })
const name = useId()
</script>

<template>
  <fieldset class="rounded-lg border border-mnema-hairline bg-mnema-surface/50 p-3" data-testid="scope-choice">
    <legend class="px-1 text-sm font-medium text-mnema-text">{{ $t('events.scope.title') }}</legend>
    <label class="flex min-h-9 cursor-pointer items-center gap-2 text-sm text-mnema-text max-md:min-h-11">
      <input v-model="model" type="radio" :name="name" value="this" class="accent-mnema-accent" data-testid="scope-this" />
      <span>{{ $t('events.scope.this') }}</span>
    </label>
    <label class="flex min-h-9 cursor-pointer items-center gap-2 text-sm text-mnema-text max-md:min-h-11">
      <input v-model="model" type="radio" :name="name" value="following" class="accent-mnema-accent" data-testid="scope-following" />
      <span>{{ $t('events.scope.following') }}</span>
    </label>
    <p v-if="hint" class="mt-1 text-xs leading-relaxed text-mnema-tertiary">{{ hint }}</p>
  </fieldset>
</template>
