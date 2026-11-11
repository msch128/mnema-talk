<script setup lang="ts">
// The frame of the channel header's popovers (mentions, saved, reminders,
// pins): a card under the header icon, a full-width sheet on a phone, a title
// that stays put while the rows scroll. The rows are the default slot.
defineProps({
  title: { type: String, required: true }
})
</script>

<template>
  <div
    role="dialog"
    :aria-label="title"
    class="absolute right-0 top-full z-30 mt-2 flex max-h-[min(70vh,34rem)] w-[min(24rem,calc(100vw-1.5rem))] flex-col max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:max-h-[calc(100dvh-4.5rem-env(safe-area-inset-top))] max-md:w-auto overflow-hidden rounded-xl bg-mnema-elevated shadow-popover pop-in"
  >
    <div class="shrink-0 border-b border-mnema-hairline px-4 py-3 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ title }}</div>
    <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
      <slot />
    </div>
  </div>
</template>
