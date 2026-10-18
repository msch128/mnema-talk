<script setup lang="ts">
import type { Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref, computed, nextTick, watch, onMounted } from 'vue'
import {
  Hash, Plus, ArrowUp, ArrowDown, Users, Loader2, Bell, BellOff, AtSign, X, Volume2
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import ReplyComposerBar from './ReplyComposerBar.vue'
import ImageLightbox from './ImageLightbox.vue'
import ContextMenu from './ContextMenu.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import SlashSuggestions from './SlashSuggestions.vue'
import SlashCommandBar from './SlashCommandBar.vue'
import EphemeralNotes from './EphemeralNotes.vue'
import MessageRow from './MessageRow.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useSlashCommands } from '../composables/useSlashCommands'
import { useMessageActions } from '../composables/useMessageActions'
import { useMessageMenu } from '../composables/useMessageMenu'
import { useMessageScroll } from '../composables/useMessageScroll'
import { useUnreadDivider } from '../composables/useUnreadDivider'
import { useNotificationHint } from '../composables/useNotificationHint'
import { handleMessageKeydown } from '../composables/useMessageKeyboard'
import { handleComposerKeydown, useEscapeMarksRead } from '../composables/useComposerKeys'
import { continuationIds } from '../lib/messageGrouping'
import { durationLabel } from '../lib/moderation'
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
const toasts = useToastStore()
const permissions = usePermissionsStore()

const channelName = computed(() => chatStore.activeChannel?.name || '')
const channelIcon = computed(() => (props.panel ? Volume2 : Hash))
const welcomeText = computed(() => t(props.panel ? 'talk.chatWelcome' : 'chat.welcome', { channel: channelName.value }))
const beginningText = computed(() => t(props.panel ? 'talk.chatBeginning' : 'chat.beginning', { channel: channelName.value }))
// What the member may do here; the server decides, this only hides what would fail.
const canSend = computed(() => permissions.canIn(chatStore.activeChannel?.id ?? '', 'send_messages'))
const canAttach = computed(() => canSend.value && permissions.canIn(chatStore.activeChannel?.id ?? '', 'attach_files'))
const canReact = computed(() => permissions.canIn(chatStore.activeChannel?.id ?? '', 'add_reactions'))
const placeholder = computed(() => (canSend.value
  ? t(props.panel ? 'talk.chatPlaceholder' : 'chat.placeholder', { channel: channelName.value })
  : t('chat.readOnly')))

const inputMessage = ref('')
const messageContainer = ref<HTMLElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const textAreaEl = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textAreaEl, inputMessage)
const slash = useSlashCommands(inputMessage, () => chatStore.activeChannel?.id)
const isSending = ref(false)
const selectedImage = ref<string | null>(null)
// Message the composer is currently replying to.
const replyingTo = ref<Message | null>(null)

// Editing, deleting, reactions and uploads
const actions = useMessageActions({ container: messageContainer })
const { editingId: editingMessageId, pickerId: activeReactionPickerMsgId, isUploading } = actions
const menu = useMessageMenu({ actions, reply: startReply })
const contextMenu = menu.state

const {
  showUnreadPill, highlightedId, messageRows, scrollToBottom, scrollToBottomNow,
  handleScroll, resetUnreadPill
} = useMessageScroll(messageContainer)
const { dividerBeforeId, dividerLabel, markAllRead } = useUnreadDivider()

// ---- Typing indicator ----

const typingText = computed(() => {
  const line = typingLine(chatStore.typingByChannel[chatStore.activeChannel?.id ?? ''])
  return line ? t(line.key, line.params) : ''
})

function handleComposerInput() {
  if (inputMessage.value.trim()) chatStore.sendTyping(chatStore.activeChannel?.id)
}

const { showNotifHint, dismissNotifHint, enableNotifications, notificationMenuItems } = useNotificationHint(() => props.panel)

