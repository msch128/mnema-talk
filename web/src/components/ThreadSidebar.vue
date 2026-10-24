<script setup lang="ts">
import type { Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { X, MessageSquare, Loader2, Bell, BellOff, Lock, Unlock } from '@lucide/vue'
import type { ThreadRoot } from '../stores/chat'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { updateForumPost } from '../lib/forumApi'
import ThreadRootCard from './ThreadRootCard.vue'
import ImageLightbox from './ImageLightbox.vue'
import ThreadReply from './ThreadReply.vue'
import BlockedMessageRow from './BlockedMessageRow.vue'
import { useBlocksStore } from '../stores/blocks'
import { useDmChat } from '../composables/useDmChat'
import ThreadComposer from './ThreadComposer.vue'
import { useMessageActions } from '../composables/useMessageActions'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { useDraftSlot } from '../composables/useDraftSlot'
import { threadDraftKey, type DraftReply } from '../lib/drafts'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const chatStore = useChatStore()
const toasts = useToastStore()
const permissions = usePermissionsStore()
// The thread lives in a channel; the server decides, this hides what would fail.
const threadChannel = computed(() => chatStore.activeThread?.channel_id ?? chatStore.activeChannel?.id ?? '')
const blocks = useBlocksStore()
// In a direct message conversation the server's own answer decides, not channel permissions.
const dmChat = useDmChat()
const canManageMessages = computed(() => permissions.canIn(threadChannel.value, 'manage_messages'))
const isLockedForMe = computed(() => !!chatStore.activeThread?.is_locked && !canManageMessages.value)
const canSend = computed(() => !isLockedForMe.value && (dmChat.isDm.value ? dmChat.canSend.value : permissions.canIn(threadChannel.value, 'send_messages')))
const canAttach = computed(() => canSend.value && (dmChat.isDm.value || permissions.canIn(threadChannel.value, 'attach_files')))
const canReact = computed(() => (dmChat.isDm.value ? dmChat.canSend.value : permissions.canIn(threadChannel.value, 'add_reactions')))

async function toggleLock() {
  if (!chatStore.activeThread?.title || !canManageMessages.value) return
  const newLock = !chatStore.activeThread.is_locked
  try {
    await updateForumPost(threadChannel.value, chatStore.activeThread.id, { is_locked: newLock })
    chatStore.activeThread.is_locked = newLock
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('forum.lockFailed')))
  }
}

const followed = computed(() => !!chatStore.activeThread && chatStore.followedThreads.has(chatStore.activeThread.id))
const replyInput = ref('')
const repliesContainer = ref<HTMLElement | null>(null)
const isSending = ref(false)
const selectedImage = ref<string | null>(null)

// Editing, deleting, reactions and uploads
const actions = useMessageActions({ container: repliesContainer, deleteTitle: 'thread.deleteTitle' })
const { editingId: editingReplyId, pickerId: activeReactionPickerMsgId, isUploading } = actions

function scrollToBottom() {
  nextTick(() => {
    if (repliesContainer.value) {
      repliesContainer.value.scrollTop = repliesContainer.value.scrollHeight
    }
  })
}

watch(() => chatStore.threadReplies.length, () => {
  scrollToBottom()
})

// ---- Replies inside the thread ----

const replyingTo = ref<DraftReply | null>(null)
// Each thread keeps its own reply text and target.
useDraftSlot(() => (chatStore.activeThread ? threadDraftKey(chatStore.activeThread.id) : null), replyInput, replyingTo)
const composer = ref<InstanceType<typeof ThreadComposer> | null>(null)
const highlightedId = ref<string | null>(null)
let highlightTimer: ReturnType<typeof setTimeout> | undefined

watch(() => chatStore.activeThread?.id, () => {
  actions.cancelEdit()
})

function startReply(msg: ThreadRoot) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => composer.value?.focus())
}

function cancelReply() {
  replyingTo.value = null
}

function flash(id: string) {
  clearTimeout(highlightTimer)
  highlightedId.value = null
  requestAnimationFrame(() => {
    highlightedId.value = id
    highlightTimer = setTimeout(() => { highlightedId.value = null }, 2000)
  })
}

