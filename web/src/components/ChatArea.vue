<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import {
  Hash, Plus, ArrowUp, ArrowDown, Users, Loader2, Bell, BellOff, AtSign, X
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import ReplyComposerBar from './ReplyComposerBar.vue'
import ImageLightbox from './ImageLightbox.vue'
import ContextMenu from './ContextMenu.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import MessageRow from './MessageRow.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useMessageActions } from '../composables/useMessageActions'
import { useMessageMenu } from '../composables/useMessageMenu'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { continuationIds } from '../lib/messageGrouping'
import { firstUnreadId, typingLine } from '../lib/chatLogic'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const chatStore = useChatStore()
const authStore = useAuthStore()
const toasts = useToastStore()

const inputMessage = ref('')
const messageContainer = ref(null)
const fileInput = ref(null)
const textAreaEl = ref(null)
const assist = useComposerAssist(textAreaEl, inputMessage)
const isSending = ref(false)
const selectedImage = ref(null)
// Message the composer is currently replying to.
const replyingTo = ref(null)

// Editing, deleting, reactions and uploads
const actions = useMessageActions({ container: messageContainer })
const { editingId: editingMessageId, pickerId: activeReactionPickerMsgId, isUploading } = actions
const menu = useMessageMenu({ actions, reply: startReply })
const contextMenu = menu.state

// ---- Scrolling, infinite history & jump-to-message ----

// Start loading the next page when this close (px) to either end.
const LOAD_THRESHOLD_PX = 600
// "At the bottom" for auto-stick on new messages.
const STICK_THRESHOLD_PX = 80

const showUnreadPill = ref(false)
const highlightedId = ref(null)
let highlightTimer = null
let scrollFrame = 0

function distanceFromBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight
}

function isAtBottom() {
  const el = messageContainer.value
  return !el || distanceFromBottom(el) <= STICK_THRESHOLD_PX
}

function scrollToBottomNow() {
  const el = messageContainer.value
  if (el) el.scrollTop = el.scrollHeight
  stickToBottom = true
  showUnreadPill.value = false
}

function scrollToBottom() {
  nextTick(scrollToBottomNow)
}

function findRow(id) {
  for (const row of messageRows()) if (row.dataset.msgId === id) return row
  return null
}

// Remember where the first visible messages sit so the view can be put back
// exactly after rows are added/removed above or below (prepend, trims).
function captureAnchor() {
  const el = messageContainer.value
  if (!el) return []
  const top = el.getBoundingClientRect().top
  const anchors = []
  for (const row of messageRows()) {
    const rect = row.getBoundingClientRect()
    if (rect.bottom <= top) continue
    anchors.push({ id: row.dataset.msgId, offset: rect.top - top })
    if (anchors.length >= 5) break
  }
  return anchors
}

function restoreAnchor(anchors) {
  const el = messageContainer.value
  if (!el) return
  const top = el.getBoundingClientRect().top
  for (const a of anchors) {
    const row = findRow(a.id)
    if (!row) continue
    const delta = row.getBoundingClientRect().top - top - a.offset
    if (delta) el.scrollTop += delta
    return
  }
}

function flashMessage(id) {
  clearTimeout(highlightTimer)
  highlightedId.value = null
  // Re-render without the class first so the animation restarts on repeat jumps.
  requestAnimationFrame(() => {
    highlightedId.value = id
    highlightTimer = setTimeout(() => { highlightedId.value = null }, 2000)
  })
}

function scrollToMessage(id) {
  const row = findRow(id)
  if (!row) return
  row.scrollIntoView({ block: 'center' })
  flashMessage(id)
}

function maybeLoadMore() {
  const el = messageContainer.value
  if (!el || !chatStore.messages.length || chatStore.isLoadingWindow) return
  if (el.scrollTop < LOAD_THRESHOLD_PX && chatStore.hasMoreBefore && !chatStore.isLoadingBefore) {
    chatStore.loadOlder()
  }
  if (distanceFromBottom(el) < LOAD_THRESHOLD_PX && chatStore.hasMoreAfter && !chatStore.isLoadingAfter) {
    chatStore.loadNewer()
  }
}

// Whether the user is parked at the bottom. Kept across layout changes
// (window resize, panel drag, member list / thread toggle, reply bar) so a
// shrinking viewport doesn't leave the newest message behind the composer.
let stickToBottom = true
let lastSize = { w: 0, h: 0 }
let resizeObserver = null

function sizeChangedSinceObserved(el) {
  return el.clientWidth !== lastSize.w || el.clientHeight !== lastSize.h
}

