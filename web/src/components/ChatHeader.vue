<script setup lang="ts">
import type { Component } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import { computed } from 'vue'
import { Hash, Users, Bell, BellOff, AtSign, X, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useNotificationHint } from '../composables/useNotificationHint'
import { durationLabel } from '../lib/moderation'

// The channel header (48px, aligned with the side columns; 40px in the Talk's
// chat) and the desktop notification opt-in under it, shown until answered or dismissed.
const props = defineProps({
  panel: { type: Boolean, default: false }
})
const emit = defineEmits<{ close: []; menu: [x: number, y: number, items: ContextMenuItem[]] }>()

const chatStore = useChatStore()
const channelIcon = computed<Component>(() => (props.panel ? Volume2 : Hash))
const { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems } = useNotificationHint(() => props.panel)

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
      'px-4 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 flex-shrink-0 z-10',
      panel ? 'h-10' : 'h-12'
    ]"
  >
    <!-- Left: Channel name & topic -->
    <div :class="['flex items-center gap-2 min-w-0', panel ? 'flex-1' : '']">
      <component :is="channelIcon" class="w-5 h-5 text-mnema-tertiary flex-shrink-0" />
      <span
        data-testid="chat-channel-name"
        :class="['font-semibold text-base text-mnema-text truncate', panel ? 'min-w-0' : 'flex-shrink-0 max-w-[60%]']"
      >
        {{ chatStore.activeChannel?.name || $t('chat.selectChannel') }}
      </span>
      <span v-if="chatStore.activeChannel?.topic && !panel" class="text-sm text-mnema-muted pl-3 ml-1 border-l border-mnema-border truncate min-w-0">
        {{ chatStore.activeChannel.topic }}
      </span>
      <span
        v-if="chatStore.activeChannel?.slow_mode_seconds"
        data-testid="slow-mode-chip"
        class="flex-shrink-0 rounded-md border border-mnema-border px-1.5 py-0.5 font-mono text-xs text-mnema-muted"
        v-tooltip="$t('moderation.slowModeHint')"
      >{{ $t('moderation.slowModeChip', { time: durationLabel(chatStore.activeChannel.slow_mode_seconds) }) }}</span>
    </div>

    <!-- Right: Badges & member list toggle -->
    <div class="flex items-center gap-2 flex-shrink-0">
      <button
        v-if="chatStore.activeChannel"
        @click="openNotificationMenu"
        class="w-8 h-8 flex items-center justify-center rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
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
        class="w-8 h-8 flex items-center justify-center rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        v-tooltip="$t('talk.closeChat')"
        :aria-label="$t('talk.closeChat')"
      >
        <X class="w-5 h-5" />
      </button>
      <button
        v-else
        @click="chatStore.showMemberList = !chatStore.showMemberList"
        :class="[
          'w-8 h-8 flex items-center justify-center rounded-md transition',
          chatStore.showMemberList
            ? 'text-mnema-accent bg-mnema-surface'
            : 'text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface'
        ]"
        v-tooltip="$t('members.toggle')"
        :aria-pressed="chatStore.showMemberList ? 'true' : 'false'"
      >
        <Users class="w-5 h-5" />
      </button>
    </div>
  </header>

  <!-- Desktop notification opt-in: shown until answered or dismissed -->
  <div
    v-if="showNotifHint"
    class="px-4 py-2 border-b border-mnema-hairline bg-mnema-raised flex items-center gap-3 text-sm flex-shrink-0"
    role="region"
    :aria-label="$t('notifications.hintTitle')"
  >
    <Bell class="w-4 h-4 text-mnema-accent flex-shrink-0" />
    <p class="min-w-0 flex-1 text-mnema-muted">
      <span class="font-medium text-mnema-text">{{ $t('notifications.hintTitle') }}</span>
      <span class="ml-2">{{ $t('notifications.hintBody') }}</span>
    </p>
    <button
      type="button"
      class="px-2.5 py-1 rounded-md bg-mnema-accent text-mnema-accent-ink font-semibold hover:bg-mnema-accent-hover transition flex-shrink-0"
      @click="enableNotifications"
    >
      {{ $t('notifications.enable') }}
    </button>
    <button
      type="button"
      class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition flex-shrink-0"
      :aria-label="$t('notifications.notNow')"
      v-tooltip="$t('notifications.notNow')"
      @click="dismissNotifHint"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
