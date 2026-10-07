<script setup lang="ts">
import type { PropType } from 'vue'
import type { ResizePanel } from './presentationTypes'
// A voice channel's own text chat, under its Talk's stage (like Discord's
// chat next to a voice channel). The whole chat is the regular one
// (ChatArea in its panel variant): messages, attachments, reactions, edits,
// replies, threads, mentions and typing work as in a text channel.
// Its height is dragged on the handle at the top (useResizable, axis 'y').
import { watch, onMounted, onUnmounted } from 'vue'
import { Loader2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import ChatArea from './ChatArea.vue'
import ResizeHandle from './ResizeHandle.vue'

const props = defineProps({
  channelId: { type: String, required: true },
  // Height handle from useResizable (axis 'y'); without one the panel takes 300px.
  panel: { type: Object as PropType<ResizePanel | null>, default: null }
})
const emit = defineEmits<{ close: [] }>()

const chatStore = useChatStore()

// The chat shows the active channel: make it this Talk's channel (it usually
// already is: opening /v/:id selects it).
watch(() => props.channelId, id => {
  if (!id || chatStore.activeChannel?.id === id) return
  const ch = chatStore.allChannels.find(c => c.id === id)
  if (ch) chatStore.selectChannel(ch)
}, { immediate: true })

// While the panel is open its messages count as read (unreads otherwise).
onMounted(() => chatStore.setVoiceChatReading(true))
onUnmounted(() => {
  chatStore.setVoiceChatReading(false)
  // A thread opened from this chat closes with it.
  const thread = chatStore.activeThread
  if (thread && (!thread.channel_id || thread.channel_id === props.channelId)) chatStore.closeThread()
})
</script>

<template>
  <div
    data-testid="voice-chat-panel"
    class="relative flex-shrink-0 min-h-0 flex flex-col border-t border-mnema-hairline bg-mnema-canvas"
    :style="{ height: `${panel ? panel.width : 300}px` }"
  >
    <ResizeHandle v-if="panel" :panel="panel" :label="$t('resize.voiceChat')" />
    <ChatArea
      v-if="chatStore.activeChannel?.id === channelId"
      panel
      class="min-h-0"
      @close="emit('close')"
    />
    <div v-else class="flex-1 flex items-center justify-center" role="status" :aria-label="$t('talk.chatLoading')">
      <Loader2 class="w-6 h-6 animate-spin text-mnema-accent" />
    </div>
  </div>
</template>
