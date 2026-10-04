<script setup>
import { ref, nextTick, watch, onMounted, onUnmounted } from 'vue'
import {
  X, MessageSquare, ArrowUp, Plus, FileText, Loader2,
  Pencil, Trash2, Smile, SmilePlus, Check, Reply
} from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import ReplyPreview from './ReplyPreview.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'

const chatStore = useChatStore()
const authStore = useAuthStore()

const replyInput = ref('')
const repliesContainer = ref(null)
const fileInput = ref(null)
const isUploading = ref(false)
const isSending = ref(false)
const selectedImage = ref(null)

// Reply Editing & Reactions
const editingReplyId = ref(null)
const editReplyText = ref('')
const isSavingEdit = ref(false)
const activeReactionPickerMsgId = ref(null)
const quickEmojis = ['👍', '❤️', '😂', '🔥', '🎉', '🚀']

function startEditReply(reply) {
  editingReplyId.value = reply.id
  editReplyText.value = reply.content
  activeReactionPickerMsgId.value = null
}

function cancelEditReply() {
  editingReplyId.value = null
  editReplyText.value = ''
}

async function saveEditReply(reply) {
  if (!editReplyText.value.trim() || isSavingEdit.value) return
  isSavingEdit.value = true
  try {
    const chId = chatStore.activeChannel?.id || reply.channel_id
    await chatStore.editMessage(chId, reply.id, editReplyText.value.trim())
    editingReplyId.value = null
    editReplyText.value = ''
  } catch (err) {
    alert(err.message || 'Fehler beim Bearbeiten')
  } finally {
    isSavingEdit.value = false
  }
}

