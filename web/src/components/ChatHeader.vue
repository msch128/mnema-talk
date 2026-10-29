<script setup lang="ts">
import type { Component } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Hash, Users, Bell, BellOff, AtSign, X, Volume2, Search, MessageCircle, Info, Timer, Ellipsis, Inbox, Bookmark } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
import { useLaterStore } from '../stores/later'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useNotificationHint } from '../composables/useNotificationHint'
import { useFirstRunShown } from '../composables/useFirstRunShown'
import { durationLabel } from '../lib/moderation'
import MentionsInbox from './MentionsInbox.vue'
import SavedLaterPanel from './SavedLaterPanel.vue'
import PinnedMessages from './PinnedMessages.vue'
import TipBar from './TipBar.vue'
import FirstRunCard from './FirstRunCard.vue'
import NavDrawerButton from './NavDrawerButton.vue'
import DmInfoDialog from './DmInfoDialog.vue'
import DmTalkActions from './DmTalkActions.vue'
import DmHeaderTitle from './DmHeaderTitle.vue'
import DmMuteButton from './DmMuteButton.vue'
import { useDmChat } from '../composables/useDmChat'
import { openSearch } from '../lib/globalSearch'
import { quoteValue } from '../lib/searchQuery'

// The channel header (48px, aligned with the side columns; 40px in the Talk's
// chat) and the desktop notification opt-in under it, shown until answered or dismissed.
const props = defineProps({
  panel: { type: Boolean, default: false }
})
const emit = defineEmits<{ close: []; menu: [x: number, y: number, items: ContextMenuItem[]] }>()

// Below this header width the mentions and the saved & reminders buttons fold into one
// More menu, so the channel name keeps its room. The header's own width counts
// (a container query), not the window's: the side columns take their share.
const NARROW_BELOW = 640
const headerEl = ref<HTMLElement | null>(null)
const narrow = ref(false)
// The buttons mount only once the width is known, so a narrow header
// never mounts them in their wide form first (each one loads its data on mount).
const measured = ref(false)
let observer: ResizeObserver | null = null
onMounted(() => {
  const el = headerEl.value
  if (el && el.clientWidth > 0) narrow.value = el.clientWidth < NARROW_BELOW
  measured.value = true
  if (!el || typeof ResizeObserver === 'undefined') return
  observer = new ResizeObserver(entries => {
    const width = entries[0]?.contentRect.width ?? el.clientWidth
    if (width > 0) narrow.value = width < NARROW_BELOW
  })
  observer.observe(el)
})
onBeforeUnmount(() => observer?.disconnect())
type FoldedPanel = 'mentions' | 'saved'
const foldedOpen = ref<FoldedPanel | null>(null)

const chatStore = useChatStore()
// A conversation takes the channel's place: no topic, pins or notification menu, and its members instead of the community's.
const { dm, isDm } = useDmChat()
const showInfo = ref(false)
const serverFeatures = useServerFeaturesStore()
// A conversation shows its face, presence and mute button on servers that have them.
const richDm = computed(() => isDm.value && serverFeatures.dmV2 && !props.panel)
const later = useLaterStore()
onMounted(() => { void serverFeatures.load() })
// Auto-delete is shown wherever it applies, so nobody posts in a channel unaware that it forgets.
const autoDeleteDays = computed(() => {
  const days = chatStore.activeChannel?.auto_delete_days ?? 0
  return days > 0 && serverFeatures.channelAutoDelete && !isDm.value ? days : 0
})
const channelIcon = computed<Component>(() => {
  if (props.panel) return Volume2
  return isDm.value ? MessageCircle : Hash
})
const { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems } = useNotificationHint(() => props.panel)
const firstRunShown = useFirstRunShown()

function searchInChannel() {
  const channel = chatStore.activeChannel
  if (!channel) return
  if (isDm.value) void openSearch(`in:${quoteValue(channel.name)} `, 'dm')
  else void openSearch(`in:${quoteValue(channel.name)} `)
}

// The channel description: one truncated line next to the name, in full on click.
const topicOpen = ref(false)
const showTopic = computed(() => !props.panel && !isDm.value && !!chatStore.activeChannel?.topic)
watch(() => chatStore.activeChannel?.id, () => { topicOpen.value = false })

// The name keeps the room it needs; only beside a topic is it capped so the
// topic stays visible too.
const titleClass = computed(() => {
  if (!showTopic.value) return 'min-w-0'
  return 'shrink-0 max-w-[60%] max-md:shrink max-md:max-w-none'
})

const hasSaved = computed(() => serverFeatures.has('saved'))
const hasLater = computed(() => serverFeatures.has('reminders') || serverFeatures.has('messages.scheduled'))
const folded = computed(() => narrow.value && !props.panel)
const unreadMentions = computed(() => Object.values(chatStore.readStates).reduce((sum, s) => sum + (s.mention_count || 0), 0))
const moreNeedsAttention = computed(() => unreadMentions.value > 0 || (hasLater.value && later.attention > 0))
watch(folded, isFolded => { if (!isFolded) foldedOpen.value = null })