function openNotificationMenu(e: MouseEvent) {
  const channel = chatStore.activeChannel
  if (!channel) return
  if (!(e.currentTarget instanceof Element)) return
  const rect = e.currentTarget.getBoundingClientRect()
  menu.show(Math.max(8, rect.right - 200), rect.bottom + 4, notificationMenuItems(channel.id))
}

watch(() => chatStore.activeChannel?.id, () => {
  resetUnreadPill()
  replyingTo.value = null
  actions.cancelEdit()
  nextTick(() => textAreaEl.value?.focus())
})

async function handleJumpToPresent() {
  await chatStore.jumpToLatest()
}

// ---- Replies ----

function startReply(msg: Message) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => textAreaEl.value?.focus())
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
      textAreaEl.value?.focus()
    })
  }
// Immediate: a mention that opened the Talk's chat lands in its composer.
}, { immediate: true })

function onMessageKeydown(e: KeyboardEvent, msg: Message) {
  handleMessageKeydown(e, {
    rows: messageRows,
    menu: (el: HTMLElement) => menu.openAtElement(el, msg),
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

onMounted(() => textAreaEl.value?.focus())

// After sending while viewing older history, jump to the present.
async function revealOwnMessage() {
  if (chatStore.hasMoreAfter) await chatStore.jumpToLatest()
  else scrollToBottom()
}

async function handleSend() {
  const text = inputMessage.value.trim()
  if (!text || !canSend.value || isUploading.value || isSending.value) return

  isSending.value = true
  try {
    await chatStore.sendMessage(text, null, replyingTo.value?.id || null)
    inputMessage.value = ''
    replyingTo.value = null
    await revealOwnMessage()
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('chat.sendFailed')))
  } finally {
    isSending.value = false
  }
}

function handleKeyDown(e: KeyboardEvent) {
  handleComposerKeydown(e, { consumers: [slash.onKeydown, assist.onKeydown], replying: () => !!replyingTo.value, cancelReply, send: handleSend })
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
    :aria-label="panel ? $t('talk.chatPanel', { channel: channelName }) : undefined"
    class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden"
  >
    <!-- Channel Header (48px, aligned with the side columns; 40px in the Talk's chat) -->
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

    <EphemeralNotes v-if="chatStore.activeChannel" :channel-id="chatStore.activeChannel.id" />

    <!-- Composer -->
    <div class="px-4 pb-1 flex-shrink-0">
      <SlashCommandBar
        v-if="slash.picked.value && canSend"
        :command="slash.picked.value"
        :values="slash.values"
        :running="slash.running.value"
        @set="(name, value) => (slash.values[name] = value)"
        @run="slash.run()"
        @cancel="slash.cancel(); textAreaEl?.focus()"
      />
      <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="cancelReply" />
      <div
        :class="[
          'relative min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
          replyingTo ? 'rounded-b-lg' : 'rounded-lg'
        ]"
      >
        <SlashSuggestions
          v-if="slash.open.value && canSend"
          id="chat-slash"
          :items="slash.suggestions.value"
          :active="slash.active.value"
          @pick="slash.pick"
          @hover="slash.active.value = $event"
        />
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
          v-if="canAttach"
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
          @input="handleComposerInput(); assist.onInput(); slash.onInput()"
          @click="assist.onInput"
          @keyup.left="assist.onInput"
          @keyup.right="assist.onInput"
          @blur="assist.close(); slash.close()"
          aria-autocomplete="list"
          :aria-expanded="assist.open.value ? 'true' : 'false'"
          :aria-controls="assist.open.value ? 'chat-mentions' : undefined"
          :aria-activedescendant="assist.open.value ? `chat-mentions-${assist.active.value}` : undefined"
          :placeholder="placeholder"
          :disabled="!canSend"
          :aria-label="placeholder"
          rows="1"
          class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <EmojiButton :disabled="isSending || !canSend" @pick="assist.insertText" />

        <!-- Send Button -->
        <button
          @click="handleSend"
          :disabled="!canSend || !inputMessage.trim() || isUploading || isSending"
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
  </component>
</template>