// Thread replies are fully loaded here, so jumps stay inside the panel.
function jumpToReplied(msg: Message) {
  const id = msg.reply_to_id
  const container = repliesContainer.value
  if (!id || !container || msg.reply_to?.deleted) {
    chatStore.showToast(t('chat.messageNotFound'))
    return
  }
  const row = container.querySelector(`[data-reply-id="${id}"]`)
  if (!row) {
    chatStore.showToast(t('chat.messageNotFound'))
    return
  }
  row.scrollIntoView({ block: 'center' })
  flash(id)
}

// Same keys as in the channel: arrows move between the root and the
// replies, r replies, e edits an own reply, Escape backs out.
function onRootKeydown(e: KeyboardEvent, msg: ThreadRoot) {
  handleMessageKeydown(e, {
    rows: () => repliesContainer.value?.querySelectorAll<HTMLElement>('[data-reply-id]') || [],
    reply: () => startReply(msg),
    escape: () => {
      if (editingReplyId.value) actions.cancelEdit()
      else if (replyingTo.value) cancelReply()
      else return false
      return true
    }
  })
}

function onReplyKeydown(e: KeyboardEvent, msg: Message, { editable = true } = {}) {
  handleMessageKeydown(e, {
    rows: () => repliesContainer.value?.querySelectorAll<HTMLElement>('[data-reply-id]') || [],
    reply: () => startReply(msg),
    edit: () => {
      if (!editable || !actions.isOwn(msg)) return false
      actions.startEdit(msg)
      return true
    },
    escape: () => {
      if (editingReplyId.value) actions.cancelEdit()
      else if (replyingTo.value) cancelReply()
      else return false
      return true
    }
  })
}

onMounted(() => {
  scrollToBottom()
})

onUnmounted(() => {
  clearTimeout(highlightTimer)
})

function isReplyBlocked(text: string): boolean {
  if (!text || !canSend.value) return true
  return isSending.value || isUploading.value
}

async function handleSendReply() {
  const text = replyInput.value.trim()
  if (isReplyBlocked(text)) return

  isSending.value = true
  try {
    await chatStore.sendThreadReply(text, replyingTo.value?.id || null)
    replyInput.value = ''
    replyingTo.value = null
    scrollToBottom()
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('thread.sendFailed')))
  } finally {
    isSending.value = false
  }
}

async function handleFileUpload(e: Event) {
  const replyId = replyingTo.value?.id || null
  const ok = await actions.upload(e.target as HTMLInputElement, (file: File) => chatStore.uploadThreadMedia(file, '', replyId))
  if (!ok) return
  replyingTo.value = null
  scrollToBottom()
}
</script>

