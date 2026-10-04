<script setup>
import { ref, nextTick, watch, onMounted } from 'vue'
import { Hash, Plus, ArrowUp, FileText, Image as ImageIcon } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const chatStore = useChatStore()
const authStore = useAuthStore()

const inputMessage = ref('')
const messageContainer = ref(null)
const fileInput = ref(null)
const isUploading = ref(false)

function scrollToBottom() {
  nextTick(() => {
    if (messageContainer.value) {
      messageContainer.value.scrollTop = messageContainer.value.scrollHeight
    }
  })
}

watch(() => chatStore.messages.length, () => {
  scrollToBottom()
})

onMounted(() => {
  scrollToBottom()
})

function handleSend() {
  if (!inputMessage.value.trim() || isUploading.value) return
  chatStore.sendMessage(inputMessage.value)
  inputMessage.value = ''
  scrollToBottom()
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
    const uploadRes = await chatStore.uploadMedia(file)
    await chatStore.sendMessage(`[Datei: ${uploadRes.original_filename}]`)
    if (fileInput.value) fileInput.value.value = ''
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
</script>

<template>
  <main class="flex-1 bg-mnema-canvas flex flex-col h-full overflow-hidden">
    <!-- Channel Header -->
    <header class="h-14 px-6 border-b border-mnema-hairline bg-mnema-canvas/90 backdrop-blur-sm flex items-center justify-between flex-shrink-0 z-10">
      <div class="flex items-center gap-2 min-w-0">
        <Hash class="w-4 h-4 text-mnema-tertiary flex-shrink-0" />
        <span class="font-semibold text-xs text-mnema-text truncate">
          {{ chatStore.activeChannel?.name || 'Kanal auswählen' }}
        </span>
        <span v-if="chatStore.activeChannel?.topic" class="text-xs text-mnema-tertiary pl-3 border-l border-mnema-hairline truncate">
          {{ chatStore.activeChannel.topic }}
        </span>
      </div>

      <div class="flex items-center gap-2">
        <span class="text-[10px] font-mono text-mnema-tertiary px-2 py-0.5 rounded border border-mnema-hairline bg-mnema-surface">
          E2E WebRTC SFU
        </span>
      </div>
    </header>

    <!-- Message Timeline -->
    <div ref="messageContainer" class="flex-1 overflow-y-auto px-6 py-5 space-y-4">
      <!-- Empty State matching mnema.xyz -->
      <div v-if="!chatStore.messages.length" class="h-full flex flex-col items-center justify-center text-center p-8">
        <div class="w-12 h-12 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-3 text-mnema-accent bg-mnema-surface/50">
          <Hash class="w-5 h-5 opacity-80" />
        </div>
        <p class="font-semibold text-sm text-mnema-text">Willkommen in #{{ chatStore.activeChannel?.name || 'chat' }}</p>
        <p class="text-xs text-mnema-tertiary mt-1 max-w-xs">
          Beginn des Gesprächsverlaufs. Medien werden automatisch über den S3-Storage synchronisiert.
        </p>
      </div>

      <!-- Messages List -->
      <div
        v-for="msg in chatStore.messages"
        :key="msg.id"
        class="flex items-start gap-3 hover:bg-mnema-surface/40 -mx-3 px-3 py-1.5 rounded-lg transition-colors group"
      >
        <!-- User Avatar -->
        <div class="w-8 h-8 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-mnema-accent font-semibold text-xs flex-shrink-0 mt-0.5">
          {{ msg.display_name?.charAt(0).toUpperCase() || '?' }}
        </div>

        <!-- Content Body -->
        <div class="flex-1 min-w-0">
          <div class="flex items-baseline gap-2">
            <span class="font-semibold text-xs text-mnema-text hover:text-mnema-accent transition-colors cursor-pointer">
              {{ msg.display_name || msg.username }}
            </span>
            <span class="text-[10px] text-mnema-tertiary font-mono">{{ formatTime(msg.created_at) }}</span>
          </div>

          <p class="text-xs text-mnema-body-ink break-words select-text mt-0.5 leading-relaxed">
            {{ msg.content }}
          </p>

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
          :disabled="isUploading"
          class="p-1.5 rounded-md hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
          title="Datei oder Bild hochladen (S3)"
        >
          <Plus class="w-4 h-4" />
        </button>

        <!-- Text Input -->
        <textarea
          v-model="inputMessage"
          @keydown="handleKeyDown"
          :placeholder="`Nachricht an #${chatStore.activeChannel?.name || 'chat'}`"
          rows="1"
          class="bg-transparent flex-1 resize-none outline-none text-xs text-mnema-text placeholder-mnema-tertiary"
        ></textarea>

        <!-- Send Button -->
        <button
          @click="handleSend"
          :disabled="!inputMessage.trim() || isUploading"
          class="p-1.5 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20 disabled:bg-mnema-surface disabled:text-mnema-tertiary"
          title="Senden"
        >
          <ArrowUp class="w-4 h-4" />
        </button>
      </div>
    </div>
  </main>
</template>
