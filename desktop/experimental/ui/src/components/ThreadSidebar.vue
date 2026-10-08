<script setup lang="ts">
import type { Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref, nextTick, watch, onMounted, onUnmounted } from 'vue'
import {
  X, MessageSquare, ArrowUp, Plus, Loader2,
  Pencil, Trash2, Smile, Reply
} from '@lucide/vue'
import type { ThreadRoot } from '../stores/chat'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import ReplyPreview from './ReplyPreview.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import ImageLightbox from './ImageLightbox.vue'
import ReactionPalette from './ReactionPalette.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import MessageAttachments from './MessageAttachments.vue'
import MessageEditor from './MessageEditor.vue'
import ReactionBar from './ReactionBar.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useMessageActions, formatTime, PICKER_ANCHOR } from '../composables/useMessageActions'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const chatStore = useChatStore()
const authStore = useAuthStore()
const toasts = useToastStore()

const replyInput = ref('')
const repliesContainer = ref<HTMLElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
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

const replyingTo = ref<ThreadRoot | null>(null)
const replyTextArea = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(replyTextArea, replyInput)
const highlightedId = ref<string | null>(null)
let highlightTimer: ReturnType<typeof setTimeout> | undefined

watch(() => chatStore.activeThread?.id, () => {
  replyingTo.value = null
  actions.cancelEdit()
})

function startReply(msg: ThreadRoot) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => replyTextArea.value?.focus())
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

