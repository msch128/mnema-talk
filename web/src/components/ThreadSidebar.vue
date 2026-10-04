<script setup>
import { ref, computed, nextTick, watch, onMounted } from 'vue'
import { X, MessageSquare, ArrowUp, Plus, FileText, Loader2, Image as ImageIcon } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const chatStore = useChatStore()
const authStore = useAuthStore()

const replyInput = ref('')
const repliesContainer = ref(null)
const fileInput = ref(null)
const isUploading = ref(false)
const isSending = ref(false)
const selectedImage = ref(null)

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

onMounted(() => {
  scrollToBottom()
})

async function handleSendReply() {
  const text = replyInput.value.trim()
  if (!text || isSending.value || isUploading.value) return

  isSending.value = true
  try {
    await chatStore.sendThreadReply(text)
    replyInput.value = ''
    scrollToBottom()
  } catch (err) {
    alert(err.message || 'Antwort konnte nicht gesendet werden')
  } finally {
    isSending.value = false
  }
}

function handleKeyDown(e) {
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
    await chatStore.uploadThreadMedia(file)
    if (fileInput.value) fileInput.value.value = ''
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
  <aside class="w-80 md:w-96 bg-mnema-canvas border-l border-mnema-hairline flex flex-col h-full select-none z-20 flex-shrink-0">
    <!-- Thread Header -->
    <header class="h-14 px-4 border-b border-mnema-hairline bg-mnema-canvas/90 backdrop-blur-sm flex items-center justify-between flex-shrink-0">
      <div class="flex items-center gap-2 min-w-0">
        <MessageSquare class="w-4 h-4 text-mnema-accent flex-shrink-0" />
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5">
            <span class="font-semibold text-xs text-mnema-text">Thread</span>
            <span class="text-[10px] text-mnema-tertiary font-mono truncate">
              #{{ chatStore.activeChannel?.name || 'chat' }}
            </span>
          </div>
          <span class="text-[10px] text-mnema-tertiary">Rocket.Chat Diskussion</span>
        </div>
      </div>

      <button
        @click="chatStore.closeThread"
        class="p-1.5 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        title="Thread schließen"
      >
        <X class="w-4 h-4" />
      </button>
    </header>

    <!-- Scrollable Thread Content Area -->
    <div ref="repliesContainer" class="flex-1 overflow-y-auto p-3 space-y-3">
      <!-- Root Message Card -->
      <div 
        v-if="chatStore.activeThread" 
        class="bg-mnema-elevated border border-mnema-border/80 rounded-xl p-3.5 shadow-sm space-y-2"
      >
        <div class="flex items-center gap-2.5">
          <div class="w-7 h-7 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-mnema-accent font-semibold text-xs flex-shrink-0">
            {{ chatStore.activeThread.display_name?.charAt(0).toUpperCase() || '?' }}
          </div>
          <div class="min-w-0">
            <span class="font-semibold text-xs text-mnema-text truncate block">
              {{ chatStore.activeThread.display_name || chatStore.activeThread.username }}
            </span>
            <span class="text-[9px] text-mnema-tertiary font-mono">
              {{ formatDate(chatStore.activeThread.created_at) }}
            </span>
          </div>
        </div>

        <p class="text-xs text-mnema-body-ink break-words select-text leading-relaxed">
          {{ chatStore.activeThread.content }}
        </p>

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
            <div class="p-2 flex items-center justify-between text-[11px]">
              <div class="flex items-center gap-1.5 truncate">
                <FileText class="w-3 h-3 text-mnema-tertiary flex-shrink-0" />
                <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate">
                  {{ att.original_filename }}
                </a>
              </div>
              <span class="text-[9px] font-mono text-mnema-tertiary">
                {{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Thread Replies Divider -->
      <div class="flex items-center gap-2 py-1">
        <div class="flex-1 h-px bg-mnema-hairline"></div>
        <span class="text-[10px] text-mnema-tertiary font-mono tracking-wider uppercase">
          {{ chatStore.threadReplies.length }} {{ chatStore.threadReplies.length === 1 ? 'Antwort' : 'Antworten' }}
        </span>
        <div class="flex-1 h-px bg-mnema-hairline"></div>
      </div>

      <!-- Loading State -->
      <div v-if="chatStore.isThreadLoading" class="py-8 flex flex-col items-center justify-center text-mnema-tertiary gap-2">
        <Loader2 class="w-5 h-5 animate-spin text-mnema-accent" />
        <span class="text-[11px]">Thread wird geladen...</span>
      </div>

      <!-- Empty State -->
      <div 
        v-else-if="!chatStore.threadReplies.length" 
        class="py-8 text-center text-mnema-tertiary text-xs italic"
      >
        Noch keine Antworten. Schreibe die erste Nachricht im Thread!
      </div>

      <!-- Replies List -->
      <div 
        v-for="reply in chatStore.threadReplies" 
        :key="reply.id"
        class="flex items-start gap-2.5 hover:bg-mnema-surface/40 p-2 rounded-lg transition-colors group"
      >
        <div class="w-6 h-6 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-mnema-accent font-semibold text-[10px] flex-shrink-0 mt-0.5">
          {{ reply.display_name?.charAt(0).toUpperCase() || '?' }}
        </div>

        <div class="flex-1 min-w-0">
          <div class="flex items-baseline gap-2">
            <span class="font-semibold text-xs text-mnema-text">
              {{ reply.display_name || reply.username }}
            </span>
            <span class="text-[9px] text-mnema-tertiary font-mono">{{ formatTime(reply.created_at) }}</span>
          </div>

          <p class="text-xs text-mnema-body-ink break-words select-text mt-0.5 leading-relaxed">
            {{ reply.content }}
          </p>

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
              <div class="p-1.5 flex items-center justify-between text-[10px] bg-mnema-raised border-t border-mnema-hairline">
                <div class="flex items-center gap-1.5 truncate">
                  <FileText class="w-3 h-3 text-mnema-tertiary flex-shrink-0" />
                  <a :href="att.url" target="_blank" class="text-mnema-text hover:text-mnema-accent hover:underline truncate">
                    {{ att.original_filename }}
                  </a>
                </div>
                <span class="text-[9px] font-mono text-mnema-tertiary">
                  {{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Thread Composer Input Bar -->
    <div class="p-3 border-t border-mnema-hairline bg-mnema-surface/30 flex-shrink-0">
      <div class="bg-mnema-elevated border border-mnema-border rounded-lg p-2 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition">
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
          class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition disabled:opacity-50"
          title="Bild oder Datei im Thread teilen"
        >
          <Plus class="w-4 h-4" />
        </button>

        <textarea
          v-model="replyInput"
          @keydown="handleKeyDown"
          placeholder="Im Thread antworten..."
          rows="1"
          class="bg-transparent flex-1 resize-none outline-none text-xs text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <button
          @click="handleSendReply"
          :disabled="!replyInput.trim() || isUploading || isSending"
          class="p-1.5 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          title="Antwort senden"
        >
          <ArrowUp class="w-3.5 h-3.5" />
        </button>
      </div>
    </div>

    <!-- Image Lightbox Modal -->
    <div 
      v-if="selectedImage" 
      @click="selectedImage = null"
      class="fixed inset-0 z-50 bg-black/85 backdrop-blur-md flex items-center justify-center p-4 cursor-pointer"
    >
      <img 
        :src="selectedImage" 
        alt="Vergrößertes Bild" 
        class="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl border border-mnema-border"
      />
    </div>
  </aside>
</template>