function handleContainerResize() {
  const el = messageContainer.value
  if (!el) return
  const changed = sizeChangedSinceObserved(el)
  lastSize = { w: el.clientWidth, h: el.clientHeight }
  if (changed && stickToBottom) el.scrollTop = el.scrollHeight
}

function handleScroll() {
  if (scrollFrame) return
  scrollFrame = requestAnimationFrame(() => {
    scrollFrame = 0
    const el = messageContainer.value
    // Scroll events caused by a pending resize (scrollTop clamping) must not
    // clear the stick flag before the ResizeObserver re-pins the view.
    if (el && !sizeChangedSinceObserved(el)) stickToBottom = isAtBottom()
    if (isAtBottom() && !chatStore.hasMoreAfter) showUnreadPill.value = false
    maybeLoadMore()
  })
}

// One pre-render hook for every window change: decide what caused it, then
// after the DOM updated scroll to the bottom, to a jump target, or put the
// previously visible messages back where they were.
let seenLatest = chatStore.latestLoadSeq
let seenLive = chatStore.liveAppendSeq
let seenJump = chatStore.jumpTarget?.seq ?? 0

watch(
  () => [chatStore.messages, chatStore.latestLoadSeq, chatStore.liveAppendSeq, chatStore.jumpTarget],
  () => {
    const anchors = captureAnchor()
    const wasAtBottom = isAtBottom()
    const latest = chatStore.latestLoadSeq !== seenLatest
    const live = chatStore.liveAppendSeq !== seenLive
    const jump = chatStore.jumpTarget && chatStore.jumpTarget.seq !== seenJump ? chatStore.jumpTarget : null
    seenLatest = chatStore.latestLoadSeq
    seenLive = chatStore.liveAppendSeq
    seenJump = chatStore.jumpTarget?.seq ?? 0

    nextTick(() => {
      const last = chatStore.messages[chatStore.messages.length - 1]
      const mine = live && last?.user_id && last.user_id === authStore.user?.id
      if (jump) {
        scrollToMessage(jump.id)
      } else if (latest) {
        scrollToBottomNow()
      } else if (live && (wasAtBottom || mine)) {
        scrollToBottomNow()
      } else {
        restoreAnchor(anchors)
        if (live) showUnreadPill.value = true
      }
      maybeLoadMore()
    })
  },
  { flush: 'pre' }
)

// ---- "New since" divider ----

// Last-read time captured when the channel was opened: the divider stays put
// while the channel is open, even though the server marks it read meanwhile.
const dividerSince = ref(null)
watch(
  () => [chatStore.activeChannel?.id, chatStore.activeChannelLastReadAt],
  () => { dividerSince.value = chatStore.activeChannelLastReadAt },
  { immediate: true }
)
const dividerBeforeId = computed(() =>
  firstUnreadId(chatStore.messages, dividerSince.value, authStore.user?.id, chatStore.hasMoreBefore)
)

