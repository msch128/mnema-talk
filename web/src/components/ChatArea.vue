<script setup lang="ts">
import type { Message } from '../types/domain'
import { ref, computed, nextTick, watch, onMounted } from 'vue'
import { Hash, ArrowDown, ArrowUp, Loader2, MessageCircle, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import ChatHeader from './ChatHeader.vue'
import ChatComposer from './ChatComposer.vue'
import ImageLightbox from './ImageLightbox.vue'
import ContextMenu from './ContextMenu.vue'
import EphemeralNotes from './EphemeralNotes.vue'
import PinNotices from './PinNotices.vue'
import MessageRow from './MessageRow.vue'
import BlockedMessageRow from './BlockedMessageRow.vue'
import { useMessageActions } from '../composables/useMessageActions'
import { useDmChat } from '../composables/useDmChat'
import { toggleSavedMessage, useMessageMenu } from '../composables/useMessageMenu'
import { useMessageScroll } from '../composables/useMessageScroll'
import { useUnreadDivider } from '../composables/useUnreadDivider'
import { useFirstUnreadJump } from '../composables/useFirstUnreadJump'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { useDraftSlot } from '../composables/useDraftSlot'
import { channelDraftKey, type DraftReply } from '../lib/drafts'
import { useEscapeMarksRead } from '../composables/useComposerKeys'
import { continuationIds } from '../lib/messageGrouping'
import { typingLine } from '../lib/chatLogic'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { t } from '../i18n'

// panel: the voice channel's chat under its Talk (VoiceChatPanel): a compact
// header with a close button, Discord's welcome texts, no member toggle.
const props = defineProps({
  panel: { type: Boolean, default: false }
})
const emit = defineEmits<{ close: [] }>()

const chatStore = useChatStore()
const authStore = useAuthStore()
const serverFeatures = useServerFeaturesStore()
const toasts = useToastStore()
const permissions = usePermissionsStore()

const blocks = useBlocksStore()
// A conversation sits in the channel slot: its words, and the server's own answer to "may I write".
const dmChat = useDmChat()
const channelName = computed(() => chatStore.activeChannel?.name || '')
const channelIcon = computed(() => {
  if (props.panel) return Volume2
  return dmChat.isDm.value ? MessageCircle : Hash
})
const welcomeText = computed(() => (dmChat.isDm.value ? dmChat.welcome.value : t(props.panel ? 'talk.chatWelcome' : 'chat.welcome', { channel: channelName.value })))
const beginningText = computed(() => (dmChat.isDm.value ? dmChat.beginning.value : t(props.panel ? 'talk.chatBeginning' : 'chat.beginning', { channel: channelName.value })))
// What the member may do here; the server decides, this only hides what would fail.
const canIn = (key: 'send_messages' | 'attach_files' | 'add_reactions') => permissions.canIn(chatStore.activeChannel?.id ?? '', key)
const canSend = computed(() => (dmChat.isDm.value ? dmChat.canSend.value : canIn('send_messages')))
const canAttach = computed(() => canSend.value && (dmChat.isDm.value || canIn('attach_files')))
const canReact = computed(() => (dmChat.isDm.value ? dmChat.canSend.value : canIn('add_reactions')))
const placeholder = computed(() => {
  if (dmChat.isDm.value) return dmChat.placeholder.value
  return canSend.value ? t(props.panel ? 'talk.chatPlaceholder' : 'chat.placeholder', { channel: channelName.value }) : t('chat.readOnly')
})

const inputMessage = ref('')
const messageContainer = ref<HTMLElement | null>(null)
const composer = ref<InstanceType<typeof ChatComposer> | null>(null)
const textAreaEl = computed(() => composer.value?.textArea ?? null)
const isSending = ref(false)
const selectedImage = ref<string | null>(null)
// Message the composer is currently replying to.
const replyingTo = ref<DraftReply | null>(null)
// Text and reply target stay with their channel while another one is open.
useDraftSlot(() => (chatStore.activeChannel ? channelDraftKey(chatStore.activeChannel.id) : null), inputMessage, replyingTo)

// Editing, deleting, reactions and uploads
const actions = useMessageActions({ container: messageContainer })
const { editingId: editingMessageId, pickerId: activeReactionPickerMsgId, isUploading } = actions
const menu = useMessageMenu({ actions, reply: startReply })
const contextMenu = menu.state

const {
  showUnreadPill, highlightedId, messageRows, scrollToBottom, scrollToBottomNow,
  handleScroll, resetUnreadPill
} = useMessageScroll(messageContainer)
const { dividerSince, dividerBeforeId, dividerLabel, markAllRead } = useUnreadDivider()
const { showJumpToUnread, jumpToFirstUnread } = useFirstUnreadJump({ container: messageContainer, since: dividerSince, dividerBeforeId })

// ---- Typing indicator ----

const typingText = computed(() => {
  const line = typingLine(chatStore.typingByChannel[chatStore.activeChannel?.id ?? ''])
  return line ? t(line.key, line.params) : ''
})

watch(() => chatStore.activeChannel?.id, () => {
  resetUnreadPill()
  actions.cancelEdit()
  nextTick(() => composer.value?.focus())
})

async function handleJumpToPresent() {
  await chatStore.jumpToLatest()
}

// ---- Replies ----

function startReply(msg: Message) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => composer.value?.focus())
}

