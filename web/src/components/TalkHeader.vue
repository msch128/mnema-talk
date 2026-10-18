<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
import { MessageSquare, UserRoundX, Users, Volume2 } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import TalkParticipants from './TalkParticipants.vue'
import VoiceTimer from './VoiceTimer.vue'
import { headerButton } from './talkButtons'

// The Talk's header: its name, who is there and for how long, and the
// buttons for the Talk's chat, "only participants with video" and the member list.
defineProps({
  channelName: { type: String, default: '' },
  users: { type: Array as PropType<TalkParticipant[]>, required: true },
  startedAt: { type: String, default: '' },
  narrow: { type: Boolean, default: false },
  connected: { type: Boolean, default: false },
  showChat: { type: Boolean, default: false },
  chatUnread: { type: String, default: '' }
})
const emit = defineEmits<{ 'update:showChat': [show: boolean] }>()

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
</script>

<template>
  <header class="h-12 px-4 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 flex-shrink-0 z-10">
    <div class="flex items-center gap-3 min-w-0">
      <div
        v-if="!narrow"
        class="w-8 h-8 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint flex-shrink-0"
        aria-hidden="true"
      >
        <Volume2 class="w-4 h-4" />
      </div>
      <div class="flex items-center gap-2 min-w-0">
        <h2 class="font-semibold text-base leading-5 text-mnema-text truncate min-w-[3rem]">
          {{ channelName || $t('voice.channelFallback') }}
        </h2>
        <TalkParticipants class="flex-shrink-0" :users="users" :started-at="startedAt" />
        <VoiceTimer
          v-if="startedAt && !narrow"
          :since="startedAt"
          data-testid="talk-timer"
          v-tooltip="$t('talk.runningForTip')"
          class="text-xs text-mnema-tertiary flex-shrink-0"
        />
      </div>
    </div>

    <!-- Top Right Actions -->
    <div class="flex items-center gap-2 flex-shrink-0">
      <!-- The Talk's chat (under the stage), with its unread count while closed -->
      <button
        type="button"
        data-testid="voice-chat-toggle"
        @click="emit('update:showChat', !showChat)"
        :class="headerButton(showChat)"
        v-tooltip.visual="showChat ? $t('talk.closeChat') : $t('talk.openChat')"
        :aria-label="chatUnread ? $t('talk.openChatUnread', { count: chatUnread }) : showChat ? $t('talk.closeChat') : $t('talk.openChat')"
        :aria-pressed="showChat ? 'true' : 'false'"
      >
        <MessageSquare class="w-4 h-4" />
        <span
          v-if="chatUnread"
          data-testid="voice-chat-unread"
          class="absolute -top-1.5 -right-1.5 min-w-[18px] h-[18px] px-1 rounded-full bg-mnema-danger text-white text-[11px] font-bold leading-[18px] text-center ring-2 ring-mnema-canvas"
          aria-hidden="true"
        >{{ chatUnread }}</span>
      </button>

      <!-- Only participants with video (per browser) -->
      <button
        v-if="connected"
        type="button"
        data-testid="hide-no-video"
        @click="voiceStore.toggleHideNoVideo()"
        :class="headerButton(voiceStore.hideNoVideo)"
        v-tooltip="$t('talk.hideNoVideo')"
        :aria-pressed="voiceStore.hideNoVideo ? 'true' : 'false'"
      >
        <UserRoundX class="w-4 h-4" />
      </button>

      <!-- Toggle Member List Sidebar -->
      <button
        type="button"
        @click="chatStore.showMemberList = !chatStore.showMemberList"
        :class="headerButton(chatStore.showMemberList)"
        v-tooltip="$t('members.toggle')"
        :aria-pressed="chatStore.showMemberList ? 'true' : 'false'"
      >
        <Users class="w-4 h-4" />
      </button>
    </div>
  </header>
</template>
