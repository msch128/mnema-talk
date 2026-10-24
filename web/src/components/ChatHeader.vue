<script setup lang="ts">
import type { Component } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import { computed, onMounted, ref } from 'vue'
import { Hash, Users, Bell, BellOff, AtSign, X, Volume2, Search, MessageCircle, Info } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useNotificationHint } from '../composables/useNotificationHint'
import { durationLabel } from '../lib/moderation'
import MentionsInbox from './MentionsInbox.vue'
import SavedMessages from './SavedMessages.vue'
import LaterPopover from './LaterPopover.vue'
import PinnedMessages from './PinnedMessages.vue'
import TipBar from './TipBar.vue'
import NavDrawerButton from './NavDrawerButton.vue'
import DmInfoDialog from './DmInfoDialog.vue'
import DmTalkActions from './DmTalkActions.vue'
import { useDmChat } from '../composables/useDmChat'
import { openSearch } from '../lib/globalSearch'
import { quoteValue } from '../lib/searchQuery'

// The channel header (48px, aligned with the side columns; 40px in the Talk's
// chat) and the desktop notification opt-in under it, shown until answered or dismissed.
const props = defineProps({
  panel: { type: Boolean, default: false }
})
const emit = defineEmits<{ close: []; menu: [x: number, y: number, items: ContextMenuItem[]] }>()

const chatStore = useChatStore()
// A conversation takes the channel's place: no topic, pins or notification menu, and its members instead of the community's.
const { dm, isDm } = useDmChat()
const showInfo = ref(false)
const serverFeatures = useServerFeaturesStore()
onMounted(() => { void serverFeatures.load() })
const channelIcon = computed<Component>(() => {
  if (props.panel) return Volume2
  return isDm.value ? MessageCircle : Hash
})
const { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems } = useNotificationHint(() => props.panel)

function searchInChannel() {
  const channel = chatStore.activeChannel
  if (!channel) return
  if (isDm.value) void openSearch(`in:${quoteValue(channel.name)} `, 'dm')
  else void openSearch(`in:${quoteValue(channel.name)} `)
}

// The name keeps the room it needs; only beside a topic is it capped so the
// topic stays visible too.
const titleClass = computed(() => {
  if (props.panel || !chatStore.activeChannel?.topic) return 'min-w-0'
  return 'shrink-0 max-w-[60%] max-md:shrink max-md:max-w-none'
})

function openNotificationMenu(e: MouseEvent) {
  const channel = chatStore.activeChannel
  if (!channel) return
  if (!(e.currentTarget instanceof Element)) return
  const rect = e.currentTarget.getBoundingClientRect()
  emit('menu', Math.max(8, rect.right - 200), rect.bottom + 4, notificationMenuItems(channel.id))
}
</script>

