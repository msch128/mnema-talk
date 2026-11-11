<script setup lang="ts">
// The quiet "Saved" of a personal preference. The live region is always there,
// so screen readers announce the text when it appears.
import { Check } from '@lucide/vue'

defineProps({ visible: { type: Boolean, default: false } })
</script>

<template>
  <span role="status" aria-live="polite" class="inline-flex min-h-5 items-center gap-1 text-xs text-mnema-tertiary" data-testid="saved-indicator">
    <template v-if="visible">
      <Check class="h-3.5 w-3.5 text-mnema-accent" aria-hidden="true" />
      {{ $t('draft.saved') }}
    </template>
  </span>
</template>