function dividerLabel() {
  const d = new Date(dividerSince.value)
  const sameDay = d.toDateString() === new Date().toDateString()
  const time = sameDay
    ? d.toLocaleTimeString([locale.value], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([locale.value], { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
  return t('chat.newSinceDivider', { time })
}

// Esc in the channel: mark everything read and drop the divider.
function markAllRead() {
  const id = chatStore.activeChannel?.id
  if (!id || chatStore.activeChannel.type === 'voice') return false
  const st = chatStore.readStates[id]
  if (!dividerBeforeId.value && !(st?.unread_count > 0) && !(st?.mention_count > 0)) return false
  dividerSince.value = null
  chatStore.markChannelRead(id)
  return true
}

// ---- Typing indicator ----

const typingText = computed(() => {
  const line = typingLine(chatStore.typingByChannel[chatStore.activeChannel?.id])
  return line ? t(line.key, line.params) : ''
})

function handleComposerInput() {
  if (inputMessage.value.trim()) chatStore.sendTyping(chatStore.activeChannel?.id)
}

// ---- Desktop notifications ----

const NOTIF_HINT_KEY = 'mnema_notif_hint_dismissed'
function readHintDismissed() {
  try { return localStorage.getItem(NOTIF_HINT_KEY) === '1' } catch { return false }
}
const notifHintDismissed = ref(readHintDismissed())
const showNotifHint = computed(() =>
  chatStore.notificationPermission === 'default' && !notifHintDismissed.value
)

function dismissNotifHint() {
  notifHintDismissed.value = true
  try { localStorage.setItem(NOTIF_HINT_KEY, '1') } catch { /* private mode: dismissed for this session only */ }
}

// Asked on a click, never on page load.
async function enableNotifications() {
  const result = await chatStore.requestNotificationPermission()
  if (result === 'granted') toasts.success(t('notifications.granted'))
  else if (result === 'denied') toasts.error(t('notifications.denied'))
  if (result !== 'default') dismissNotifHint()
}

function openNotificationMenu(e) {
  const channel = chatStore.activeChannel
  if (!channel) return
  const rect = e.currentTarget.getBoundingClientRect()
  const current = chatStore.notificationLevel(channel.id)
  const levels = [
    { level: 'all', icon: Bell },
    { level: 'mentions', icon: AtSign },
    { level: 'mute', icon: BellOff }
  ]
  menu.show(Math.max(8, rect.right - 200), rect.bottom + 4, levels.map(({ level, icon }) => ({
    label: t(`notifications.${level}`),
    icon,
    shortcut: current === level ? '✓' : '',
    action: () => chatStore.setNotificationLevel(channel.id, level)
  })))
}

watch(() => chatStore.activeChannel?.id, () => {
  showUnreadPill.value = false
  replyingTo.value = null
  actions.cancelEdit()
  nextTick(() => textAreaEl.value?.focus())
})

async function handleJumpToPresent() {
  await chatStore.jumpToLatest()
}

// ---- Replies ----

function startReply(msg) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => textAreaEl.value?.focus())
}

function cancelReply() {
  replyingTo.value = null
}

function jumpToReplied(msg) {
  if (msg.reply_to?.deleted) {
    chatStore.showToast(t('chat.messageNotFound'))
    return
  }
  chatStore.jumpToMessage(msg.reply_to_id)
}

watch(() => chatStore.pendingMention, (newVal) => {
  if (newVal) {
    inputMessage.value = `${inputMessage.value ? inputMessage.value.trim() + ' ' : ''}@${newVal} `
    chatStore.pendingMention = ''
    nextTick(() => {
      textAreaEl.value?.focus()
    })
  }
})

function messageRows() {
  return messageContainer.value ? messageContainer.value.querySelectorAll('[data-msg-id]') : []
}

function onMessageKeydown(e, msg) {
  handleMessageKeydown(e, {
    rows: messageRows,
    menu: el => menu.openAtElement(el, msg),
    reply: () => startReply(msg),
    edit: () => {
      if (!actions.isOwn(msg)) return false
      actions.startEdit(msg)
      return true
    },
    escape: () => {
      if (contextMenu.value.open) menu.close()
      else if (editingMessageId.value) actions.cancelEdit()
      else if (replyingTo.value) cancelReply()
      else return false
      return true
    }
  })
}

function onGlobalKeydown(e) {
  if (e.key !== 'Escape') return
  if (contextMenu.value.open) { menu.close(); return }
  // Something else (dialog, lightbox, composer reply, editor) already used the key.
  if (e.defaultPrevented || selectedImage.value || editingMessageId.value || replyingTo.value) return
  if (inputMessage.value.trim() || document.querySelector('[role="dialog"]')) return
  const tag = e.target?.tagName
  if ((tag === 'INPUT' || tag === 'TEXTAREA') && e.target !== textAreaEl.value) return
  if (markAllRead()) e.preventDefault()
}

onMounted(() => {
  scrollToBottom()
  textAreaEl.value?.focus()
  window.addEventListener('keydown', onGlobalKeydown)
  if (typeof ResizeObserver !== 'undefined' && messageContainer.value) {
    resizeObserver = new ResizeObserver(handleContainerResize)
    resizeObserver.observe(messageContainer.value)
  }
})

onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  resizeObserver?.disconnect()
  clearTimeout(highlightTimer)
  if (scrollFrame) cancelAnimationFrame(scrollFrame)
})

// After sending while viewing older history, jump to the present.
async function revealOwnMessage() {
  if (chatStore.hasMoreAfter) await chatStore.jumpToLatest()
  else scrollToBottom()
}

async function handleSend() {
  const text = inputMessage.value.trim()
  if (!text || isUploading.value || isSending.value) return

  isSending.value = true
  try {
    await chatStore.sendMessage(text, null, replyingTo.value?.id || null)
    inputMessage.value = ''
    replyingTo.value = null
    await revealOwnMessage()
  } catch (err) {
    toasts.error(err.message || t('chat.sendFailed'))
  } finally {
    isSending.value = false
  }
}

function handleKeyDown(e) {
  if (assist.onKeydown(e)) return
  if (e.key === 'Escape' && replyingTo.value) {
    e.preventDefault()
    cancelReply()
    return
  }
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    handleSend()
  }
}

async function handleFileUpload(e) {
  const replyId = replyingTo.value?.id || null
  const ok = await actions.upload(e.target, file => chatStore.uploadMedia(file, '', null, replyId))
  if (!ok) return
  replyingTo.value = null
  await revealOwnMessage()
}