<template>
  <header
    :class="[
      'px-4 max-md:pl-1 max-md:pr-2 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 shrink-0 z-10',
      panel ? 'h-10' : 'h-12'
    ]"
  >
    <!-- Left: Channel name & topic -->
    <div class="flex items-center gap-2 min-w-0 flex-1">
      <NavDrawerButton v-if="!panel" />
      <component :is="channelIcon" class="w-5 h-5 max-md:hidden text-mnema-tertiary shrink-0" />
      <span
        data-testid="chat-channel-name"
        :class="['font-semibold text-base text-mnema-text truncate', titleClass]"
      >
        {{ chatStore.activeChannel?.name || $t('chat.selectChannel') }}
      </span>
      <span v-if="chatStore.activeChannel?.topic && !panel" class="max-md:hidden text-sm text-mnema-muted pl-3 ml-1 border-l border-mnema-border truncate min-w-0">
        {{ chatStore.activeChannel.topic }}
      </span>
      <span
        v-if="chatStore.activeChannel?.slow_mode_seconds"
        data-testid="slow-mode-chip"
        class="shrink-0 rounded-md border border-mnema-border px-1.5 py-0.5 font-mono text-xs text-mnema-muted"
        v-tooltip="$t('moderation.slowModeHint')"
      >{{ $t('moderation.slowModeChip', { time: durationLabel(chatStore.activeChannel.slow_mode_seconds) }) }}</span>
    </div>

    <!-- Right: Badges & member list toggle -->
    <div class="flex items-center gap-2 max-md:gap-0 shrink-0">
      <MentionsInbox v-if="!panel" />
      <SavedMessages v-if="!panel && serverFeatures.has('saved')" />
      <LaterPopover v-if="!panel" />
      <PinnedMessages v-if="chatStore.activeChannel && !isDm" />
      <DmTalkActions v-if="isDm && dm && serverFeatures.talksLobby" :conversation-id="dm.id" :can-send="dm.can_send" />
      <button
        v-if="chatStore.activeChannel"
        type="button"
        data-testid="search-in-channel"
        @click="searchInChannel"
        class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
        v-tooltip="$t('chat.searchInChannel')"
        :aria-label="$t('chat.searchInChannel')"
      >
        <Search class="w-5 h-5" />
      </button>
      <button
        v-if="chatStore.activeChannel && !isDm"
        @click="openNotificationMenu"
        class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
        v-tooltip="$t('notifications.channelSettings')"
        aria-haspopup="menu"
      >
        <BellOff v-if="chatStore.notificationLevel(chatStore.activeChannel.id) === 'mute'" class="w-5 h-5" />
        <AtSign v-else-if="chatStore.notificationLevel(chatStore.activeChannel.id) === 'mentions'" class="w-5 h-5" />
        <Bell v-else class="w-5 h-5" />
      </button>
      <!-- The Talk's chat closes; the member list has its toggle in the Talk header -->
      <button
        v-if="panel"
        type="button"
        data-testid="voice-chat-close"
        @click="emit('close')"
        class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
        v-tooltip="$t('talk.closeChat')"
        :aria-label="$t('talk.closeChat')"
      >
        <X class="w-5 h-5" />
      </button>
      <button
        v-else-if="isDm"
        type="button"
        data-testid="dm-info-button"
        class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
        v-tooltip="$t('dm.info')"
        :aria-label="$t('dm.info')"
        @click="showInfo = true"
      >
        <Info class="w-5 h-5" />
      </button>
      <button
        v-else
        @click="chatStore.showMemberList = !chatStore.showMemberList"
        class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
        v-tooltip="$t('members.toggle')"
        :aria-pressed="chatStore.showMemberList ? 'true' : 'false'"
      >
        <Users class="w-5 h-5" />
      </button>
    </div>
  </header>

  <DmInfoDialog v-if="showInfo && dm" :dm="dm" @close="showInfo = false" />

  <!-- Desktop notification opt-in: shown until answered or dismissed -->
  <div
    v-if="showNotifHint"
    class="px-4 py-2 border-b border-mnema-hairline bg-mnema-raised flex items-center gap-3 text-sm shrink-0"
    role="region"
    :aria-label="$t('notifications.hintTitle')"
  >
    <Bell class="w-4 h-4 text-mnema-accent shrink-0" />
    <p class="min-w-0 flex-1 text-mnema-muted">
      <span class="font-medium text-mnema-text">{{ $t('notifications.hintTitle') }}</span>
      <span class="ml-2">{{ $t('notifications.hintBody') }}</span>
    </p>
    <button
      type="button"
      class="btn btn-primary btn-sm shrink-0"
      @click="enableNotifications"
    >
      {{ $t('notifications.enable') }}
    </button>
    <button
      type="button"
      class="btn btn-icon btn-sm hover:text-mnema-text shrink-0"
      :aria-label="$t('notifications.notNow')"
      v-tooltip="$t('notifications.notNow')"
      @click="dismissNotifHint"
    >
      <X class="w-4 h-4" />
    </button>
  </div>

  <!-- One feature tip at a time, never next to the notification hint -->
  <TipBar v-if="!panel && !showNotifHint" />
</template>
