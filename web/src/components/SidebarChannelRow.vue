<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel, VoiceUser } from '../types/domain'
// One channel in the sidebar: the row itself, plus the people connected to
// it for a voice channel. Admins can drag the row (and its users with it) or
// move it with Alt+ArrowUp / Alt+ArrowDown.
import { computed } from 'vue'
import { Hash, Volume2, Trash2, Monitor, BellOff, MessageSquare } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { channelDraftKey, hasDraft } from '../lib/drafts'
import UserAvatar from './UserAvatar.vue'
import VoiceTimer from './VoiceTimer.vue'
import MuteMarks from './MuteMarks.vue'

const props = defineProps({
  channel: { type: Object as PropType<Channel>, required: true },
  admin: { type: Boolean, default: false },
  // This row is being dragged.
  dragging: { type: Boolean, default: false },
  // Drop indicator: 'top' | 'bottom' | ''.
  indicator: { type: String as PropType<'' | 'top' | 'bottom'>, default: '' },
  // Briefly highlighted (moved, duplicated).
  flash: { type: Boolean, default: false },
  // id of the hint that tells admins how to move rows.
  hintId: { type: String, default: '' }
})

const emit = defineEmits<{ open: []; 'open-chat': []; menu: [event: MouseEvent | KeyboardEvent]; delete: []; 'drag-start': [event: PointerEvent]; move: [direction: -1 | 1]; 'voice-user-click': [user: VoiceUser]; 'member-menu': [event: MouseEvent | KeyboardEvent, user: VoiceUser] }>()

function moveKey(e: KeyboardEvent, dir: -1 | 1) {
  if (!props.admin) return
  e.preventDefault()
  emit('move', dir)
}

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()

const isVoice = computed(() => props.channel.type === 'voice')
const active = computed(() =>
  isVoice.value
    ? voiceStore.currentChannelId === props.channel.id && voiceStore.activeView === 'voice'
    : chatStore.activeChannel?.id === props.channel.id && voiceStore.activeView === 'chat'
)
// The channel's messages are on screen: the open text channel, or a voice
// channel whose chat panel is open next to its Talk. Then no badges.
const reading = computed(() =>
  isVoice.value
    ? chatStore.voiceChatReading && chatStore.activeChannel?.id === props.channel.id && voiceStore.activeView === 'voice'
    : active.value
)
// An unsent message waits here (the open channel shows it in its composer).
const draft = computed(() => !reading.value && hasDraft(authStore.user?.id, channelDraftKey(props.channel.id)))
// Voice channels have their own text chat, so unreads and mentions too.
const muted = computed(() => chatStore.readStates[props.channel.id]?.notify_level === 'mute')
// A muted channel keeps its mentions but does not announce plain unreads.
const unread = computed(() => (muted.value ? 0 : chatStore.readStates[props.channel.id]?.unread_count || 0))
const mentions = computed(() => chatStore.readStates[props.channel.id]?.mention_count || 0)
const voiceUsers = computed(() => {
  if (!isVoice.value) return []
  const users = voiceStore.channelUsers[props.channel.id]
  return users ? Object.values(users) : []
})

// Shared row styling (34px channel rows, rounded hover/selected states).
const rowClass = computed(() => [
  'relative w-full h-[34px] max-md:h-11 flex items-center justify-between gap-1.5 px-2 mb-px rounded-md text-nav transition-colors motion-reduce:transition-none group cursor-pointer text-left min-w-0 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent',
  active.value
    ? 'bg-mnema-hover text-mnema-text font-medium shadow-[inset_2px_0_0_0_#2DA771]'
    : unread.value > 0
      ? 'text-mnema-text font-semibold hover:bg-mnema-hover/70'
      : 'text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text',
  muted.value && !active.value && 'opacity-60',
  props.flash && 'nav-flash'
])
</script>