// Consecutive messages by the same author (< 7 min apart) render compactly.
// The "new since" divider always starts a fresh group.
const groupedIds = computed(() => {
  const ids = continuationIds(chatStore.messages)
  if (dividerBeforeId.value) ids.delete(dividerBeforeId.value)
  return ids
})
</script>

<template>
  <main class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden">
    <!-- Channel Header (48px, aligned with the side columns) -->
    <header class="h-12 px-4 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 flex-shrink-0 z-10">
      <!-- Left: Channel name & topic -->
      <div class="flex items-center gap-2 min-w-0">
        <Hash class="w-5 h-5 text-mnema-tertiary flex-shrink-0" />
        <span class="font-semibold text-base text-mnema-text truncate flex-shrink-0 max-w-[60%]">
          {{ chatStore.activeChannel?.name || $t('chat.selectChannel') }}
        </span>
        <span v-if="chatStore.activeChannel?.topic" class="text-sm text-mnema-muted pl-3 ml-1 border-l border-mnema-border truncate min-w-0">
          {{ chatStore.activeChannel.topic }}
        </span>
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
        <button
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

    <!-- Message timeline (72px gutter, 40px avatars) -->
    <div class="relative flex-1 min-h-0 flex flex-col">
    <div ref="messageContainer" class="flex-1 overflow-y-auto overflow-x-hidden pt-2 pb-6" @scroll.passive="handleScroll">
      <!-- Initial / jump loading -->
      <div v-if="!chatStore.messages?.length && chatStore.isLoadingWindow" class="h-full flex items-center justify-center text-mnema-tertiary">
        <Loader2 class="w-6 h-6 animate-spin text-mnema-accent" />
      </div>

      <!-- Empty State -->
      <div v-else-if="!chatStore.messages?.length" class="h-full flex flex-col items-center justify-center text-center p-8">
        <div class="w-12 h-12 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-3 text-mnema-accent bg-mnema-surface/50">
          <Hash class="w-5 h-5 opacity-80" />
        </div>
        <p class="font-semibold text-lg text-mnema-text">{{ $t('chat.welcome', { channel: chatStore.activeChannel?.name || '' }) }}</p>
        <p class="text-sm text-mnema-tertiary mt-1 max-w-sm">
          {{ $t('chat.emptyBody') }}
        </p>
      </div>

      <!-- Top of the window: more history loading, or the channel's beginning -->
      <template v-else>
        <div v-if="chatStore.hasMoreBefore" class="h-12 flex items-center justify-center text-mnema-tertiary" aria-live="polite">
          <Loader2 v-if="chatStore.isLoadingBefore" class="w-5 h-5 animate-spin text-mnema-accent" />
        </div>
        <div v-else class="px-4 pt-8 pb-2">
          <div class="w-16 h-16 rounded-full bg-mnema-surface flex items-center justify-center mb-2 text-mnema-text">
            <Hash class="w-9 h-9" />
          </div>
          <p class="font-bold text-3xl text-mnema-text">{{ $t('chat.welcome', { channel: chatStore.activeChannel?.name || '' }) }}</p>
          <p class="text-base text-mnema-muted mt-1">{{ $t('chat.beginning', { channel: chatStore.activeChannel?.name || '' }) }}</p>
        </div>
      </template>

      <!-- Messages List -->
      <template v-for="msg in (chatStore.messages || [])" :key="msg.id">
      <!-- "New since" divider -->
      <div
        v-if="dividerBeforeId === msg.id"
        role="separator"
        class="flex items-center gap-3 px-4 mt-4 mb-1 select-none"
      >
        <span class="flex-1 h-px bg-mnema-accent/40"></span>
        <span class="text-xs font-semibold text-mnema-accent uppercase tracking-wide">{{ dividerLabel() }}</span>
        <span class="flex-1 h-px bg-mnema-accent/40"></span>
      </div>
      <MessageRow
        :msg="msg"
        :grouped="groupedIds.has(msg.id)"
        :highlighted="highlightedId === msg.id"
        :mentions-me="msg.user_id !== authStore.user?.id && chatStore.messageMentionsMe(msg)"
        :is-own="actions.isOwn(msg)"
        :editing="editingMessageId === msg.id"
        v-model:edit-text="actions.editText.value"
        :saving="actions.isSavingEdit.value"
        :picker-id="activeReactionPickerMsgId"
        @contextmenu.prevent="menu.onContextMenu($event, msg)"
        @keydown="onMessageKeydown($event, msg)"
        @reply="startReply(msg)"
        @edit="actions.startEdit(msg)"
        @save="actions.saveEdit(msg)"
        @cancel-edit="actions.cancelEdit()"
        @more="menu.openFromButton($event, msg)"
        @react="actions.toggleReaction(msg.id, $event)"
        @toggle-picker="actions.togglePicker($event)"
        @close-picker="actions.closePicker()"
        @open-thread="chatStore.openThread(msg)"
        @open-profile="chatStore.openUserProfile(msg)"
        @open-image="selectedImage = $event"
        @jump="jumpToReplied(msg)"
      />
      </template>
    </div>

    <!-- Viewing older history: the window doesn't reach the newest messages -->
    <button
      v-if="chatStore.hasMoreAfter && chatStore.messages.length"
      type="button"
      class="absolute left-4 right-4 bottom-1 h-8 px-3 flex items-center justify-between gap-2 rounded-lg bg-mnema-band text-mnema-mint text-sm font-medium shadow-lg hover:brightness-110 transition z-20"
      @click="handleJumpToPresent"
    >
      <span class="truncate">
        {{ chatStore.missedLiveCount > 0 ? $t('chat.newMessages') : $t('chat.viewingOlder') }}
      </span>
      <span class="flex items-center gap-1 flex-shrink-0 font-semibold">
        {{ $t('chat.jumpToEnd') }}
        <ArrowDown class="w-4 h-4" />
      </span>
    </button>

    <!-- New messages arrived while scrolled up -->
    <button
      v-else-if="showUnreadPill"
      type="button"
      class="absolute left-1/2 -translate-x-1/2 bottom-2 h-8 px-4 flex items-center gap-1.5 rounded-full bg-mnema-accent text-mnema-accent-ink text-sm font-semibold shadow-lg hover:bg-mnema-accent-hover transition z-20"
      @click="scrollToBottomNow"
    >
      {{ $t('chat.newMessages') }}
      <ArrowDown class="w-4 h-4" />
    </button>

    </div>

    <!-- Composer -->
    <div class="px-4 pb-1 flex-shrink-0">
      <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="cancelReply" />
      <div
        :class="[
          'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
          replyingTo ? 'rounded-b-lg' : 'rounded-lg'
        ]"
      >
        <MentionSuggestions
          v-if="assist.open.value"
          id="chat-mentions"
          :items="assist.suggestions.value"
          :active="assist.active.value"
          @pick="assist.pick"
          @hover="assist.active.value = $event"
        />

        <!-- Hidden File Input -->
        <input 
          ref="fileInput" 
          type="file" 
          class="hidden" 
          @change="handleFileUpload" 
          accept="image/*,video/*"
        />

        <!-- Attachment Button -->
        <button
          @click="fileInput?.click()"
          :disabled="isUploading || isSending"
          class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
          v-tooltip="$t('chat.upload')"
        >
          <Plus class="w-5 h-5" />
        </button>

        <!-- Text Input -->
        <textarea
          ref="textAreaEl"
          v-model="inputMessage"
          @keydown="handleKeyDown"
          @input="handleComposerInput(); assist.onInput()"
          @click="assist.onInput"
          @keyup.left="assist.onInput"
          @keyup.right="assist.onInput"
          @blur="assist.close"
          aria-autocomplete="list"
          :aria-expanded="assist.open.value ? 'true' : 'false'"
          :aria-controls="assist.open.value ? 'chat-mentions' : undefined"
          :aria-activedescendant="assist.open.value ? `chat-mentions-${assist.active.value}` : undefined"
          :placeholder="$t('chat.placeholder', { channel: chatStore.activeChannel?.name || '' })"
          :aria-label="$t('chat.placeholder', { channel: chatStore.activeChannel?.name || '' })"
          rows="1"
          class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <EmojiButton :disabled="isSending" @pick="assist.insertText" />

        <!-- Send Button -->
        <button
          @click="handleSend"
          :disabled="!inputMessage.trim() || isUploading || isSending"
          class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          v-tooltip="$t('chat.send')"
        >
          <ArrowUp class="w-4 h-4" />
        </button>
      </div>

      <!-- Who is typing (keeps its height so the layout doesn't jump) -->
      <div class="h-5 px-1 pt-0.5 text-xs text-mnema-tertiary truncate" role="status" aria-live="polite">
        {{ typingText }}
      </div>
    </div>

    <!-- Image lightbox -->
    <ImageLightbox v-if="selectedImage" :src="selectedImage" @close="selectedImage = null" />

    <!-- Message context menu -->
    <ContextMenu
      v-model="contextMenu.open"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :items="contextMenu.items"
    />
  </main>
</template>
