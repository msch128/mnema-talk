<script setup lang="ts">
import { clockTimeOptions } from '../lib/time'
import type { Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { X, MessageSquare, Loader2, Pin, Reply, Bell, BellOff } from '@lucide/vue'
import type { ThreadRoot } from '../stores/chat'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import ImageLightbox from './ImageLightbox.vue'
import MessageAttachments from './MessageAttachments.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import PollCard from './PollCard.vue'
import BotBadge from './BotBadge.vue'
import ReactionBar from './ReactionBar.vue'
import ThreadReply from './ThreadReply.vue'
import ThreadComposer from './ThreadComposer.vue'
import { useMessageActions } from '../composables/useMessageActions'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { useDraftSlot } from '../composables/useDraftSlot'
import { threadDraftKey, type DraftReply } from '../lib/drafts'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const chatStore = useChatStore()
const toasts = useToastStore()
const permissions = usePermissionsStore()
// The thread lives in a channel; the server decides, this hides what would fail.
const threadChannel = computed(() => chatStore.activeThread?.channel_id ?? chatStore.activeChannel?.id ?? '')
const canSend = computed(() => permissions.canIn(threadChannel.value, 'send_messages'))
const canAttach = computed(() => canSend.value && permissions.canIn(threadChannel.value, 'attach_files'))
const canReact = computed(() => permissions.canIn(threadChannel.value, 'add_reactions'))

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

function formatDate(dateStr?: string) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleDateString([locale.value], { day: '2-digit', month: '2-digit' }) + ' ' + d.toLocaleTimeString([locale.value], clockTimeOptions(locale.value))
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
        v-if="chatStore.activeThread && chatStore.followKnown"
        type="button"
        data-testid="thread-follow"
        :aria-pressed="followed ? 'true' : 'false'"
        :aria-label="followed ? $t('thread.unfollow') : $t('thread.follow')"
        class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md hover:bg-mnema-surface transition"
        :class="followed ? 'text-mnema-accent' : 'text-mnema-tertiary hover:text-mnema-text'"
        v-tooltip="followed ? $t('thread.following') : $t('thread.notFollowing')"
        @click="chatStore.activeThread && chatStore.setThreadFollow(chatStore.activeThread.id, !followed)"
      >
        <Bell v-if="followed" class="w-[18px] h-[18px]" />
        <BellOff v-else class="w-[18px] h-[18px]" />
      </button>

      <button
        @click="chatStore.closeThread"
        class="w-8 h-8 flex items-center justify-center shrink-0 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        v-tooltip="$t('thread.close')"
      >
        <X class="w-5 h-5" />
      </button>
    </header>

    <!-- Scrollable Thread Content Area -->
    <div ref="repliesContainer" class="flex-1 overflow-y-auto overflow-x-hidden p-4 space-y-3">
      <!-- Root Message Card -->
      <div
        v-if="chatStore.activeThread"
        :data-reply-id="chatStore.activeThread.id"
        tabindex="0"
        role="article"
        @keydown="onRootKeydown($event, chatStore.activeThread)"
        :class="[
          'relative group bg-mnema-elevated border border-mnema-border/80 rounded-xl p-3.5 shadow-xs space-y-2 focus:outline-hidden focus-visible:border-mnema-accent/60',
          highlightedId === chatStore.activeThread.id ? 'msg-flash' : ''
        ]"
      >
        <!-- Reply to the thread's root message -->
        <button
          type="button"
          @click.stop="startReply(chatStore.activeThread)"
          class="absolute right-2 top-2 hidden group-hover:flex group-focus-within:flex p-1 rounded-sm bg-mnema-elevated border border-mnema-border text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
          v-tooltip="$t('chat.reply')"
        >
          <Reply class="w-3.5 h-3.5" />
        </button>

        <div class="flex items-center gap-2.5">
          <UserAvatar
            :user="chatStore.activeThread"
            size="md"
            class="cursor-pointer hover:opacity-85 transition"
            @click="chatStore.openUserProfile(chatStore.activeThread)"
          />
          <div class="min-w-0">
            <button
              type="button"
              data-testid="author-name"
              @click="chatStore.openUserProfile(chatStore.activeThread)"
              class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate block max-w-full text-left focus-visible:text-mnema-accent"
            >
              {{ chatStore.activeThread.display_name || chatStore.activeThread.username }}
            </button>
            <BotBadge v-if="chatStore.activeThread.is_bot" :webhook="chatStore.activeThread.is_webhook === true" />
            <span class="text-xs text-mnema-tertiary font-mono">
              {{ formatDate(chatStore.activeThread.created_at) }}
            </span>
          </div>
        </div>

        <MarkdownContent v-if="chatStore.activeThread.content" :content="chatStore.activeThread.content" :mention-roles="chatStore.activeThread.mention_roles ?? []" />
        <MessageEmbeds :embeds="chatStore.activeThread.embeds ?? []" />
        <!-- A poll that started the thread keeps its options, votes and time left here -->
        <PollCard v-if="chatStore.activeThread.poll" :msg="chatStore.activeThread as Message" />
        <span v-if="chatStore.activeThread.is_edited" class="block text-xs text-mnema-tertiary italic">{{ $t('chat.edited') }}</span>
        <span v-if="chatStore.activeThread.is_pinned" data-testid="thread-root-pinned" class="flex items-center gap-1 text-xs text-mnema-accent">
          <Pin class="w-3.5 h-3.5" />{{ $t('moderation.pinned') }}
        </span>

        <!-- Root Message Attachments -->
        <MessageAttachments
          :attachments="chatStore.activeThread.attachments ?? []"
          variant="root"
          @open-image="selectedImage = $event"
        />
        <!-- Root message reactions -->
        <ReactionBar
          :reactions="chatStore.activeThread.reactions ?? []"
          :can-add="canReact"
          small-icon
          :picker-open="activeReactionPickerMsgId === `bottom-${chatStore.activeThread.id}`"
          @toggle="actions.toggleReaction(chatStore.activeThread.id, $event)"
          @toggle-picker="actions.togglePicker(`bottom-${chatStore.activeThread.id}`)"
          @close-picker="actions.closePicker()"
        />
      </div>

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
      <ThreadReply
        v-for="reply in chatStore.threadReplies"
        :key="reply.id"
        :reply="reply"
        :actions="actions"
        :can-react="canReact"
        :highlighted="highlightedId === reply.id"
        @keydown="onReplyKeydown($event, reply)"
        @reply="startReply(reply)"
        @jump="jumpToReplied(reply)"
        @open-image="selectedImage = $event"
      />
    </div>

    <ThreadComposer
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
