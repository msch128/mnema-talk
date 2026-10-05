<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import {
  Hash, Plus, ArrowUp, ArrowDown, FileText, Users,
  MessageSquare, Pencil, Trash2, Smile, SmilePlus, Check, Loader2, Reply,
  MoreHorizontal, Link as LinkIcon, Copy, Bookmark, Bell, BellOff, AtSign, X
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import ReplyPreview from './ReplyPreview.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import ImageLightbox from './ImageLightbox.vue'
import ContextMenu from './ContextMenu.vue'
import { continuationIds } from '../lib/messageGrouping'
import { previewText } from '../lib/replies'
import { firstUnreadId, typingLine } from '../lib/chatLogic'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t, locale } from '../i18n'

const chatStore = useChatStore()
const authStore = useAuthStore()
const toasts = useToastStore()

const inputMessage = ref('')
const messageContainer = ref(null)
const fileInput = ref(null)
const textAreaEl = ref(null)
const isUploading = ref(false)
const isSending = ref(false)
const selectedImage = ref(null)
// Message the composer is currently replying to.
const replyingTo = ref(null)

// Message Editing & Reactions
const editingMessageId = ref(null)
const editMessageText = ref('')
const isSavingEdit = ref(false)
const activeReactionPickerMsgId = ref(null)
const quickEmojis = ['👍', '❤️', '😂', '🔥', '🎉', '🚀']

function startEditMessage(msg) {
  editingMessageId.value = msg.id
  editMessageText.value = msg.content
  activeReactionPickerMsgId.value = null
}

function cancelEditMessage() {
  editingMessageId.value = null
  editMessageText.value = ''
}

async function saveEditMessage(msg) {
  if (!editMessageText.value.trim() || isSavingEdit.value) return
  isSavingEdit.value = true
  try {
    await chatStore.editMessage(chatStore.activeChannel.id, msg.id, editMessageText.value.trim())
    editingMessageId.value = null
    editMessageText.value = ''
  } catch (err) {
    toasts.error(err.message || t('chat.editFailed'))
  } finally {
    isSavingEdit.value = false
  }
}

