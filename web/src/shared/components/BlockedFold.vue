<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { EyeOff } from '@lucide/vue'
import { useBlocksStore } from '../../stores/blocks'
import { focusRevealed } from '../lib/revealFocus'

// The one way a blocked member's words are folded: wherever a message of
// another member is shown (the message list, a reply line, a quote, a search
// hit, a pin, a saved item, a forum post, an event comment), it is wrapped in
// this. A message whose author the reader blocked shows "Blocked message ·
// Show" instead of the slot; the slot is not rendered at all until the reader
// asks. Showing it reveals that message everywhere for the session
// (stores/blocks.ts decides) and moves focus to what was revealed.
// Attributes (a class for the place) go to the placeholder only.
defineOptions({ inheritAttrs: false })
const props = defineProps<{
  msg: { id: string; user_id?: string | undefined }
  // In a one-line place (a reply line, a search root): no touch-sized button.
  small?: boolean
  // Inside a control that reveals the message itself (a search hit, where
  // Enter shows it): no button of its own, the hint is read out instead.
  passive?: boolean
}>()
const blocks = useBlocksStore()
// Set when Show was pressed here: the markers around the revealed slot let
// focus find it (BlockedMessageRow's row is replaced instead, see focusRevealed).
const shownHere = ref(false)
const start = ref<HTMLElement | null>(null)
const end = ref<HTMLElement | null>(null)

async function show() {
  shownHere.value = true
  blocks.reveal(props.msg.id)
  await nextTick()
  focusRevealed(start.value, end.value, props.msg.id)
}
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
    <template v-if="passive">
      <span aria-hidden="true">· <span class="text-mnema-accent">{{ $t('blocks.show') }}</span></span>
      <span class="sr-only">{{ $t('blocks.showWithEnter') }}</span>
    </template>
    <template v-else>
      <span aria-hidden="true">·</span>
      <button
        type="button"
        data-testid="blocked-message-show"
        :class="['shrink-0 rounded-sm px-1 text-mnema-accent underline-offset-2 hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent', small ? '' : 'max-md:min-h-11']"
        @click.stop="show"
        @keydown.enter.stop
        @keydown.space.stop
      >{{ $t('blocks.show') }}</button>
    </template>
  </span>
  <template v-else>
    <span v-if="shownHere" ref="start" hidden></span>
    <slot />
    <span v-if="shownHere" ref="end" hidden></span>
  </template>
</template>
