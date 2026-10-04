<script setup>
import { ref, nextTick, watch, onMounted } from 'vue'
import { Hash, PlusCircle, Send, Image, FileText } from 'lucide-vue-next'
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
    // Send message containing file preview
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
  <main class="flex-1 bg-discord-dark flex flex-col h-full overflow-hidden">
    <!-- Channel Header -->
    <header class="h-12 px-4 border-b border-discord-darkest flex items-center gap-2 shadow-sm flex-shrink-0">
      <Hash class="w-5 h-5 text-discord-muted" />
      <span class="font-bold text-white">{{ chatStore.activeChannel?.name || 'Kanal auswählen' }}</span>
      <span v-if="chatStore.activeChannel?.topic" class="text-xs text-discord-muted pl-2 border-l border-discord-darkest">
        {{ chatStore.activeChannel.topic }}
      </span>
    </header>

    <!-- Message Timeline -->
    <div ref="messageContainer" class="flex-1 overflow-y-auto p-4 space-y-4">
      <div v-if="!chatStore.messages.length" class="h-full flex flex-col items-center justify-center text-discord-muted">
        <Hash class="w-12 h-12 mb-2 opacity-50" />
        <p class="font-medium text-white">Willkommen in #{{ chatStore.activeChannel?.name || 'chat' }}!</p>
        <p class="text-xs text-discord-muted">Dies ist der Beginn des Gesprächsverlaufs.</p>
      </div>

      <div
        v-for="msg in chatStore.messages"
        :key="msg.id"
        class="flex items-start gap-3 hover:bg-discord-darker/30 -mx-4 px-4 py-1 rounded transition group"
      >
        <!-- User Avatar -->
        <div class="w-10 h-10 rounded-full bg-discord-accent flex items-center justify-center text-white font-bold text-base flex-shrink-0 mt-0.5">
          {{ msg.display_name?.charAt(0).toUpperCase() || '?' }}
        </div>

        <!-- Content -->
        <div class="flex-1 min-w-0">
          <div class="flex items-baseline gap-2">
            <span class="font-semibold text-sm text-white hover:underline cursor-pointer">
              {{ msg.display_name || msg.username }}
            </span>
            <span class="text-[11px] text-discord-muted">{{ formatTime(msg.created_at) }}</span>
          </div>

          <p class="text-sm text-discord-text break-words select-text mt-0.5 leading-relaxed">
            {{ msg.content }}
          </p>

          <!-- Media Attachments (Images, Clips) -->
          <div v-if="msg.attachments && msg.attachments.length" class="mt-2 space-y-2">
            <div v-for="att in msg.attachments" :key="att.id" class="max-w-md rounded-lg overflow-hidden border border-discord-darkest bg-discord-darkest/50">
              <template v-if="att.mime_type.startsWith('image/')">
                <img 
                  :src="att.url" 
                  :alt="att.original_filename" 
                  class="max-h-80 w-auto rounded object-cover cursor-pointer hover:opacity-95 transition"
                  loading="lazy"
                />
              </template>
              <template v-else-if="att.mime_type.startsWith('video/')">
                <video :src="att.url" controls class="max-h-80 w-full rounded"></video>
              </template>
              <div v-else class="p-3 flex items-center gap-2 text-xs text-discord-text">
                <FileText class="w-4 h-4 text-discord-muted" />
                <a :href="att.url" target="_blank" class="text-discord-accent hover:underline truncate">
                  {{ att.original_filename }}
                </a>
                <span class="text-discord-muted">({{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB)</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Message Input Bar (Discord Style) -->
    <div class="px-4 pb-4 flex-shrink-0">
      <div class="bg-discord-light rounded-lg px-4 py-2.5 flex items-center gap-3 shadow-inner">
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
          class="text-discord-muted hover:text-white transition disabled:opacity-50"
          title="Datei oder Bild hochladen (S3)"
        >
          <PlusCircle class="w-5 h-5" />
        </button>

        <!-- Text Input -->
        <textarea
          v-model="inputMessage"
          @keydown="handleKeyDown"
          :placeholder="`Nachricht an #${chatStore.activeChannel?.name || 'chat'}`"
          rows="1"
          class="bg-transparent flex-1 resize-none outline-none text-sm text-white placeholder-discord-muted"
        ></textarea>

        <!-- Send Button -->
        <button
          @click="handleSend"
          :disabled="!inputMessage.trim() || isUploading"
          class="text-discord-muted hover:text-discord-accent transition disabled:opacity-30"
        >
          <Send class="w-4 h-4" />
        </button>
      </div>
    </div>
  </main>
</template>
