<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { 
  Hash, Plus, ArrowUp, FileText, Image as ImageIcon, Users, 
  MessageSquare, MessageSquareQuote, Pencil, Trash2, Smile, SmilePlus, Check, X, Loader2,
  Phone, PhoneOff, Video, Mic, MicOff, Monitor
} from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'

const chatStore = useChatStore()
const authStore = useAuthStore()
const voiceStore = useVoiceStore()
const { leaveVoiceChannel, toggleScreenShare } = useWebRTC()

const inputMessage = ref('')
const messageContainer = ref(null)
const fileInput = ref(null)
const textAreaEl = ref(null)
const isUploading = ref(false)
const isSending = ref(false)
const selectedImage = ref(null)

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
    alert(err.message || 'Fehler beim Bearbeiten')
  } finally {
    isSavingEdit.value = false
  }
}

async function handleDeleteMessage(msg) {
  if (confirm('Möchtest du diese Nachricht wirklich löschen?')) {
    try {
      await chatStore.deleteMessage(chatStore.activeChannel.id, msg.id)
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
    if (messageContainer.value) {
      messageContainer.value.scrollTop = messageContainer.value.scrollHeight
    }
  })
}

watch(() => chatStore.messages?.length || 0, () => {
  scrollToBottom()
})

watch(() => chatStore.activeChannel?.id, () => {
  nextTick(() => {
    textAreaEl.value?.focus()
    scrollToBottom()
  })
})

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

onMounted(() => {
  scrollToBottom()
  textAreaEl.value?.focus()
  window.addEventListener('click', handleGlobalClick)
})

onUnmounted(() => {
  window.removeEventListener('click', handleGlobalClick)
})

async function handleSend() {
  const text = inputMessage.value.trim()
  if (!text || isUploading.value || isSending.value) return

  isSending.value = true
  try {
    await chatStore.sendMessage(text)
    inputMessage.value = ''
    scrollToBottom()
  } catch (err) {
    alert(err.message || 'Nachricht konnte nicht gesendet werden')
  } finally {
    isSending.value = false
  }
}

function handleKeyDown(e) {
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
    await chatStore.uploadMedia(file)
    if (fileInput.value) fileInput.value.value = ''
    scrollToBottom()
  } catch (err) {
    alert(err.message || 'Upload fehlgeschlagen')
  } finally {
    isUploading.value = false
  }
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

const currentVoiceChannelName = computed(() => {
  if (!voiceStore.currentChannelId) return ''
  for (const cat of chatStore.categories) {
    const ch = cat.channels?.find(c => c.id === voiceStore.currentChannelId)
    if (ch) return ch.name
  }
  return chatStore.uncategorized?.find(c => c.id === voiceStore.currentChannelId)?.name || 'Hangout'
})
</script>

