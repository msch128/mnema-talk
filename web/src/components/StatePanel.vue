<script setup lang="ts">
import type { Component, PropType } from 'vue'
import { CircleAlert, Loader2 } from '@lucide/vue'
// What a list or panel shows instead of rows: a spinner while it loads, one
// calm line when it is empty, one line (and a way to retry) when it failed.
// The text is the default slot; an extra action can go in the `action` slot.
defineProps({
  state: { type: String as PropType<'loading' | 'empty' | 'error'>, required: true },
  // Icon above the text of an empty panel.
  icon: { type: Object as PropType<Component | null>, default: null },
  // Show "Try again" on an error; the parent reloads on `retry`.
  retryable: { type: Boolean, default: false }
})
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div
    :role="state === 'error' ? 'alert' : state === 'loading' ? 'status' : undefined"
    class="flex flex-col items-center gap-2 px-4 py-8 text-center text-sm text-mnema-tertiary"
  >
    <template v-if="state === 'loading'">
      <Loader2 class="h-4 w-4 animate-spin text-mnema-accent" aria-hidden="true" />
      <span class="sr-only">{{ $t('common.loading') }}</span>
    </template>
    <CircleAlert v-else-if="state === 'error'" class="h-5 w-5" aria-hidden="true" />
    <component :is="icon" v-else-if="icon" class="h-5 w-5" aria-hidden="true" />
    <p v-if="$slots.default" class="max-w-[28ch] text-balance"><slot /></p>
    <button v-if="state === 'error' && retryable" type="button" data-testid="state-retry" class="btn btn-ghost btn-sm" @click="emit('retry')">
      {{ $t('common.retry') }}
    </button>
    <slot name="action" />
  </div>
</template>