<template>
  <aside class="w-full min-w-0 bg-mnema-canvas border-l border-mnema-hairline flex flex-col h-full select-none">
    <!-- Thread Header -->
    <header class="h-12 pl-4 pr-2 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-2 shrink-0">
      <div class="flex items-center gap-2 min-w-0">
        <MessageSquare class="w-5 h-5 text-mnema-accent shrink-0" />
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5 min-w-0">
            <span class="font-semibold text-base text-mnema-text shrink-0">{{ $t('thread.title') }}</span>
            <span class="text-sm text-mnema-tertiary truncate">
              #{{ chatStore.activeChannel?.name || '' }}
            </span>
          </div>
        </div>
      </div>

      <button
        v-if="chatStore.activeThread?.title && canManageMessages"
        type="button"
        data-testid="thread-lock-toggle"
        :aria-label="chatStore.activeThread.is_locked ? $t('forum.unlockPost') : $t('forum.lockPost')"
        class="w-8 h-8 flex items-center justify-center shrink-0 rounded-md hover:bg-mnema-surface transition"
        :class="chatStore.activeThread.is_locked ? 'text-amber-400' : 'text-mnema-tertiary hover:text-mnema-text'"
        v-tooltip="chatStore.activeThread.is_locked ? $t('forum.unlockPost') : $t('forum.lockPost')"
        @click="toggleLock"
      >
        <Lock v-if="chatStore.activeThread.is_locked" class="w-[18px] h-[18px]" />
        <Unlock v-else class="w-[18px] h-[18px]" />
      </button>

      <button
        v-if="chatStore.activeThread && chatStore.followKnown"
        type="button"
        data-testid="thread-follow"
        :aria-pressed="followed ? 'true' : 'false'"
        :aria-label="followed ? $t('thread.unfollow') : $t('thread.follow')"
        class="w-8 h-8 flex items-center justify-center shrink-0 rounded-md hover:bg-mnema-surface transition"
        :class="followed ? 'text-mnema-accent' : 'text-mnema-tertiary hover:text-mnema-text'"
        v-tooltip="followed ? $t('thread.following') : $t('thread.notFollowing')"
        @click="chatStore.activeThread && chatStore.setThreadFollow(chatStore.activeThread.id, !followed)"
      >
        <Bell v-if="followed" class="w-[18px] h-[18px]" />
        <BellOff v-else class="w-[18px] h-[18px]" />
      </button>

      <button
        @click="chatStore.closeThread"
        class="btn btn-icon w-8 h-8 shrink-0"
        v-tooltip="$t('thread.close')"
      >
        <X class="w-5 h-5" />
      </button>
    </header>

    <!-- Scrollable Thread Content Area -->
    <div ref="repliesContainer" class="flex-1 overflow-y-auto overflow-x-hidden p-4 space-y-3">
      <!-- Root Message Card -->
      <ThreadRootCard
        v-if="chatStore.activeThread"
        :root="chatStore.activeThread"
        :actions="actions"
        :can-react="canReact"
        :highlighted="highlightedId === chatStore.activeThread.id"
        @reply="startReply"
        @open-image="selectedImage = $event"
        @keydown="onRootKeydown"
      />

      <!-- Thread Replies Divider -->
      <div class="flex items-center gap-2 py-1">
        <div class="flex-1 h-px bg-mnema-hairline"></div>
        <span class="text-xs text-mnema-tertiary font-mono tracking-wider uppercase">
          {{ $t('chat.replies', { count: chatStore.threadReplies.length }) }}
        </span>
        <div class="flex-1 h-px bg-mnema-hairline"></div>
      </div>

      <!-- Loading State -->
      <div v-if="chatStore.isThreadLoading" class="py-8 flex flex-col items-center justify-center text-mnema-tertiary gap-2">
        <Loader2 class="w-5 h-5 animate-spin text-mnema-accent" />
        <span class="text-xs">{{ $t('thread.loading') }}</span>
      </div>

      <!-- Empty State -->
      <div
        v-else-if="!chatStore.threadReplies.length"
        class="py-8 text-center text-mnema-tertiary text-sm italic"
      >
        {{ $t('thread.empty') }}
      </div>

      <!-- Replies List -->
      <template v-for="reply in chatStore.threadReplies" :key="reply.id">
      <BlockedMessageRow v-if="blocks.hides(reply)" :msg="reply" compact @show="blocks.reveal(reply.id)" />
      <ThreadReply
        v-else
        :reply="reply"
        :actions="actions"
        :can-react="canReact"
        :highlighted="highlightedId === reply.id"
        @keydown="onReplyKeydown($event, reply)"
        @reply="startReply(reply)"
        @jump="jumpToReplied(reply)"
        @open-image="selectedImage = $event"
      />
      </template>
    </div>

    <div v-if="isLockedForMe" class="p-3 bg-amber-400/10 border-t border-mnema-hairline flex items-center gap-2 text-xs text-amber-400 shrink-0">
      <Lock class="w-4 h-4 shrink-0" />
      <span>{{ $t('forum.lockedNotice') }}</span>
    </div>
    <ThreadComposer
      v-else
      ref="composer"
      v-model="replyInput"
      :replying-to="replyingTo"
      :can-send="canSend"
      :can-attach="canAttach"
      :uploading="isUploading"
      :sending="isSending"
      @send="handleSendReply"
      @cancel-reply="cancelReply"
      @file="handleFileUpload"
    />

    <!-- Image lightbox -->
    <ImageLightbox v-if="selectedImage" :src="selectedImage" @close="selectedImage = null" />
  </aside>
</template>
