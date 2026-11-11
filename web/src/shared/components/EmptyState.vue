<script setup lang="ts">
// A calm placeholder for a list with nothing in it: a muted icon, one sentence
// and, when there is a next step, the action in the default slot.
import type { Component } from 'vue'

defineProps<{
  icon: Component
  // What goes here, in one sentence.
  text: string
  // Optional second line: what to do next.
  hint?: string
}>()
</script>

<template>
  <div class="flex flex-col items-center gap-2 px-6 py-8 text-center" data-testid="empty-state">
    <component :is="icon" class="h-6 w-6 text-mnema-tertiary" aria-hidden="true" />
    <p class="text-sm text-mnema-muted">{{ text }}</p>
    <p v-if="hint" class="max-w-sm text-xs text-mnema-tertiary">{{ hint }}</p>
    <div v-if="$slots['default']" class="mt-1"><slot /></div>
  </div>
</template>