async function handleDeleteReply(reply) {
  if (confirm('Möchtest du diese Antwort wirklich löschen?')) {
    try {
      const chId = chatStore.activeChannel?.id || reply.channel_id
      await chatStore.deleteMessage(chId, reply.id)
    } catch (err) {
      alert(err.message || 'Fehler beim Löschen')
    }
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

const replyingTo = ref(null)
const replyTextArea = ref(null)
const highlightedId = ref(null)
let highlightTimer = null

watch(() => chatStore.activeThread?.id, () => {
  replyingTo.value = null
  cancelEditReply()
})

function startReply(msg) {
  replyingTo.value = msg
  activeReactionPickerMsgId.value = null
  nextTick(() => replyTextArea.value?.focus())
}

function cancelReply() {
  replyingTo.value = null
}

function flash(id) {
  clearTimeout(highlightTimer)
  highlightedId.value = null
  requestAnimationFrame(() => {
    highlightedId.value = id
    highlightTimer = setTimeout(() => { highlightedId.value = null }, 2000)
  })
}

// Thread replies are fully loaded here, so jumps stay inside the panel.
function jumpToReplied(msg) {
  const id = msg.reply_to_id
  const container = repliesContainer.value
  if (!id || !container || msg.reply_to?.deleted) {
    chatStore.showToast('Nachricht nicht gefunden')
    return
  }
  const row = container.querySelector(`[data-reply-id="${id}"]`)
  if (!row) {
    chatStore.showToast('Nachricht nicht gefunden')
    return
  }
  row.scrollIntoView({ block: 'center' })
  flash(id)
}

function toggleReactionPicker(pickerId) {
  activeReactionPickerMsgId.value = activeReactionPickerMsgId.value === pickerId ? null : pickerId
}

function handleGlobalClick(e) {
  if (activeReactionPickerMsgId.value && !e.target.closest('.reaction-picker-anchor')) {
    activeReactionPickerMsgId.value = null
  }
}

onMounted(() => {
  scrollToBottom()
  window.addEventListener('click', handleGlobalClick)
})

onUnmounted(() => {
  window.removeEventListener('click', handleGlobalClick)
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
    alert(err.message || 'Antwort konnte nicht gesendet werden')
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
    handleSendReply()
  }
}

async function handleFileUpload(e) {
  const file = e.target.files?.[0]
  if (!file) return

  isUploading.value = true
  try {
    await chatStore.uploadThreadMedia(file, '', replyingTo.value?.id || null)
    if (fileInput.value) fileInput.value.value = ''
    replyingTo.value = null
    scrollToBottom()
  } catch (err) {
    alert(err.message || 'Upload in Thread fehlgeschlagen')
  } finally {
    isUploading.value = false
  }
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatDate(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleDateString([], { day: '2-digit', month: '2-digit' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
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
            <span class="font-semibold text-base text-mnema-text flex-shrink-0">Thread</span>
            <span class="text-sm text-mnema-tertiary truncate">
              #{{ chatStore.activeChannel?.name || 'chat' }}
            </span>
          </div>
          <span class="text-xs text-mnema-tertiary truncate">Rocket.Chat Diskussion</span>
        </div>
      </div>

      <button
        @click="chatStore.closeThread"
        class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        title="Thread schließen"
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
        :class="[
          'relative group bg-mnema-elevated border border-mnema-border/80 rounded-xl p-3.5 shadow-sm space-y-2',
          highlightedId === chatStore.activeThread.id ? 'msg-flash' : ''
        ]"
      >
        <!-- Reply to the thread's root message -->
        <button
          type="button"
          @click.stop="startReply(chatStore.activeThread)"
          class="absolute right-2 top-2 hidden group-hover:flex p-1 rounded bg-mnema-elevated border border-mnema-border text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
          title="Antworten"
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
            <span 
              @click="chatStore.openUserProfile(chatStore.activeThread)"
              class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate block"
            >
              {{ chatStore.activeThread.display_name || chatStore.activeThread.username }}
            </span>
            <span class="text-xs text-mnema-tertiary font-mono">
              {{ formatDate(chatStore.activeThread.created_at) }}
            </span>
          </div>
        </div>

        <MarkdownContent v-if="chatStore.activeThread.content" :content="chatStore.activeThread.content" />

        <!-- Root Message Attachments -->
        <div v-if="chatStore.activeThread.attachments?.length" class="space-y-1.5 pt-1">
          <div 
            v-for="att in chatStore.activeThread.attachments" 
            :key="att.id"
            class="rounded-lg overflow-hidden border border-mnema-border bg-mnema-surface/40"
          >
            <template v-if="att.mime_type.startsWith('image/')">
              <img 
                :src="att.url" 
                :alt="att.original_filename" 
                @click="selectedImage = att.url"
                class="max-h-48 w-full object-cover cursor-pointer hover:opacity-90 transition"
                loading="lazy"
              />
            </template>
            <div class="p-2 flex items-center justify-between text-xs">
              <div class="flex items-center gap-1.5 truncate">
                <FileText class="w-3.5 h-3.5 text-mnema-tertiary flex-shrink-0" />
                <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate">
                  {{ att.original_filename }}
                </a>
              </div>
              <span class="text-xs font-mono text-mnema-tertiary">
                {{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB
              </span>
            </div>
          </div>
        </div>
        <!-- Root Message Reactions -->
        <div v-if="chatStore.activeThread.reactions && chatStore.activeThread.reactions.length" class="flex flex-wrap gap-1 mt-2 items-center">
          <button 
            v-for="r in chatStore.activeThread.reactions" 
            :key="r.emoji"
            @click.stop="handleToggleReaction(chatStore.activeThread.id, r.emoji)"
            :class="[
              'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-sm border transition cursor-pointer active:scale-95',
              hasUserReacted(r)
                ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
                : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
            ]"
            :title="`Reaktion ${r.emoji}`"
          >
            <span>{{ r.emoji }}</span>
            <span class="text-xs font-mono">{{ r.count }}</span>
          </button>

          <!-- Discord-style Add Reaction "+" button inline with reactions -->
          <div class="relative reaction-picker-anchor inline-block">
            <button
              @click.stop="toggleReactionPicker(`bottom-${chatStore.activeThread.id}`)"
              class="inline-flex items-center justify-center w-7 h-7 rounded-full border border-dashed border-mnema-border hover:border-mnema-accent text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition cursor-pointer text-sm"
              title="Reaktion hinzufügen"
            >
              <SmilePlus class="w-3.5 h-3.5" />
            </button>

            <!-- Quick Emoji Palette Popup from bottom -->
            <div 
              v-if="activeReactionPickerMsgId === `bottom-${chatStore.activeThread.id}`"
              class="absolute left-0 bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1.5 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-['']"
            >
              <button
                v-for="emoji in quickEmojis"
                :key="emoji"
                @click.stop="handleToggleReaction(chatStore.activeThread.id, emoji)"
                class="hover:scale-125 transition p-1 text-base rounded hover:bg-mnema-surface active:scale-95"
              >
                {{ emoji }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Thread Replies Divider -->
      <div class="flex items-center gap-2 py-1">
        <div class="flex-1 h-px bg-mnema-hairline"></div>
        <span class="text-xs text-mnema-tertiary font-mono tracking-wider uppercase">
          {{ chatStore.threadReplies.length }} {{ chatStore.threadReplies.length === 1 ? 'Antwort' : 'Antworten' }}
        </span>
        <div class="flex-1 h-px bg-mnema-hairline"></div>
      </div>

      <!-- Loading State -->
      <div v-if="chatStore.isThreadLoading" class="py-8 flex flex-col items-center justify-center text-mnema-tertiary gap-2">
        <Loader2 class="w-5 h-5 animate-spin text-mnema-accent" />
        <span class="text-xs">Thread wird geladen...</span>
      </div>

      <!-- Empty State -->
      <div 
        v-else-if="!chatStore.threadReplies.length" 
        class="py-8 text-center text-mnema-tertiary text-sm italic"
      >
        Noch keine Antworten. Schreibe die erste Nachricht im Thread!
      </div>

      <!-- Replies List -->
      <div
        v-for="reply in chatStore.threadReplies"
        :key="reply.id"
        :data-reply-id="reply.id"
        :class="[
          'relative hover:bg-mnema-surface/50 -mx-2 px-2 py-1.5 rounded-md transition-colors group',
          highlightedId === reply.id ? 'msg-flash' : ''
        ]"
      >
        <!-- Hover Quick Actions Bar -->
        <div 
          :class="[
            'absolute right-2 -top-2.5 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-0.5 shadow-md z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
            activeReactionPickerMsgId === reply.id ? 'flex' : 'hidden group-hover:flex'
          ]"
        >
          <div class="relative reaction-picker-anchor">
            <button
              @click.stop="toggleReactionPicker(reply.id)"
              :class="[
                'p-1 rounded transition',
                activeReactionPickerMsgId === reply.id
                  ? 'bg-mnema-surface text-amber-400'
                  : 'hover:bg-mnema-surface text-mnema-tertiary hover:text-amber-400'
              ]"
              title="Reagieren"
            >
              <Smile class="w-3.5 h-3.5" />
            </button>
            <div 
              v-if="activeReactionPickerMsgId === reply.id"
              class="absolute right-0 bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-['']"
            >
              <button
                v-for="emoji in quickEmojis"
                :key="emoji"
                @click.stop="handleToggleReaction(reply.id, emoji)"
                class="hover:scale-125 transition p-1 text-sm rounded hover:bg-mnema-surface active:scale-95"
              >
                {{ emoji }}
              </button>
            </div>
          </div>

          <button
            @click.stop="startReply(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
            title="Antworten"
          >
            <Reply class="w-3.5 h-3.5" />
          </button>

          <button
            v-if="reply.user_id === authStore.user?.id"
            @click.stop="startEditReply(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition"
            title="Antwort bearbeiten"
          >
            <Pencil class="w-3.5 h-3.5" />
          </button>

          <button
            v-if="reply.user_id === authStore.user?.id || authStore.isAdmin"
            @click.stop="handleDeleteReply(reply)"
            class="p-1 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-red-400 transition"
            title="Antwort löschen"
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
            <span 
              @click="chatStore.openUserProfile(reply)"
              class="font-semibold text-message text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer truncate"
            >
              {{ reply.display_name || reply.username }}
            </span>
            <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(reply.created_at) }}</span>
            <span v-if="reply.is_edited" class="text-xs text-mnema-tertiary italic">(bearbeitet)</span>
          </div>

          <!-- Inline Reply Editor -->
          <div v-if="editingReplyId === reply.id" class="mt-1 space-y-1">
            <textarea
              v-model="editReplyText"
              rows="2"
              @keydown.enter.exact.prevent="saveEditReply(reply)"
              @keydown.esc.prevent="cancelEditReply"
              class="w-full text-message p-1.5 rounded-lg bg-mnema-surface border border-mnema-accent text-mnema-text focus:outline-none resize-none"
            ></textarea>
            <div class="flex items-center justify-between text-xs text-mnema-tertiary">
              <span>Enter = Speichern, Esc = Abbrechen</span>
              <div class="flex items-center gap-1">
                <button @click="cancelEditReply" class="px-1.5 py-0.5 text-mnema-muted hover:text-mnema-text">Abbrechen</button>
                <button @click="saveEditReply(reply)" :disabled="isSavingEdit" class="px-2 py-0.5 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50">
                  <Check class="w-3.5 h-3.5" />
                  <span>Speichern</span>
                </button>
              </div>
            </div>
          </div>

          <MarkdownContent v-else-if="reply.content" :content="reply.content" class="mt-0.5" />

          <!-- Reply Attachments -->
          <div v-if="reply.attachments?.length" class="mt-1.5 space-y-1.5">
            <div 
              v-for="att in reply.attachments" 
              :key="att.id" 
              class="rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated"
            >
              <template v-if="att.mime_type.startsWith('image/')">
                <img 
                  :src="att.url" 
                  :alt="att.original_filename" 
                  @click="selectedImage = att.url"
                  class="max-h-40 w-full object-cover cursor-pointer hover:opacity-90 transition"
                  loading="lazy"
                />
              </template>
              <div class="p-1.5 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline">
                <div class="flex items-center gap-1.5 truncate">
                  <FileText class="w-3.5 h-3.5 text-mnema-tertiary flex-shrink-0" />
                  <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate">
                    {{ att.original_filename }}
                  </a>
                </div>
                <span class="text-xs font-mono text-mnema-tertiary">
                  {{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB
                </span>
              </div>
            </div>
          </div>

          <!-- Reaction Badges (Ganz unten an der Antwort, wie in Discord) -->
          <div v-if="reply.reactions && reply.reactions.length" class="flex flex-wrap gap-1 mt-1.5 items-center">
            <button 
              v-for="r in reply.reactions" 
              :key="r.emoji"
              @click.stop="handleToggleReaction(reply.id, r.emoji)"
              :class="[
                'inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full text-xs border transition cursor-pointer active:scale-95',
                hasUserReacted(r)
                  ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
                  : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
              ]"
              :title="`Reaktion ${r.emoji}`"
            >
              <span>{{ r.emoji }}</span>
              <span class="text-xs font-mono">{{ r.count }}</span>
            </button>

            <!-- Discord-style Add Reaction "+" button inline with reactions -->
            <div class="relative reaction-picker-anchor inline-block">
              <button
                @click.stop="toggleReactionPicker(`bottom-${reply.id}`)"
                class="inline-flex items-center justify-center w-7 h-7 rounded-full border border-dashed border-mnema-border hover:border-mnema-accent text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition cursor-pointer text-sm"
                title="Reaktion hinzufügen"
              >
                <SmilePlus class="w-3.5 h-3.5" />
              </button>

              <!-- Quick Emoji Palette Popup from bottom -->
              <div 
                v-if="activeReactionPickerMsgId === `bottom-${reply.id}`"
                class="absolute left-0 bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1.5 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-['']"
              >
                <button
                  v-for="emoji in quickEmojis"
                  :key="emoji"
                  @click.stop="handleToggleReaction(reply.id, emoji)"
                  class="hover:scale-125 transition p-1 text-base rounded hover:bg-mnema-surface active:scale-95"
                >
                  {{ emoji }}
                </button>
              </div>
            </div>
          </div>
        </div>
        </div>
      </div>
    </div>

    <!-- Thread Composer Input Bar -->
    <div class="px-4 pb-6 pt-1 flex-shrink-0">
      <ReplyComposerBar v-if="replyingTo" :target="replyingTo" @cancel="cancelReply" />
      <div
        :class="[
          'min-h-[52px] bg-mnema-elevated border border-mnema-border pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition',
          replyingTo ? 'rounded-b-lg' : 'rounded-lg'
        ]"
      >
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
          title="Bild oder Datei im Thread teilen"
        >
          <Plus class="w-5 h-5" />
        </button>

        <textarea
          ref="replyTextArea"
          v-model="replyInput"
          @keydown="handleKeyDown"
          placeholder="Im Thread antworten..."
          rows="1"
          class="bg-transparent flex-1 min-w-0 resize-none outline-none text-message py-0.5 text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <button
          @click="handleSendReply"
          :disabled="!replyInput.trim() || isUploading || isSending"
          class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          title="Antwort senden"
        >
          <ArrowUp class="w-4 h-4" />
        </button>
      </div>
    </div>

    <!-- Image Lightbox Modal -->
    <div 
      v-if="selectedImage" 
      @click="selectedImage = null"
      class="fixed inset-0 z-50 bg-black/90 flex items-center justify-center p-4 cursor-pointer"
    >
      <img 
        :src="selectedImage" 
        alt="Vergrößertes Bild" 
        class="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl border border-mnema-border"
      />
    </div>
  </aside>
</template>