<template>
  <main class="flex-1 bg-mnema-canvas flex flex-col h-full overflow-hidden">
    <!-- Active Voice Hangout Top Banner (if connected while browsing text) -->
    <div 
      v-if="voiceStore.isConnected" 
      class="bg-mnema-band/35 border-b border-mnema-hairline px-6 py-2 flex items-center justify-between text-xs flex-shrink-0 z-20"
    >
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-mnema-accent shadow-[0_0_6px_rgba(45,167,113,0.8)]"></span>
        <span class="font-medium text-mnema-mint text-xs">
          Aktiv im Voice: {{ currentVoiceChannelName }}
        </span>
        <button 
          @click="voiceStore.showStatsModal = true"
          class="text-[10px] text-mnema-accent hover:underline font-mono"
          title="Detaillierte Verbindungsmetrik (RTC) öffnen"
        >
          ({{ voiceStore.ping }}ms Ping)
        </button>
      </div>

      <div class="flex items-center gap-2">
        <button
          @click="voiceStore.activeView = 'voice'"
          class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-[11px] transition shadow-sm"
        >
          Zur Talk-Bühne wechseln
        </button>
        <button
          @click="leaveVoiceChannel"
          class="px-2 py-1 rounded hover:bg-mnema-danger/20 text-mnema-muted hover:text-mnema-danger text-[11px] transition"
        >
          Trennen
        </button>
      </div>
    </div>

    <!-- Channel Header -->
    <header class="h-14 px-6 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between flex-shrink-0 z-10">
      <!-- Left: Channel or DM Recipient Info -->
      <div v-if="chatStore.activeChannel?.type === 'dm'" class="flex items-center gap-3 min-w-0">
        <div class="relative cursor-pointer flex-shrink-0" @click="chatStore.openUserProfile(chatStore.activeChannel.recipient)">
          <UserAvatar :user="chatStore.activeChannel.recipient" size="sm" />
          <span 
            :class="[
              'absolute -bottom-0.5 -right-0.5 w-2.5 h-2.5 rounded-full border border-mnema-canvas',
              chatStore.onlineUserIds.has(chatStore.activeChannel.recipient?.id) ? 'bg-mnema-accent' : 'bg-mnema-tertiary'
            ]"
          ></span>
        </div>
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5">
            <span 
              @click="chatStore.openUserProfile(chatStore.activeChannel.recipient)"
              class="font-semibold text-xs text-mnema-text hover:text-mnema-accent cursor-pointer truncate"
            >
              {{ chatStore.activeChannel.name }}
            </span>
            <span class="text-[10px] text-mnema-tertiary font-mono">
              @{{ chatStore.activeChannel.recipient?.username }}
            </span>
          </div>
          <span v-if="chatStore.activeChannel.recipient?.bio" class="text-[10px] text-mnema-tertiary truncate max-w-sm">
            {{ chatStore.activeChannel.recipient.bio }}
          </span>
        </div>
      </div>

      <div v-else class="flex items-center gap-2 min-w-0">
        <Hash class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
        <span class="font-semibold text-xs text-mnema-text truncate">
          {{ chatStore.activeChannel?.name || 'Kanal auswählen' }}
        </span>
        <span v-if="chatStore.activeChannel?.topic" class="text-xs text-mnema-tertiary pl-3 border-l border-mnema-hairline truncate">
          {{ chatStore.activeChannel.topic }}
        </span>
      </div>

      <!-- Right: Call Controls & Badges -->
      <div class="flex items-center gap-2">
        <!-- Direct Message Call Controls -->
        <template v-if="chatStore.activeChannel?.type === 'dm'">
          <!-- Active DM Call Bar -->
          <div 
            v-if="chatStore.activeDmCall && chatStore.activeDmCall.channel_id === chatStore.activeChannel.id"
            class="flex items-center gap-1.5 bg-mnema-elevated border border-mnema-border px-2.5 py-1 rounded-lg shadow-sm"
          >
            <span class="flex items-center gap-1 text-[11px] text-mnema-mint font-semibold mr-1">
              <span class="w-2 h-2 rounded-full bg-mnema-accent animate-pulse"></span>
              <span>{{ chatStore.activeDmCall.status === 'connected' ? 'Verbunden (SFU)' : 'Wählt...' }}</span>
            </span>

            <button
              @click="voiceStore.toggleMute"
              :class="['p-1 rounded transition', voiceStore.isMuted ? 'text-mnema-danger bg-mnema-danger/10' : 'text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface']"
              :title="voiceStore.isMuted ? 'Mikrofon aktivieren' : 'Stummschalten'"
            >
              <MicOff v-if="voiceStore.isMuted" class="w-3.5 h-3.5" />
              <Mic v-else class="w-3.5 h-3.5" />
            </button>

            <button
              @click="toggleScreenShare"
              :class="['p-1 rounded transition', voiceStore.isScreenSharing ? 'text-mnema-accent bg-mnema-accent/10' : 'text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface']"
              title="Bildschirm übertragen (4K 60fps via SFU)"
            >
              <Monitor class="w-3.5 h-3.5" />
            </button>

            <button
              @click="chatStore.endDMCall"
              class="px-2 py-0.5 rounded bg-mnema-danger text-white text-[11px] font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 shadow-sm ml-1 cursor-pointer"
              title="Anruf beenden"
            >
              <PhoneOff class="w-3 h-3" />
              <span>Auflegen</span>
            </button>
          </div>

          <!-- Inactive Call: Start Audio / Video Call -->
          <div v-else class="flex items-center gap-1.5">
            <button
              @click="chatStore.startDMCall(chatStore.activeChannel.id, chatStore.activeChannel.recipient)"
              class="px-2.5 py-1 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover text-xs font-semibold flex items-center gap-1.5 transition active:scale-95 shadow-sm cursor-pointer"
              title="Sprachanruf starten (Server SFU)"
            >
              <Phone class="w-3.5 h-3.5" />
              <span>Anrufen</span>
            </button>
            <button
              @click="chatStore.startDMCall(chatStore.activeChannel.id, chatStore.activeChannel.recipient); voiceStore.activeView = 'voice'"
              class="p-1.5 rounded-md hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition cursor-pointer"
              title="Videoanruf starten (Server SFU)"
            >
              <Video class="w-4 h-4" />
            </button>
          </div>
        </template>

        <span class="text-[10px] font-mono text-mnema-tertiary px-2 py-0.5 rounded border border-mnema-hairline bg-mnema-surface">
          Server-SFU
        </span>

        <button
          @click="chatStore.showMemberList = !chatStore.showMemberList"
          :class="[
            'p-1.5 rounded-md transition',
            chatStore.showMemberList 
              ? 'text-mnema-accent bg-mnema-surface' 
              : 'text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface'
          ]"
          title="Mitgliederliste ein-/ausblenden"
        >
          <Users class="w-4 h-4" />
        </button>
      </div>
    </header>

    <!-- Message Timeline -->
    <div ref="messageContainer" class="flex-1 overflow-y-auto px-6 py-5 space-y-4">
      <!-- Empty State -->
      <div v-if="!chatStore.messages?.length" class="h-full flex flex-col items-center justify-center text-center p-8">
        <template v-if="chatStore.activeChannel?.type === 'dm'">
          <UserAvatar :user="chatStore.activeChannel.recipient" size="lg" class="mb-3" />
          <p class="font-semibold text-sm text-mnema-text">{{ chatStore.activeChannel.name }}</p>
          <p class="text-xs text-mnema-tertiary mt-1 max-w-xs">
            Beginn eurer privaten Direktnachrichten. Anrufe und Medien werden sicher über den Mnema-Talk Server vermittelt.
          </p>
          <div class="mt-4 flex items-center gap-2">
            <button
              @click="chatStore.startDMCall(chatStore.activeChannel.id, chatStore.activeChannel.recipient)"
              class="px-3 py-1.5 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover text-xs font-semibold flex items-center gap-1.5 transition active:scale-95 shadow-sm cursor-pointer"
            >
              <Phone class="w-3.5 h-3.5" />
              <span>Anruf starten</span>
            </button>
          </div>
        </template>
        <template v-else>
          <div class="w-12 h-12 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-3 text-mnema-accent bg-mnema-surface/50">
            <Hash class="w-5 h-5 opacity-80" />
          </div>
          <p class="font-semibold text-sm text-mnema-text">Willkommen in #{{ chatStore.activeChannel?.name || 'chat' }}</p>
          <p class="text-xs text-mnema-tertiary mt-1 max-w-xs">
            Beginn des Gesprächsverlaufs. Medien werden automatisch über den S3-Storage synchronisiert.
          </p>
        </template>
      </div>

      <!-- Messages List -->
      <div
        v-for="msg in (chatStore.messages || [])"
        :key="msg.id"
        class="relative flex items-start gap-3 hover:bg-mnema-surface/40 -mx-3 px-3 py-2 rounded-lg transition-colors group"
      >
        <!-- Hover Quick Actions Bar -->
        <div 
          :class="[
            'absolute right-3 -top-3.5 items-center gap-0.5 bg-mnema-elevated border border-mnema-border rounded-lg p-1 shadow-lg z-20 before:absolute before:-inset-2 before:content-[\'\'] before:-z-10',
            activeReactionPickerMsgId === msg.id ? 'flex' : 'hidden group-hover:flex'
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
              title="Reagieren"
            >
              <Smile class="w-3.5 h-3.5" />
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
                class="hover:scale-125 transition p-1 text-sm rounded hover:bg-mnema-surface active:scale-95"
              >
                {{ emoji }}
              </button>
            </div>
          </div>

          <!-- In Thread antworten -->
          <button
            @click.stop="chatStore.openThread(msg)"
            class="flex items-center gap-1 text-[11px] text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface px-1.5 py-1 rounded transition"
            title="In Thread antworten"
          >
            <MessageSquare class="w-3.5 h-3.5" />
          </button>

          <!-- Edit Message (if author) -->
          <button
            v-if="msg.user_id === authStore.user?.id"
            @click.stop="startEditMessage(msg)"
            class="p-1.5 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-accent transition"
            title="Nachricht bearbeiten"
          >
            <Pencil class="w-3.5 h-3.5" />
          </button>

          <!-- Delete Message (if author or admin) -->
          <button
            v-if="msg.user_id === authStore.user?.id || authStore.isAdmin"
            @click.stop="handleDeleteMessage(msg)"
            class="p-1.5 rounded hover:bg-mnema-surface text-mnema-tertiary hover:text-red-400 transition"
            title="Nachricht löschen"
          >
            <Trash2 class="w-3.5 h-3.5" />
          </button>
        </div>

        <!-- User Avatar -->
        <UserAvatar 
          :user="msg" 
          size="md"
          class="cursor-pointer hover:opacity-85 transition mt-0.5"
          @click="chatStore.openUserProfile(msg)"
        />

        <!-- Content Body -->
        <div class="flex-1 min-w-0">
          <div class="flex items-baseline gap-2">
            <span 
              @click="chatStore.openUserProfile(msg)"
              class="font-semibold text-xs text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer"
            >
              {{ msg.display_name || msg.username }}
            </span>
            <span class="text-[10px] text-mnema-tertiary font-mono">{{ formatTime(msg.created_at) }}</span>
            <span v-if="msg.is_edited" class="text-[9px] text-mnema-tertiary italic">(bearbeitet)</span>
          </div>

          <!-- Inline Message Editor -->
          <div v-if="editingMessageId === msg.id" class="mt-1 space-y-1.5">
            <textarea
              v-model="editMessageText"
              rows="2"
              @keydown.enter.exact.prevent="saveEditMessage(msg)"
              @keydown.esc.prevent="cancelEditMessage"
              class="w-full text-xs p-2 rounded-lg bg-mnema-surface border border-mnema-accent text-mnema-text focus:outline-none resize-none"
            ></textarea>
            <div class="flex items-center justify-between text-[10px] text-mnema-tertiary">
              <span>Drücke <kbd class="px-1 py-0.5 rounded bg-mnema-surface border border-mnema-border">Enter</kbd> zum Speichern, <kbd class="px-1 py-0.5 rounded bg-mnema-surface border border-mnema-border">Esc</kbd> zum Abbrechen</span>
              <div class="flex items-center gap-1.5">
                <button @click="cancelEditMessage" class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text">Abbrechen</button>
                <button @click="saveEditMessage(msg)" :disabled="isSavingEdit" class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50">
                  <Loader2 v-if="isSavingEdit" class="w-3 h-3 animate-spin" />
                  <Check v-else class="w-3 h-3" />
                  <span>Speichern</span>
                </button>
              </div>
            </div>
          </div>

          <!-- Markdown Message Content -->
          <MarkdownContent v-else-if="msg.content" :content="msg.content" class="mt-0.5" />

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
                  class="max-h-80 w-auto rounded-t object-cover cursor-pointer hover:opacity-95 transition"
                  loading="lazy"
                />
              </template>
              <template v-else-if="att.mime_type.startsWith('video/')">
                <video :src="att.url" controls class="max-h-80 w-full rounded-t"></video>
              </template>
              <div class="p-2.5 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline">
                <div class="flex items-center gap-2 truncate">
                  <FileText class="w-3.5 h-3.5 text-mnema-tertiary flex-shrink-0" />
                  <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate text-xs">
                    {{ att.original_filename }}
                  </a>
                </div>
                <span class="text-[10px] font-mono text-mnema-tertiary pl-2 flex-shrink-0">
                  {{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB
                </span>
              </div>
            </div>
          </div>

          <!-- Rocket.Chat Style Thread Counter Badge -->
          <div v-if="msg.reply_count > 0" class="mt-2">
            <button
              @click.stop="chatStore.openThread(msg)"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-medium bg-mnema-accent-subtle/80 hover:bg-mnema-accent-subtle text-mnema-accent border border-mnema-accent/30 transition shadow-xs"
            >
              <MessageSquare class="w-3.5 h-3.5 text-mnema-accent" />
              <span>{{ msg.reply_count }} {{ msg.reply_count === 1 ? 'Antwort' : 'Antworten' }}</span>
              <span class="text-[9px] opacity-75 font-mono ml-0.5">Thread öffnen &rarr;</span>
            </button>
          </div>

          <!-- Reaction Badges (Ganz unten an der Nachricht, wie in Discord) -->
          <div v-if="msg.reactions && msg.reactions.length" class="flex flex-wrap gap-1 mt-2 items-center">
            <button 
              v-for="r in msg.reactions" 
              :key="r.emoji"
              @click.stop="handleToggleReaction(msg.id, r.emoji)"
              :class="[
                'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border transition cursor-pointer active:scale-95',
                hasUserReacted(r)
                  ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
                  : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
              ]"
              :title="`Reaktion ${r.emoji}`"
            >
              <span>{{ r.emoji }}</span>
              <span class="text-[10px] font-mono">{{ r.count }}</span>
            </button>

            <!-- Discord-style Add Reaction "+" button inline with reactions -->
            <div class="relative reaction-picker-anchor inline-block">
              <button
                @click.stop="toggleReactionPicker(`bottom-${msg.id}`)"
                class="inline-flex items-center justify-center w-6 h-6 rounded-full border border-dashed border-mnema-border hover:border-mnema-accent text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition cursor-pointer text-xs"
                title="Reaktion hinzufügen"
              >
                <SmilePlus class="w-3.5 h-3.5" />
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
                  class="hover:scale-125 transition p-1 text-sm rounded hover:bg-mnema-surface active:scale-95"
                >
                  {{ emoji }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Message Input Bar (Keeper's Desk Elevated Card) -->
    <div class="px-6 pb-5 flex-shrink-0">
      <div class="bg-mnema-elevated border border-mnema-border rounded-lg p-2.5 flex items-center gap-2.5 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition">
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
          class="p-1.5 rounded-md hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
          title="Datei oder Bild hochladen (S3)"
        >
          <Plus class="w-4 h-4" />
        </button>

        <!-- Text Input -->
        <textarea
          ref="textAreaEl"
          v-model="inputMessage"
          @keydown="handleKeyDown"
          :placeholder="chatStore.activeChannel?.type === 'dm' ? `Nachricht an @${chatStore.activeChannel.name}` : `Nachricht an #${chatStore.activeChannel?.name || 'chat'}`"
          rows="1"
          class="bg-transparent flex-1 resize-none outline-none text-xs text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <!-- Send Button -->
        <button
          @click="handleSend"
          :disabled="!inputMessage.trim() || isUploading || isSending"
          class="p-1.5 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          title="Senden"
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
  </main>
</template>