async function handleDeleteMessage(msg) {
  const ok = await confirm({
    title: t('chat.deleteTitle'),
    body: t('chat.deleteBody'),
    excerpt: previewText(msg.content).slice(0, 160) || (msg.attachments?.length ? t('chat.attachment') : ''),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await chatStore.deleteMessage(chatStore.activeChannel.id, msg.id)
  } catch (err) {
    toasts.error(err.message || t('chat.deleteFailed'))
  }
}

async function handleToggleReaction(msgId, emoji) {
  try {
    activeReactionPickerMsgId.value = null
    await chatStore.toggleReaction(msgId, emoji)
  } catch (err) {
    console.warn('Reaction error:', err)
  }
}

function hasUserReacted(reaction) {
  if (!authStore.user?.id || !reaction?.users) return false
  return reaction.users.includes(authStore.user.id)
}

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

function messageRows() {
  return messageContainer.value ? messageContainer.value.querySelectorAll('[data-msg-id]') : []
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
  contextMenu.value = {
    open: true,
    x: Math.max(8, rect.right - 200),
    y: rect.bottom + 4,
    items: levels.map(({ level, icon }) => ({
      label: t(`notifications.${level}`),
      icon,
      shortcut: current === level ? '✓' : '',
      action: () => chatStore.setNotificationLevel(channel.id, level)
    }))
  }
}

watch(() => chatStore.activeChannel?.id, () => {
  showUnreadPill.value = false
  replyingTo.value = null
  cancelEditMessage()
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

function toggleReactionPicker(pickerId) {
  activeReactionPickerMsgId.value = activeReactionPickerMsgId.value === pickerId ? null : pickerId
}

function handleGlobalClick(e) {
  if (activeReactionPickerMsgId.value && !e.target.closest('.reaction-picker-anchor')) {
    activeReactionPickerMsgId.value = null
  }
}

// Context Menu
const contextMenu = ref({
  open: false,
  x: 0,
  y: 0,
  items: []
})

function openMessageContextMenu(e, msg) {
  if (e?.preventDefault) e.preventDefault()
  activeReactionPickerMsgId.value = null

  const items = [
    {
      label: t('chat.reply'),
      icon: Reply,
      shortcut: 'r',
      action: () => startReply(msg)
    },
    {
      label: t('chat.addReaction'),
      icon: SmilePlus,
      action: () => {
        // After the menu's own click has finished bubbling: the window click
        // handler would otherwise close the picker straight away.
        setTimeout(() => { activeReactionPickerMsgId.value = msg.id }, 0)
      }
    },
    {
      label: t('chat.openThread'),
      icon: MessageSquare,
      action: () => chatStore.openThread(msg)
    }
  ]

  if (msg.user_id === authStore.user?.id) {
    items.push({
      label: t('chat.edit'),
      icon: Pencil,
      shortcut: 'e',
      action: () => startEditMessage(msg)
    })
  }

  items.push({ type: 'separator' })

  items.push({
    label: t('chat.copyText'),
    icon: Copy,
    action: async () => {
      try {
        await navigator.clipboard.writeText(msg.content || '')
        toasts.success(t('chat.copiedText'))
      } catch (err) {
        console.warn('Copy failed:', err)
      }
    }
  })

  items.push({
    label: t('chat.copyLink'),
    icon: LinkIcon,
    action: async () => {
      try {
        const link = `${window.location.origin}/c/${chatStore.activeChannel?.id}/m/${msg.id}`
        await navigator.clipboard.writeText(link)
        toasts.success(t('chat.copiedLink'))
      } catch (err) {
        console.warn('Copy failed:', err)
      }
    }
  })

  items.push({
    label: t('chat.markUnread'),
    icon: Bookmark,
    action: async () => {
      try {
        await chatStore.markChannelUnread(chatStore.activeChannel?.id, msg.id)
        toasts.success(t('chat.markedUnread'))
      } catch (err) {
        toasts.error(err.message || t('chat.markUnreadFailed'))
      }
    }
  })

  if (msg.user_id === authStore.user?.id || authStore.isAdmin) {
    items.push({ type: 'separator' })
    items.push({
      label: t('chat.delete'),
      icon: Trash2,
      danger: true,
      action: () => handleDeleteMessage(msg)
    })
  }

  contextMenu.value = {
    open: true,
    x: e.clientX || 0,
    y: e.clientY || 0,
    items
  }
}

function openContextMenuFromButton(e, msg) {
  const rect = e.currentTarget.getBoundingClientRect()
  openMessageContextMenu({
    preventDefault: () => {},
    clientX: rect.left,
    clientY: rect.bottom + 4
  }, msg)
}

// Right-click opens at the pointer; the keyboard (no pointer position) opens
// at the message's own position.
function onMessageContextMenu(e, msg) {
  if (!e.clientX && !e.clientY && e.currentTarget) {
    openMenuAtElement(e.currentTarget, msg)
    return
  }
  openMessageContextMenu(e, msg)
}

function openMenuAtElement(el, msg) {
  const rect = el.getBoundingClientRect()
  openMessageContextMenu({
    preventDefault: () => {},
    clientX: rect.left + 80,
    clientY: Math.min(Math.max(rect.top, 0) + 24, window.innerHeight - 24)
  }, msg)
}

// Moves focus to the previous/next message row.
function focusSiblingMessage(row, dir) {
  const rows = [...messageRows()]
  const next = rows[rows.indexOf(row) + dir]
  if (!next) return false
  next.focus()
  return true
}

function handleMessageKeydown(e, msg) {
  if (['INPUT', 'TEXTAREA'].includes(e.target.tagName)) return

  if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
    e.preventDefault()
    openMenuAtElement(e.currentTarget, msg)
    return
  }
  if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && e.target === e.currentTarget && !e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
    e.preventDefault()
    focusSiblingMessage(e.currentTarget, e.key === 'ArrowUp' ? -1 : 1)
    return
  }
  // Ctrl/Cmd/Alt combos belong to the browser or OS (reload, find, ...).
  if (e.ctrlKey || e.metaKey || e.altKey) return

  if (e.key === 'r' || e.key === 'R') {
    e.preventDefault()
    startReply(msg)
  } else if (e.key === 'e' || e.key === 'E') {
    if (msg.user_id === authStore.user?.id) {
      e.preventDefault()
      startEditMessage(msg)
    }
  } else if (e.key === 'Escape') {
    if (contextMenu.value.open) {
      e.preventDefault()
      contextMenu.value.open = false
    } else if (activeReactionPickerMsgId.value) {
      e.preventDefault()
      activeReactionPickerMsgId.value = null
    } else if (editingMessageId.value) {
      e.preventDefault()
      cancelEditMessage()
    } else if (replyingTo.value) {
      e.preventDefault()
      cancelReply()
    }
  }
}

function onGlobalKeydown(e) {
  if (e.key !== 'Escape') return
  if (contextMenu.value.open) { contextMenu.value.open = false; return }
  if (activeReactionPickerMsgId.value) { activeReactionPickerMsgId.value = null; return }
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
  window.addEventListener('click', handleGlobalClick)
  window.addEventListener('keydown', onGlobalKeydown)
  if (typeof ResizeObserver !== 'undefined' && messageContainer.value) {
    resizeObserver = new ResizeObserver(handleContainerResize)
    resizeObserver.observe(messageContainer.value)
  }
})