function openMoreMenu(e: MouseEvent) {
  if (!(e.currentTarget instanceof Element)) return
  const rect = e.currentTarget.getBoundingClientRect()
  const items: ContextMenuItem[] = [
    { id: 'mentions', label: t('mentions.inbox'), icon: Inbox, action: () => { foldedOpen.value = 'mentions' } }
  ]
  if (hasSaved.value || hasLater.value) items.push({ id: 'saved', label: t('panel.title'), icon: Bookmark, action: () => { foldedOpen.value = 'saved' } })
  emit('menu', Math.max(8, rect.right - 200), rect.bottom + 4, items)
}

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
    ref="headerEl"
    :class="[
      'px-4 max-md:pl-1 max-md:pr-2 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 shrink-0 z-10',
      panel ? 'h-10' : 'h-12'
    ]"
  >
    <!-- Left: Channel name & topic -->
    <div class="flex items-center gap-2 min-w-0 flex-1">
      <NavDrawerButton v-if="!panel" />
      <DmHeaderTitle v-if="richDm && dm" :dm="dm" @members="showInfo = true" />
      <component :is="channelIcon" v-if="!richDm" class="w-5 h-5 max-md:hidden text-mnema-tertiary shrink-0" />
      <div v-if="!richDm" class="flex min-w-0 items-center gap-2 max-md:flex-col max-md:items-start max-md:gap-0">
        <span
          data-testid="chat-channel-name"
          :class="['font-semibold text-base text-mnema-text truncate', titleClass]"
        >
          {{ chatStore.activeChannel?.name || $t('chat.selectChannel') }}
        </span>
        <button
          v-if="showTopic"
          type="button"
          data-testid="channel-topic"
          :aria-expanded="topicOpen ? 'true' : 'false'"
          class="min-w-0 max-md:max-w-full truncate text-left text-sm text-mnema-muted hover:text-mnema-text md:ml-1 md:border-l md:border-mnema-border md:pl-3 max-md:text-xs"
          @click="topicOpen = !topicOpen"
        >{{ chatStore.activeChannel?.topic }}</button>
      </div>
      <span
        v-if="chatStore.activeChannel?.slow_mode_seconds"
        data-testid="slow-mode-chip"
        class="shrink-0 rounded-md border border-mnema-border px-1.5 py-0.5 font-mono text-xs text-mnema-muted"
        v-tooltip="$t('moderation.slowModeHint')"
      >{{ $t('moderation.slowModeChip', { time: durationLabel(chatStore.activeChannel.slow_mode_seconds) }) }}</span>
      <span
        v-if="autoDeleteDays"
        data-testid="auto-delete-chip"
        class="inline-flex shrink-0 items-center gap-1 rounded-md border border-mnema-border px-1.5 py-0.5 font-mono text-xs text-mnema-muted"
        v-tooltip="$t('autoDelete.tooltip', { period: $t(`autoDelete.periods.${autoDeleteDays}`) })"
      ><Timer class="h-3 w-3" aria-hidden="true" />{{ $t(`autoDelete.periods.${autoDeleteDays}`) }}</span>
    </div>

    <!-- Right: Badges & member list toggle -->
    <div class="flex items-center gap-2 max-md:gap-0 shrink-0">
      <template v-if="!panel && !folded && measured">
        <MentionsInbox />
        <SavedLaterPanel />
      </template>
      <div v-else-if="!panel && measured" class="relative">
        <button
          type="button"
          data-testid="header-more"
          class="btn btn-icon relative w-9 h-9"
          v-tooltip="$t('chat.headerMore')"
          :aria-label="$t('chat.headerMore')"
          aria-haspopup="menu"
          @click="openMoreMenu"
        >
          <Ellipsis class="w-5 h-5" />
          <span
            v-if="moreNeedsAttention"
            data-testid="header-more-badge"
            class="absolute right-1 top-1 h-2 w-2 rounded-full bg-mnema-accent"
          />
        </button>
        <MentionsInbox bare :open="foldedOpen === 'mentions'" @update:open="foldedOpen = $event ? 'mentions' : null" />
        <SavedLaterPanel bare :open="foldedOpen === 'saved'" @update:open="foldedOpen = $event ? 'saved' : null" />
      </div>
      <PinnedMessages v-if="chatStore.activeChannel && !isDm" />
      <DmMuteButton v-if="richDm && dm" :dm="dm" />
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

  <!-- The full channel description, opened by a click on its truncated line in the header -->
  <div
    v-if="showTopic && topicOpen"
    data-testid="channel-topic-full"
    class="px-4 py-2 border-b border-mnema-hairline bg-mnema-raised text-sm text-mnema-muted whitespace-pre-line wrap-break-word shrink-0"
  >{{ chatStore.activeChannel?.topic }}</div>

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

  <!-- One feature tip at a time, never next to the notification hint or the Owner's checklist -->
  <TipBar v-if="!panel && !showNotifHint && !firstRunShown" />

  <!-- The Owner's "Get started" list; renders nothing for anyone else -->
  <FirstRunCard v-if="!panel" />
</template>