<template>
  <div
    :class="['relative', dragging && 'opacity-40']"
    :data-channel-item="channel.id"
  >
    <span
      v-if="indicator === 'top' || indicator === 'bottom'"
      :class="['drop-line', indicator === 'top' ? '-top-px' : '-bottom-px']"
      data-drop-indicator
      aria-hidden="true"
    ></span>

    <div
      :class="rowClass"
      role="button"
      tabindex="0"
      data-drop="channel"
      :data-id="channel.id"
      :data-channel-type="channel.type"
      aria-haspopup="menu"
      :aria-describedby="admin && hintId ? hintId : undefined"
      :aria-keyshortcuts="admin ? 'Alt+ArrowUp Alt+ArrowDown' : undefined"
      @click="emit('open')"
      @contextmenu="emit('menu', $event)"
      @pointerdown="emit('drag-start', $event)"
      @keydown.enter.self.prevent="emit('open')"
      @keydown.space.self.prevent="emit('open')"
      @keydown.f10.shift.self.prevent="emit('menu', $event)"
      @keydown.context-menu.self.prevent="emit('menu', $event)"
      @keydown.alt.up.self="moveKey($event, -1)"
      @keydown.alt.down.self="moveKey($event, 1)"
    >
      <!-- Unread pip on left edge -->
      <span
        v-if="unread > 0 && !reading"
        class="absolute -left-1.5 w-1 h-2 rounded-r bg-mnema-text"
      ></span>

      <div class="flex items-center gap-1.5 min-w-0">
        <Volume2
          v-if="isVoice"
          :class="['w-5 h-5 shrink-0 transition-colors', voiceStore.currentChannelId === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text']"
        />
        <Hash
          v-else
          :class="[
            'w-5 h-5 shrink-0 transition-colors',
            chatStore.activeChannel?.id === channel.id
              ? 'text-mnema-accent'
              : unread > 0
                ? 'text-mnema-text'
                : 'text-mnema-tertiary group-hover:text-mnema-text'
          ]"
        />
        <span class="truncate">{{ channel.name }}</span>
        <span v-if="draft" class="shrink-0 rounded-sm bg-mnema-surface px-1 text-[10px] font-semibold uppercase tracking-wide text-mnema-tertiary" data-testid="channel-draft">{{ $t('sidebar.draft') }}</span>
        <BellOff v-if="muted" class="w-3.5 h-3.5 shrink-0 text-mnema-tertiary" data-testid="channel-muted" :aria-label="$t('notifications.mute')" />
      </div>

      <!-- Unread & Mention Badges; for a Talk in use, how long it runs -->
      <div class="flex items-center gap-1 ml-auto shrink-0">
        <VoiceTimer
          v-if="isVoice && voiceStore.roomStartedAt[channel.id]"
          :since="voiceStore.roomStartedAt[channel.id]"
          data-testid="sidebar-talk-timer"
          class="text-xs text-mnema-tertiary"
        />
        <span
          v-if="mentions > 0 && !reading"
          data-testid="channel-mentions"
          class="px-1.5 py-0.5 rounded-full bg-mnema-danger text-white text-xs font-bold leading-none min-w-[18px] text-center"
        >
          {{ mentions }}
        </span>
        <span
          v-else-if="unread > 0 && !reading"
          data-testid="channel-unread"
          class="px-1.5 py-0.5 rounded-full bg-mnema-surface border border-mnema-border text-mnema-text text-xs font-semibold leading-none min-w-[18px] text-center"
        >
          {{ unread }}
        </span>
      </div>

      <!-- A Talk's chat can be read without joining the Talk -->
      <button
        v-if="isVoice"
        type="button"
        data-no-drag
        data-testid="talk-open-chat"
        @click.stop="emit('open-chat')"
        @pointerdown.stop
        v-tooltip="$t('talk.openChat')"
        :aria-label="$t('talk.openChat')"
        class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100 max-md:opacity-100 max-md:z-10 w-6 h-6 max-md:w-11 max-md:h-11 flex items-center justify-center rounded-sm text-mnema-tertiary hover:text-mnema-text focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent transition shrink-0"
      >
        <MessageSquare class="w-4 h-4" />
      </button>

      <button
        v-if="admin"
        type="button"
        data-no-drag
        @click.stop="emit('delete')"
        v-tooltip="isVoice ? $t('sidebar.deleteVoiceChannel') : $t('sidebar.deleteChannel')"
        :aria-label="isVoice ? $t('sidebar.deleteVoiceChannel') : $t('sidebar.deleteChannel')"
        class="max-md:hidden opacity-0 group-hover:opacity-100 focus-visible:opacity-100 w-6 h-6 flex items-center justify-center rounded-sm text-mnema-tertiary hover:text-mnema-danger transition shrink-0"
      >
        <Trash2 class="w-4 h-4" />
      </button>
    </div>

    <!-- Connected voice users (indented, 24px avatars, speaking ring) -->
    <div v-if="voiceUsers.length" class="pl-7 pb-1" data-drop="channel-tail" :data-id="channel.id">
      <div
        v-for="user in voiceUsers"
        :key="user.id"
        class="min-h-8 max-md:min-h-11 py-1 flex items-center gap-2 px-2 rounded-md text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text transition-colors min-w-0 cursor-pointer focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent"
        role="button"
        tabindex="0"
        aria-haspopup="menu"
        data-voice-user
        @click="emit('voice-user-click', user)"
        @keydown.enter.self.prevent="emit('voice-user-click', user)"
        @contextmenu="emit('member-menu', $event, user)"
        @keydown.f10.shift.self.prevent="emit('member-menu', $event, user)"
        @keydown.context-menu.self.prevent="emit('member-menu', $event, user)"
      >
        <UserAvatar :user="user" size="xs" :is-speaking="voiceStore.isSpeaking(user.id)" />
        <span class="flex min-w-0 flex-col leading-4">
          <span class="truncate text-sm">{{ user.display_name || user.username }}</span>
          <VoiceTimer :since="user.joined_at" class="text-[11px] text-mnema-tertiary" />
        </span>
        <span class="ml-auto flex shrink-0 items-center gap-1.5">
          <MuteMarks
            :muted="voiceStore.muteStateOf(user.id).muted"
            :deafened="voiceStore.muteStateOf(user.id).deafened"
            :size="14"
          />
          <span
            v-if="voiceStore.mediaState[user.id]?.screen"
            class="px-1.5 py-0.5 rounded-sm bg-red-600 text-white text-[10px] font-black uppercase tracking-wider flex items-center gap-1 shadow-xs shrink-0"
            data-testid="sidebar-live-badge"
            v-tooltip="$t('talk.liveTooltip')"
          >
            <Monitor class="w-3 h-3" />
            {{ $t('talk.live') }}
          </span>
          <span v-else-if="user.role === 'admin'" class="text-xs px-1 rounded-sm bg-amber-500/10 text-amber-400 shrink-0">
            {{ $t('role.admin') }}
          </span>
        </span>
      </div>
    </div>
  </div>
</template>
