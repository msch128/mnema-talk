<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../types/domain'
// A message of a member the reader blocked, folded into one quiet line. The
// text is not rendered at all until the reader asks; the row keeps the
// message's place (and id) in the list, so links and jumps still land.
import { EyeOff } from '@lucide/vue'

defineProps({
  msg: { type: Object as PropType<Message>, required: true },
  // In the narrow thread panel, without the avatar gutter of the channel list.
  compact: { type: Boolean, default: false }
})
const emit = defineEmits<{ show: [] }>()
</script>

<template>
  <div
    :data-msg-id="msg.id"
    data-testid="blocked-message"
    tabindex="0"
    role="article"
    :class="['mt-1 flex items-center gap-2 py-1 text-sm text-mnema-tertiary focus:outline-hidden focus-visible:bg-mnema-surface/40', compact ? 'px-2' : 'pl-[72px] pr-12 max-md:pl-[60px] max-md:pr-3']"
  >
    <EyeOff class="h-4 w-4 shrink-0" aria-hidden="true" />
    <span class="italic">{{ $t('blocks.collapsed') }}</span>
    <span aria-hidden="true">·</span>
    <button
      type="button"
      data-testid="blocked-message-show"
      class="rounded-sm px-1 text-mnema-accent underline-offset-2 hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:min-h-11"
      @click="emit('show')"
    >{{ $t('blocks.show') }}</button>
  </div>
</template>