function cancelReply() {
  replyingTo.value = null
}

function jumpToReplied(msg: Message) {
  if (msg.reply_to?.deleted || !msg.reply_to_id) {
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
      composer.value?.focus()
    })
  }
// Immediate: a mention that opened the Talk's chat lands in its composer.
}, { immediate: true })

function onMessageKeydown(e: KeyboardEvent, msg: Message) {
  handleMessageKeydown(e, {
    rows: messageRows,
    menu: (el: HTMLElement) => menu.openAtElement(el, msg),
    reply: () => startReply(msg),
    ...(serverFeatures.has('saved') ? { save: () => { void toggleSavedMessage(msg) } } : {}),
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

useEscapeMarksRead({
  composer: textAreaEl,
  closeMenu: () => {
    if (!contextMenu.value.open) return false
    menu.close()
    return true
  },
  occupied: () => !!(selectedImage.value || editingMessageId.value || replyingTo.value),
  draft: () => inputMessage.value,
  markAllRead
})

onMounted(() => composer.value?.focus())

// After sending while viewing older history, jump to the present.
async function revealOwnMessage() {
  if (chatStore.hasMoreAfter) await chatStore.jumpToLatest()
  else scrollToBottom()
}

function isSendBlocked(text: string): boolean {
  if (!text || !canSend.value) return true
  return isUploading.value || isSending.value
}

async function handleSend() {
  const text = inputMessage.value.trim()
  if (isSendBlocked(text)) return

  isSending.value = true
  try {
    await chatStore.sendMessage(text, null, replyingTo.value?.id || null)
    inputMessage.value = ''
    replyingTo.value = null
    await revealOwnMessage()
  } catch (err) {
    toasts.error(dmChat.sendError(err, t('chat.sendFailed')))
  } finally {
    isSending.value = false
  }
}

async function handleFileUpload(e: Event) {
  const replyId = replyingTo.value?.id || null
  const ok = await actions.upload(e.target as HTMLInputElement, (file: File) => chatStore.uploadMedia(file, '', null, replyId))
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
  <component
    :is="panel ? 'section' : 'main'"
    :id="panel ? undefined : 'main-content'"
    :tabindex="panel ? undefined : -1"
    :aria-label="panel ? $t('talk.chatPanel', { channel: channelName }) : undefined"
    class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden focus:outline-hidden"
  >
    <ChatHeader :panel="panel" @close="emit('close')" @menu="(x, y, items) => menu.show(x, y, items)" />

    <!-- Message timeline (72px gutter, 40px avatars) -->
    <div class="relative flex-1 min-h-0 flex flex-col">
    <div ref="messageContainer" class="flex-1 overflow-y-auto overflow-x-hidden pt-2 pb-6" @scroll.passive="handleScroll">
      <!-- Initial / jump loading -->
      <div v-if="!chatStore.messages?.length && chatStore.isLoadingWindow" class="h-full flex items-center justify-center text-mnema-tertiary">
        <Loader2 class="w-6 h-6 animate-spin text-mnema-accent" />
      </div>

      <!-- Empty Talk chat: the channel's beginning, at the bottom like in Discord -->
      <div
        v-else-if="!chatStore.messages?.length && panel"
        data-testid="voice-chat-empty"
        class="h-full flex flex-col justify-end px-4 pb-2"
      >
        <div class="w-14 h-14 rounded-full bg-mnema-surface flex items-center justify-center mb-2 text-mnema-text">
          <Volume2 class="w-7 h-7" />
        </div>
        <p class="font-bold text-2xl text-mnema-text">{{ welcomeText }}</p>
        <p class="text-sm text-mnema-muted mt-1">{{ beginningText }}</p>
      </div>

      <!-- Empty State -->
      <div v-else-if="!chatStore.messages?.length" class="h-full flex flex-col items-center justify-center text-center p-8">
        <div class="w-12 h-12 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-3 text-mnema-accent bg-mnema-surface/50">
          <Hash class="w-5 h-5 opacity-80" />
        </div>
        <p class="font-semibold text-lg text-mnema-text">{{ welcomeText }}</p>
        <p class="text-sm text-mnema-tertiary mt-1 max-w-sm">
          {{ $t('chat.emptyBody') }}
        </p>
      </div>

      <!-- Top of the window: more history loading, or the channel's beginning -->
      <template v-else>
        <div v-if="chatStore.hasMoreBefore" class="h-12 flex items-center justify-center text-mnema-tertiary" aria-live="polite">
          <Loader2 v-if="chatStore.isLoadingBefore" class="w-5 h-5 animate-spin text-mnema-accent" />
        </div>
        <div v-else :class="['px-4 pb-2', panel ? 'pt-4' : 'pt-8']">
          <div :class="['rounded-full bg-mnema-surface flex items-center justify-center mb-2 text-mnema-text', panel ? 'w-14 h-14' : 'w-16 h-16']">
            <component :is="channelIcon" :class="panel ? 'w-7 h-7' : 'w-9 h-9'" />
          </div>
          <p :class="['font-bold text-mnema-text', panel ? 'text-2xl' : 'text-3xl']">{{ welcomeText }}</p>
          <p :class="['text-mnema-muted mt-1', panel ? 'text-sm' : 'text-base']">{{ beginningText }}</p>
        </div>
      </template>

      <!-- Messages List -->
      <template v-for="msg in (chatStore.messages || [])" :key="msg.id">
      <!-- "New since" divider -->
      <div
        v-if="dividerBeforeId === msg.id"
        role="separator"
        data-unread-divider
        class="flex items-center gap-3 px-4 mt-4 mb-1 select-none"
      >
        <span class="flex-1 h-px bg-mnema-accent/40"></span>
        <span class="text-xs font-semibold text-mnema-accent uppercase tracking-wide">{{ dividerLabel() }}</span>
        <span class="flex-1 h-px bg-mnema-accent/40"></span>
      </div>
      <BlockedMessageRow v-if="blocks.hides(msg)" :msg="msg" @show="blocks.reveal(msg.id)" />
      <MessageRow
        v-else
        :msg="msg"
        :grouped="groupedIds.has(msg.id)"
        :highlighted="highlightedId === msg.id"
        :mentions-me="msg.user_id !== authStore.user?.id && chatStore.messageMentionsMe(msg)"
        :is-own="actions.isOwn(msg)"
        :can-react="canReact"
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

    <!-- The "new since" divider is above the viewport -->
    <button
      v-if="showJumpToUnread"
      type="button"
      data-testid="jump-to-unread"
      class="absolute left-1/2 -translate-x-1/2 top-2 h-8 px-4 flex items-center gap-1.5 rounded-full bg-mnema-band text-mnema-mint text-sm font-semibold shadow-lg hover:brightness-110 transition z-20"
      @click="jumpToFirstUnread"
    >
      <ArrowUp class="w-4 h-4" />
      {{ $t('chat.jumpToFirstUnread') }}
    </button>

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
      <span class="flex items-center gap-1 shrink-0 font-semibold">
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

    <PinNotices v-if="chatStore.activeChannel" :channel-id="chatStore.activeChannel.id" />
    <EphemeralNotes v-if="chatStore.activeChannel" :channel-id="chatStore.activeChannel.id" />

    <ChatComposer
      ref="composer"
      v-model="inputMessage"
      :replying-to="replyingTo"
      :can-send="canSend"
      :can-attach="canAttach"
      :placeholder="placeholder"
      :uploading="isUploading"
      :sending="isSending"
      :typing-text="typingText"
      @send="handleSend"
      @cancel-reply="cancelReply"
      @file="handleFileUpload"
    />

    <!-- Image lightbox -->
    <ImageLightbox v-if="selectedImage" :src="selectedImage" @close="selectedImage = null" />

    <!-- Message context menu -->
    <ContextMenu
      v-model="contextMenu.open"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :items="contextMenu.items"
    />
  </component>
</template>