onUnmounted(() => {
  window.removeEventListener('click', handleGlobalClick)
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
  const file = e.target.files?.[0]
  if (!file) return

  isUploading.value = true
  try {
    await chatStore.uploadMedia(file, '', null, replyingTo.value?.id || null)
    if (fileInput.value) fileInput.value.value = ''
    replyingTo.value = null
    await revealOwnMessage()
  } catch (err) {
    toasts.error(err.message || t('chat.uploadFailed'))
  } finally {
    isUploading.value = false
  }
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleTimeString([locale.value], { hour: '2-digit', minute: '2-digit' })
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
      <div
        :data-msg-id="msg.id"
        tabindex="0"
        role="article"
        @contextmenu.prevent="onMessageContextMenu($event, msg)"
        @keydown="handleMessageKeydown($event, msg)"
        :class="[
          'relative pl-[72px] pr-12 py-0.5 hover:bg-mnema-surface/50 transition-colors group focus:outline-none focus-visible:bg-mnema-surface/40',
          groupedIds.has(msg.id) ? '' : 'mt-[17px] first:mt-2',
          highlightedId === msg.id ? 'msg-flash' : ''
        ]"
      >
        <!-- Hover Quick Actions Bar -->
        <div
          :class="[
            'absolute right-4 -top-4 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-1 shadow-lg z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
            activeReactionPickerMsgId === msg.id ? 'flex' : 'hidden group-hover:flex group-focus-within:flex'
          ]"
        >
          <!-- Emoji Reactions Trigger -->
          <div class="relative reaction-picker-anchor">
            <button
              @click.stop="toggleReactionPicker(msg.id)"
              :class="[
                'p-1.5 rounded transition',
                activeReactionPickerMsgId === msg.id 
                  ? 'bg-mnema-surface text-amber-400' 
                  : 'hover:bg-mnema-surface text-mnema-tertiary hover:text-amber-400'
              ]"
              v-tooltip="$t('chat.addReaction')"
            >
              <Smile class="w-4 h-4" />
            </button>

            <!-- Quick Emoji Palette Popup -->
            <div 
              v-if="activeReactionPickerMsgId === msg.id"
              class="absolute right-0 bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1.5 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-['']"
            >
              <button
                v-for="emoji in quickEmojis"
                :key="emoji"
                @click.stop="handleToggleReaction(msg.id, emoji)"
                class="hover:scale-125 transition p-1 text-base rounded hover:bg-mnema-surface active:scale-95"
              >
                {{ emoji }}
              </button>
            </div>
          </div>

          <!-- Reply -->
          <button
            @click.stop="startReply(msg)"
            class="p-1.5 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
            v-tooltip="$t('chat.reply')"
          >
            <Reply class="w-4 h-4" />
          </button>

          <!-- Edit Message (if author) -->
          <button
            v-if="msg.user_id === authStore.user?.id"
            @click.stop="startEditMessage(msg)"
            class="p-1.5 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition"
            v-tooltip="$t('chat.edit')"
          >
            <Pencil class="w-4 h-4" />
          </button>

          <!-- More Actions (⋯) Context Menu Trigger -->
          <button
            @click.stop="openContextMenuFromButton($event, msg)"
            class="p-1.5 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
            v-tooltip="$t('chat.moreActions')"
          >
            <MoreHorizontal class="w-4 h-4" />
          </button>
        </div>

        <!-- Grouped follow-up: small time in the gutter on hover -->
        <span
          v-if="groupedIds.has(msg.id)"
          class="absolute left-0 top-0.5 w-[72px] text-center text-xs leading-[1.375rem] text-mnema-tertiary tabular-nums opacity-0 group-hover:opacity-100 select-none"
          aria-hidden="true"
        >
          {{ formatTime(msg.created_at) }}
        </span>

        <!-- User Avatar (first message of a group only) -->
        <UserAvatar
          v-else
          :user="msg"
          size="md"
          :class="['!absolute left-4 cursor-pointer hover:opacity-85 transition', msg.reply_to ? 'top-6' : 'top-1']"
          @click="chatStore.openUserProfile(msg)"
        />

        <!-- Content Body -->
        <div class="min-w-0">
          <!-- "Replied to" reference line -->
          <ReplyPreview v-if="msg.reply_to" :reply="msg.reply_to" @jump="jumpToReplied(msg)" />
          <div v-if="!groupedIds.has(msg.id)" class="flex items-baseline gap-2 min-w-0">
            <span
              @click="chatStore.openUserProfile(msg)"
              class="font-semibold text-message text-mnema-text hover:text-mnema-accent hover:underline transition-colors cursor-pointer truncate"
            >
              {{ msg.display_name || msg.username }}
            </span>
            <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(msg.created_at) }}</span>
            <span v-if="msg.is_edited" class="text-xs text-mnema-tertiary italic flex-shrink-0">{{ $t('chat.edited') }}</span>
          </div>

          <!-- Inline Message Editor -->
          <div v-if="editingMessageId === msg.id" class="mt-1 space-y-1.5">
            <textarea
              v-model="editMessageText"
              rows="2"
              @keydown.enter.exact.prevent="saveEditMessage(msg)"
              @keydown.esc.prevent="cancelEditMessage"
              class="w-full text-message p-2 rounded-lg bg-mnema-surface border border-mnema-accent text-mnema-text focus:outline-none resize-none"
            ></textarea>
            <div class="flex items-center justify-between text-xs text-mnema-tertiary">
              <span class="font-mono">{{ $t('chat.editHint') }}</span>
              <div class="flex items-center gap-1.5">
                <button @click="cancelEditMessage" class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text">{{ $t('common.cancel') }}</button>
                <button @click="saveEditMessage(msg)" :disabled="isSavingEdit" class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50">
                  <Loader2 v-if="isSavingEdit" class="w-3.5 h-3.5 animate-spin" />
                  <Check v-else class="w-3.5 h-3.5" />
                  <span>{{ $t('common.save') }}</span>
                </button>
              </div>
            </div>
          </div>

          <!-- Markdown Message Content -->
          <MarkdownContent v-else-if="msg.content" :content="msg.content" />

          <!-- Grouped messages have no header line, so the edit marker follows the body -->
          <span
            v-if="groupedIds.has(msg.id) && msg.is_edited && editingMessageId !== msg.id"
            class="block text-xs text-mnema-tertiary italic"
          >{{ $t('chat.edited') }}</span>

          <!-- Media Attachments (Images, Clips, Documents) -->
          <div v-if="msg.attachments && msg.attachments.length" class="mt-2 space-y-2">
            <div 
              v-for="att in msg.attachments" 
              :key="att.id" 
              class="max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-sm"
            >
              <template v-if="att.mime_type.startsWith('image/')">
                <img 
                  :src="att.url" 
                  :alt="att.original_filename" 
                  @click="selectedImage = att.url"
                  class="max-h-80 w-auto max-w-full rounded-t object-cover cursor-pointer hover:opacity-95 transition"
                  loading="lazy"
                />
              </template>
              <template v-else-if="att.mime_type.startsWith('video/')">
                <video :src="att.url" controls class="max-h-80 w-full rounded-t"></video>
              </template>
              <div class="p-2.5 flex items-center justify-between text-sm bg-mnema-raised border-t border-mnema-hairline">
                <div class="flex items-center gap-2 truncate">
                  <FileText class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
                  <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate text-sm">
                    {{ att.original_filename }}
                  </a>
                </div>
                <span class="text-xs font-mono text-mnema-tertiary pl-2 flex-shrink-0">
                  {{ $t('media.sizeMb', { size: (att.size_bytes / 1024 / 1024).toFixed(2) }) }}
                </span>
              </div>
            </div>
          </div>

          <!-- Thread counter -->
          <div v-if="msg.reply_count > 0" class="mt-2">
            <button
              @click.stop="chatStore.openThread(msg)"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-mnema-accent-subtle/80 hover:bg-mnema-accent-subtle text-mnema-accent border border-mnema-accent/30 transition shadow-xs"
            >
              <MessageSquare class="w-4 h-4 text-mnema-accent" />
              <span>{{ $t('chat.replies', { count: msg.reply_count }) }}</span>
              <span class="text-xs opacity-75 font-mono ml-0.5">{{ $t('chat.openThread') }} &rarr;</span>
            </button>
          </div>

          <!-- Reaction badges -->
          <div v-if="msg.reactions && msg.reactions.length" class="flex flex-wrap gap-1 mt-2 items-center">
            <button 
              v-for="r in msg.reactions" 
              :key="r.emoji"
              @click.stop="handleToggleReaction(msg.id, r.emoji)"
              :class="[
                'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-sm border transition cursor-pointer active:scale-95',
                hasUserReacted(r)
                  ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
                  : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
              ]"
              v-tooltip="$t('chat.reaction', { emoji: r.emoji })"
            >
              <span>{{ r.emoji }}</span>
              <span class="text-xs font-mono">{{ r.count }}</span>
            </button>

            <!-- Add-reaction button inline with reactions -->
            <div class="relative reaction-picker-anchor inline-block">
              <button
                @click.stop="toggleReactionPicker(`bottom-${msg.id}`)"
                class="inline-flex items-center justify-center w-7 h-7 rounded-full border border-dashed border-mnema-border hover:border-mnema-accent text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition cursor-pointer text-sm"
                v-tooltip="$t('chat.addReaction')"
              >
                <SmilePlus class="w-4 h-4" />
              </button>

              <!-- Quick Emoji Palette Popup from bottom -->
              <div 
                v-if="activeReactionPickerMsgId === `bottom-${msg.id}`"
                class="absolute left-0 bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1.5 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-['']"
              >
                <button
                  v-for="emoji in quickEmojis"
                  :key="emoji"
                  @click.stop="handleToggleReaction(msg.id, emoji)"
                  class="hover:scale-125 transition p-1 text-base rounded hover:bg-mnema-surface active:scale-95"
                >
                  {{ emoji }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
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
          'min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
          replyingTo ? 'rounded-b-lg' : 'rounded-lg'
        ]"
      >
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
          @input="handleComposerInput"
          :placeholder="$t('chat.placeholder', { channel: chatStore.activeChannel?.name || '' })"
          :aria-label="$t('chat.placeholder', { channel: chatStore.activeChannel?.name || '' })"
          rows="1"
          class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

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
