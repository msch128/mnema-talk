<script setup lang="ts">
import { computed } from 'vue'
import { Pin, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'

// Compact notes under the messages: someone pinned a message in this channel.
// They live in memory until dismissed; the message stays reachable in the pinned list.
const props = defineProps({ channelId: { type: String, required: true } })
const chat = useChatStore()
const notes = computed(() => chat.pinNotices.filter(n => n.channel_id === props.channelId))

function view(messageId: string, noteId: number) {
  chat.dismissPinNotice(noteId)
  void chat.jumpToMessage(messageId)
}
</script>

<template>
  <div v-if="notes.length" class="flex flex-col gap-1 px-4 pb-2" data-testid="pin-notices">
    <div
      v-for="n in notes"
      :key="n.id"
      role="status"
      class="flex items-center gap-2 text-xs text-mnema-tertiary"
    >
      <Pin class="h-3.5 w-3.5 flex-shrink-0 text-mnema-accent" />
      <span class="min-w-0 truncate">
        {{ $t('pins.notice', { name: n.actor_name }) }}
      </span>
      <button type="button" class="flex-shrink-0 font-medium text-mnema-accent hover:underline" @click="view(n.message_id, n.id)">{{ $t('pins.view') }}</button>
      <button
        type="button"
        class="ml-auto flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full transition hover:bg-mnema-surface hover:text-mnema-text"
        :aria-label="$t('pins.dismiss')"
        @click="chat.dismissPinNotice(n.id)"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
  </div>
</template>
