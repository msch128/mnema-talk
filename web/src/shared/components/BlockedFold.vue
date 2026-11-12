<script setup lang="ts">
import { EyeOff } from '@lucide/vue'
import { useBlocksStore } from '../../stores/blocks'

// The one way a blocked member's words are folded: wherever a message of
// another member is shown (the message list, a reply line, a quote, a search
// hit, a pin, a saved item, a forum post), it is wrapped in this. A message
// whose author the reader blocked shows "Blocked message · Show" instead of
// the slot; the slot is not rendered at all until the reader asks. Showing it
// reveals that message everywhere for the session (stores/blocks.ts decides).
// Attributes (a class for the place) go to the placeholder only.
defineOptions({ inheritAttrs: false })
defineProps<{
  msg: { id: string; user_id?: string | undefined }
  // In a one-line place (a reply line, a search root): no touch-sized button.
  small?: boolean
}>()
const blocks = useBlocksStore()
</script>

<template>
  <span
    v-if="blocks.hides(msg)"
    v-bind="$attrs"
    data-testid="blocked-fold"
    :class="['inline-flex min-w-0 items-center text-mnema-tertiary', small ? 'gap-1' : 'gap-2 text-sm']"
  >
    <EyeOff :class="['shrink-0', small ? 'h-3.5 w-3.5' : 'h-4 w-4']" aria-hidden="true" />
    <span class="truncate italic">{{ $t('blocks.collapsed') }}</span>
    <span aria-hidden="true">·</span>
    <button
      type="button"
      data-testid="blocked-message-show"
      :class="['shrink-0 rounded-sm px-1 text-mnema-accent underline-offset-2 hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent', small ? '' : 'max-md:min-h-11']"
      @click.stop="blocks.reveal(msg.id)"
      @keydown.enter.stop
      @keydown.space.stop
    >{{ $t('blocks.show') }}</button>
  </span>
  <slot v-else />
</template>
