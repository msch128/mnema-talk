<script setup lang="ts">
import type { PropType } from 'vue'
import type { Message } from '../../types/domain'
import BlockedFold from '../../shared/components/BlockedFold.vue'
// A message of a member the reader blocked, folded into one quiet line in the
// message list or the thread. The row keeps the message's place (and id) in
// the list, so links and jumps still land; the fold itself is BlockedFold.

defineProps({
  msg: { type: Object as PropType<Message>, required: true },
  // In the narrow thread panel, without the avatar gutter of the channel list.
  compact: { type: Boolean, default: false }
})
</script>

<template>
  <div
    :data-msg-id="msg.id"
    data-testid="blocked-message"
    tabindex="0"
    role="article"
    :class="['mt-1 flex items-center py-1 text-sm text-mnema-tertiary focus:outline-hidden focus-visible:bg-mnema-surface/40', compact ? 'px-2' : 'pl-[72px] pr-12 max-md:pl-[60px] max-md:pr-3']"
  >
    <BlockedFold :msg="msg" />
  </div>
</template>