async function handleSendReply() {
  const text = replyInput.value.trim()
  if (!text || isSending.value || isUploading.value) return

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

function handleKeyDown(e: KeyboardEvent) {
  if (assist.onKeydown(e)) return
  if (e.key === 'Escape' && replyingTo.value) {
    e.preventDefault()
    cancelReply()
    return
  }
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    handleSendReply()
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
  return d.toLocaleDateString([locale.value], { day: '2-digit', month: '2-digit' }) + ' ' + d.toLocaleTimeString([locale.value], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <aside class="w-full min-w-0 bg-mnema-canvas border-l border-mnema-hairline flex flex-col h-full select-none">
    <!-- Thread Header -->
    <header class="h-12 pl-4 pr-2 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-2 flex-shrink-0">
      <div class="flex items-center gap-2 min-w-0">
        <MessageSquare class="w-5 h-5 text-mnema-accent flex-shrink-0" />
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5 min-w-0">
            <span class="font-semibold text-base text-mnema-text flex-shrink-0">{{ $t('thread.title') }}</span>
            <span class="text-sm text-mnema-tertiary truncate">
              #{{ chatStore.activeChannel?.name || '' }}
            </span>
          </div>
        </div>
      </div>

      <button
        @click="chatStore.closeThread"
        class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
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
          'relative group bg-mnema-elevated border border-mnema-border/80 rounded-xl p-3.5 shadow-sm space-y-2 focus:outline-none focus-visible:border-mnema-accent/60',
          highlightedId === chatStore.activeThread.id ? 'msg-flash' : ''
        ]"
      >
        <!-- Reply to the thread's root message -->
        <button
          type="button"
          @click.stop="startReply(chatStore.activeThread)"
          class="absolute right-2 top-2 hidden group-hover:flex group-focus-within:flex p-1 rounded bg-mnema-elevated border border-mnema-border text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
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
            <span class="text-xs text-mnema-tertiary font-mono">
              {{ formatDate(chatStore.activeThread.created_at) }}
            </span>
          </div>
        </div>

        <MarkdownContent v-if="chatStore.activeThread.content" :content="chatStore.activeThread.content" />

        <!-- Root Message Attachments -->
        <MessageAttachments
          :attachments="chatStore.activeThread.attachments ?? []"
          variant="root"
          @open-image="selectedImage = $event"
        />
        <!-- Root message reactions -->
        <ReactionBar
          :reactions="chatStore.activeThread.reactions ?? []"
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
      <div
        v-for="reply in chatStore.threadReplies"
        :key="reply.id"
        :data-reply-id="reply.id"
        tabindex="0"
        role="article"
        @keydown="onReplyKeydown($event, reply)"
        :class="[
          'relative hover:bg-mnema-surface/50 -mx-2 px-2 py-1.5 rounded-md transition-colors group focus:outline-none focus-visible:bg-mnema-surface/40',
          highlightedId === reply.id ? 'msg-flash' : '',
          reply.user_id !== authStore.user?.id && chatStore.messageMentionsMe(reply) ? 'msg-mentions-me' : ''
        ]"
      >
        <!-- Hover Quick Actions Bar -->
        <div
          :class="[
            'absolute right-2 -top-2.5 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-0.5 shadow-md z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
            activeReactionPickerMsgId === reply.id ? 'flex' : 'hidden group-hover:flex group-focus-within:flex'
          ]"
        >
          <div :class="['relative', PICKER_ANCHOR]">
            <button
              type="button"
              @click.stop="actions.togglePicker(reply.id)"
              :class="[
                'p-1 rounded transition',
                activeReactionPickerMsgId === reply.id
                  ? 'bg-mnema-surface text-amber-400'
                  : 'hover:bg-mnema-surface text-mnema-tertiary hover:text-amber-400'
              ]"
              v-tooltip="$t('chat.addReaction')"
            >
              <Smile class="w-3.5 h-3.5" />
            </button>
            <ReactionPalette
              v-if="activeReactionPickerMsgId === reply.id"
              align="right"
              @pick="actions.toggleReaction(reply.id, $event)"
              @close="actions.closePicker()"
            />
          </div>

          <button
            type="button"
            @click.stop="startReply(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
            v-tooltip="$t('chat.reply')"
          >
            <Reply class="w-3.5 h-3.5" />
          </button>

          <button
            v-if="actions.isOwn(reply)"
            type="button"
            @click.stop="actions.startEdit(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition"
            v-tooltip="$t('thread.edit')"
          >
            <Pencil class="w-3.5 h-3.5" />
          </button>

          <button
            v-if="actions.canDelete(reply)"
            type="button"
            @click.stop="actions.deleteMessage(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-red-400 transition"
            v-tooltip="$t('thread.delete')"
          >
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>

        <!-- "Replied to" reference line; connector ends at the 32px avatar -->
        <div v-if="reply.reply_to" class="pl-11">
          <ReplyPreview :reply="reply.reply_to" spine-class="left-[-29px] w-[25px]" @jump="jumpToReplied(reply)" />
        </div>

        <div class="flex items-start gap-3">
        <UserAvatar
          :user="reply"
          size="sm"
          class="cursor-pointer hover:opacity-85 transition mt-0.5"
          @click="chatStore.openUserProfile(reply)"
        />

        <div class="flex-1 min-w-0">
          <div class="flex items-baseline gap-2 min-w-0">
            <button
              type="button"
              data-testid="author-name"
              @click="chatStore.openUserProfile(reply)"
              class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate text-left focus-visible:text-mnema-accent"
            >
              {{ reply.display_name || reply.username }}
            </button>
            <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(reply.created_at) }}</span>
            <span v-if="reply.is_edited" class="text-xs text-mnema-tertiary italic">{{ $t('chat.edited') }}</span>
          </div>

          <!-- Inline Reply Editor -->
          <MessageEditor
            v-if="editingReplyId === reply.id"
            v-model="actions.editText.value"
            compact
            :saving="actions.isSavingEdit.value"
            :label="$t('thread.edit')"
            @save="actions.saveEdit(reply)"
            @cancel="actions.cancelEdit()"
          />

          <MarkdownContent v-else-if="reply.content" :content="reply.content" class="mt-0.5" />

          <!-- Reply Attachments -->
          <MessageAttachments :attachments="reply.attachments" variant="reply" @open-image="selectedImage = $event" />

          <!-- Reaction badges -->
          <ReactionBar
            :reactions="reply.reactions"
            compact
            small-icon
            :picker-open="activeReactionPickerMsgId === `bottom-${reply.id}`"
            @toggle="actions.toggleReaction(reply.id, $event)"
            @toggle-picker="actions.togglePicker(`bottom-${reply.id}`)"
            @close-picker="actions.closePicker()"
          />
        </div>
        </div>
      </div>
    </div>

    <!-- Thread Composer Input Bar -->
    <div class="px-4 pb-6 pt-1 flex-shrink-0">
      <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="cancelReply" />
      <div
        :class="[
          'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
          replyingTo ? 'rounded-b-lg' : 'rounded-lg'
        ]"
      >
        <MentionSuggestions
          v-if="assist.open.value"
          id="thread-mentions"
          :items="assist.suggestions.value"
          :active="assist.active.value"
          @pick="assist.pick"
          @hover="assist.active.value = $event"
        />

        <!-- Hidden file input for thread -->
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          @change="handleFileUpload"
          accept="image/*,video/*"
        />

        <button
          @click="fileInput?.click()"
          :disabled="isUploading || isSending"
          class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-full text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-50"
          v-tooltip="$t('thread.upload')"
        >
          <Plus class="w-5 h-5" />
        </button>

        <textarea
          ref="replyTextArea"
          v-model="replyInput"
          @keydown="handleKeyDown"
          @input="assist.onInput"
          @click="assist.onInput"
          @keyup.left="assist.onInput"
          @keyup.right="assist.onInput"
          @blur="assist.close"
          aria-autocomplete="list"
          :aria-expanded="assist.open.value ? 'true' : 'false'"
          :aria-controls="assist.open.value ? 'thread-mentions' : undefined"
          :aria-activedescendant="assist.open.value ? `thread-mentions-${assist.active.value}` : undefined"
          :placeholder="$t('thread.placeholder')"
          :aria-label="$t('thread.placeholder')"
          rows="1"
          class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <EmojiButton :disabled="isSending" @pick="assist.insertText" />

        <button
          @click="handleSendReply"
          :disabled="!replyInput.trim() || isUploading || isSending"
          class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          v-tooltip="$t('chat.send')"
        >
          <ArrowUp class="w-4 h-4" />
        </button>
      </div>
    </div>

    <!-- Image lightbox -->
    <ImageLightbox v-if="selectedImage" :src="selectedImage" @close="selectedImage = null" />
  </aside>
</template>
