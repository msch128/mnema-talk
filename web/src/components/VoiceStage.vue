<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { 
  Volume2, Mic, MicOff, Headphones, Monitor, PhoneOff, 
  MessageSquare, Maximize2, Minimize2, Radio, Sparkles, Send, 
  Plus, Users, Activity, FileText, LayoutList, ScreenShare, Sliders
} from 'lucide-vue-next'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare } = useWebRTC()

const layoutMode = ref('split') // 'split' (talk + chat in center) | 'stage' (talk only)
const isFullscreen = ref(false)
const videoContainer = ref(null)
const screenVideoEl = ref(null)
const chatInput = ref('')
const chatContainer = ref(null)
const fileInput = ref(null)
const isUploading = ref(false)

// Current connected channel object
const activeVoiceChannel = computed(() => {
  if (!voiceStore.currentChannelId) return null
  for (const cat of chatStore.categories) {
    const ch = cat.channels?.find(c => c.id === voiceStore.currentChannelId)
    if (ch) return ch
  }
  return chatStore.uncategorized?.find(c => c.id === voiceStore.currentChannelId) || null
})

// Current users in this voice channel
const usersInVoice = computed(() => {
  if (!voiceStore.currentChannelId) return []
  const userMap = voiceStore.channelUsers[voiceStore.currentChannelId] || {}
  const list = Object.values(userMap)

  // Ensure current user is always included visually if connected
  if (authStore.user && !list.some(u => u.id === authStore.user.id)) {
    list.unshift(authStore.user)
  }
  return list
})

// Watch local screen stream and attach to video element
watch(() => voiceStore.localScreenStream, (stream) => {
  nextTick(() => {
    if (screenVideoEl.value) {
      screenVideoEl.value.srcObject = stream
    }
  })
}, { immediate: true })

function toggleScreenShare() {
  if (voiceStore.isScreenSharing) {
    stopScreenShare()
  } else {
    startScreenShare()
  }
}

function toggleFullscreen() {
  if (!document.fullscreenElement) {
    videoContainer.value?.requestFullscreen().catch(() => {})
    isFullscreen.value = true
  } else {
    document.exitFullscreen().catch(() => {})
    isFullscreen.value = false
  }
}

function scrollChatToBottom() {
  nextTick(() => {
    if (chatContainer.value) {
      chatContainer.value.scrollTop = chatContainer.value.scrollHeight
    }
  })
}

watch(() => chatStore.messages.length, () => {
  scrollChatToBottom()
})

watch(() => chatStore.pendingMention, (newVal) => {
  if (newVal) {
    chatInput.value = `${chatInput.value ? chatInput.value.trim() + ' ' : ''}@${newVal} `
    chatStore.pendingMention = ''
  }
})

onMounted(() => {
  scrollChatToBottom()
})

const isSending = ref(false)

async function sendChatMessage() {
  const text = chatInput.value.trim()
  if (!text || isUploading.value || isSending.value) return
  isSending.value = true
  try {
    await chatStore.sendMessage(text)
    chatInput.value = ''
    scrollChatToBottom()
  } catch (err) {
    alert(err.message || 'Nachricht konnte nicht gesendet werden')
  } finally {
    isSending.value = false
  }
}

async function handleFileUpload(e) {
  const file = e.target.files?.[0]
  if (!file) return

  isUploading.value = true
  try {
    await chatStore.uploadMedia(file)
    if (fileInput.value) fileInput.value.value = ''
    scrollChatToBottom()
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
  <main class="flex-1 bg-mnema-canvas flex flex-col h-full overflow-hidden select-none">
    <!-- Top Stage Header -->
    <header class="h-14 px-6 border-b border-mnema-hairline bg-mnema-canvas/90 backdrop-blur-sm flex items-center justify-between flex-shrink-0 z-10">
      <div class="flex items-center gap-3 min-w-0">
        <div class="w-8 h-8 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-xs shadow-sm flex-shrink-0">
          <Volume2 class="w-4 h-4" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <h2 class="font-semibold text-sm text-mnema-text truncate">
              {{ activeVoiceChannel?.name || 'Sprachkanal' }}
            </h2>
            <span class="text-[10px] font-mono px-2 py-0.5 rounded-full border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent">
              {{ usersInVoice.length }} Teilnehmer
            </span>
          </div>
          <div class="flex items-center gap-2 text-[10px] text-mnema-tertiary font-mono">
            <span>Opus 48kHz</span>
            <span>•</span>
            <!-- Live Clickable Ping Indicator -->
            <button
              @click="voiceStore.showStatsModal = true"
              class="flex items-center gap-1 text-mnema-accent hover:underline font-semibold"
              title="Detaillierte Verbindungsmetrik (RTC) öffnen"
            >
              <Activity class="w-3 h-3 text-mnema-accent" />
              <span>Ping: {{ voiceStore.ping }}ms</span>
            </button>
            <span>•</span>
            <!-- Noise Cancelling Status -->
            <button
              @click="voiceStore.toggleNoiseCancelling"
              :class="[
                'flex items-center gap-1 transition',
                voiceStore.noiseCancelling ? 'text-mnema-mint' : 'text-mnema-tertiary hover:text-mnema-text'
              ]"
              :title="voiceStore.noiseCancelling ? 'KI Rauschunterdrückung aktiv (RNNoise)' : 'KI Rauschunterdrückung aus'"
            >
              <Sparkles class="w-3 h-3" />
              <span>{{ voiceStore.noiseCancelling ? 'Krisp-Filter an' : 'Filter aus' }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- Top Right Actions -->
      <div class="flex items-center gap-2">
        <!-- View Toggle (Split Talk+Chat vs Full Stage) -->
        <button
          @click="layoutMode = layoutMode === 'split' ? 'stage' : 'split'"
          :class="[
            'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition',
            layoutMode === 'split'
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
              : 'border-mnema-hairline bg-mnema-surface text-mnema-muted hover:text-mnema-text'
          ]"
          :title="layoutMode === 'split' ? 'Vollbild Talk-Bühne aktivieren' : 'Chat in der Mitte anzeigen'"
        >
          <MessageSquare class="w-3.5 h-3.5" />
          <span>{{ layoutMode === 'split' ? 'Chat aktiv' : 'Chat einblenden' }}</span>
        </button>

        <!-- Toggle Member List Sidebar -->
        <button
          @click="chatStore.showMemberList = !chatStore.showMemberList"
          :class="[
            'p-1.5 rounded-md border transition',
            chatStore.showMemberList 
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent' 
              : 'border-mnema-hairline bg-mnema-surface text-mnema-tertiary hover:text-mnema-text'
          ]"
          title="Mitgliederliste ein-/ausblenden"
        >
          <Users class="w-4 h-4" />
        </button>
      </div>
    </header>

    <!-- Center Stage Container -->
    <div class="flex-1 flex flex-col overflow-hidden relative">
      <!-- 1. Voice Participant Stage & 4K Screen Share -->
      <div 
        :class="[
          'transition-all flex flex-col justify-center items-center relative overflow-hidden bg-gradient-to-b from-mnema-raised/40 to-transparent flex-shrink-0',
          layoutMode === 'split' ? 'h-64 p-4 border-b border-mnema-hairline' : 'flex-1 p-6 overflow-y-auto'
        ]"
      >
        <!-- 4K Screen Share Spotlight Mode (if sharing) -->
        <div 
          v-if="voiceStore.isScreenSharing && voiceStore.localScreenStream" 
          ref="videoContainer"
          :class="[
            'w-full max-w-5xl bg-black rounded-xl border border-mnema-border relative overflow-hidden flex items-center justify-center shadow-2xl group',
            layoutMode === 'split' ? 'h-52 mb-2' : 'h-[65vh] mb-4'
          ]"
        >
          <video 
            ref="screenVideoEl" 
            autoplay 
            playsinline 
            muted 
            class="w-full h-full object-contain"
          ></video>

          <div class="absolute top-3 left-3 bg-black/70 backdrop-blur-md border border-white/10 px-3 py-1 rounded-md flex items-center gap-2 text-xs text-white">
            <span class="w-2 h-2 rounded-full bg-mnema-accent animate-pulse"></span>
            <span class="font-mono font-semibold text-[11px]">4K 60 FPS</span>
            <span class="text-white/60 text-[10px]">Source Quality</span>
          </div>

          <div class="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity">
            <button 
              @click="toggleFullscreen" 
              class="p-2 rounded-lg bg-black/60 hover:bg-black/80 text-white transition backdrop-blur-sm"
            >
              <Maximize2 class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Participant Cards Grid -->
        <div 
          v-else 
          :class="[
            'w-full max-w-5xl grid gap-3 items-center justify-center',
            layoutMode === 'split' 
              ? 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5' 
              : 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 max-h-[70vh]'
          ]"
        >
          <div
            v-for="user in usersInVoice"
            :key="user.id"
            :class="[
              'rounded-xl border transition-all flex flex-col items-center justify-center relative shadow-sm',
              layoutMode === 'split' ? 'p-3 h-28 bg-mnema-surface/90' : 'p-6 h-52 bg-mnema-surface',
              voiceStore.speakingUsers[user.id]
                ? 'border-mnema-accent ring-2 ring-mnema-accent/40 shadow-lg shadow-mnema-accent/10 bg-mnema-surface'
                : 'border-mnema-hairline hover:border-mnema-border'
            ]"
          >
            <!-- Large Avatar -->
            <div class="relative mb-2">
              <UserAvatar 
                :user="user" 
                :size="layoutMode === 'split' ? 'lg' : 'xl'" 
                :is-speaking="!!voiceStore.speakingUsers[user.id]"
                class="cursor-pointer hover:opacity-90 transition"
                @click="chatStore.openUserProfile(user)"
              />
            </div>

            <!-- Participant Name -->
            <div class="flex items-center gap-1.5 max-w-[90%]">
              <span 
                @click="chatStore.openUserProfile(user)"
                class="text-xs font-semibold text-mnema-text hover:text-mnema-accent transition cursor-pointer truncate"
              >
                {{ user.display_name || user.username }}
              </span>
              <span v-if="user.role === 'admin'" class="text-[8px] px-1 rounded bg-amber-500/10 text-amber-400 font-mono flex-shrink-0">
                Admin
              </span>
            </div>

            <!-- Speaking State Text -->
            <div class="text-[9px] font-mono mt-0.5">
              <span v-if="voiceStore.speakingUsers[user.id]" class="text-mnema-accent font-semibold flex items-center gap-1">
                <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent animate-pulse"></span>
                Sprachaktiv
              </span>
              <span v-else class="text-mnema-tertiary">
                Bereit
              </span>
            </div>
          </div>
        </div>

        <!-- Floating Glass Audio Dock -->
        <div class="absolute bottom-3 flex items-center gap-1.5 p-1.5 rounded-full bg-mnema-elevated/90 backdrop-blur-md border border-mnema-border shadow-xl z-20">
          <!-- Mute Toggle -->
          <button
            @click="voiceStore.toggleMute"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.isMuted 
                ? 'bg-mnema-danger text-white' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :title="voiceStore.isMuted ? 'Mikrofon aktivieren' : 'Stummschalten'"
          >
            <MicOff v-if="voiceStore.isMuted" class="w-4 h-4" />
            <Mic v-else class="w-4 h-4" />
          </button>

          <!-- Deafen Toggle -->
          <button
            @click="voiceStore.toggleDeafen"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.isDeafened 
                ? 'bg-mnema-danger text-white' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :title="voiceStore.isDeafened ? 'Audio aktivieren' : 'Taub stellen'"
          >
            <Headphones class="w-4 h-4" />
          </button>

          <!-- 4K 60FPS Screen Share -->
          <button
            @click="toggleScreenShare"
            :class="[
              'flex items-center gap-1.5 px-3.5 py-2.5 rounded-full text-xs font-semibold transition',
              voiceStore.isScreenSharing 
                ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            title="Bildschirm in 4K bei 60 FPS übertragen"
          >
            <Monitor class="w-4 h-4" />
            <span class="text-[11px]">{{ voiceStore.isScreenSharing ? 'Stop' : '4K Screen' }}</span>
          </button>

          <!-- AI Noise Cancelling Toggle -->
          <button
            @click="voiceStore.toggleNoiseCancelling"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.noiseCancelling 
                ? 'bg-mnema-accent/20 text-mnema-accent border border-mnema-accent/30' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-tertiary'
            ]"
            title="KI Rauschunterdrückung (Krisp-Alternative) umschalten"
          >
            <Sparkles class="w-4 h-4" />
          </button>

          <!-- Voice & Sensitivity (Noise Gate) Settings -->
          <button
            @click="voiceStore.showAudioSettings = true"
            class="p-2.5 rounded-full transition-all bg-mnema-surface hover:bg-mnema-hover text-mnema-text"
            title="Sprach- & Empfindlichkeitseinstellungen (Noise Gate für gemeinsame Räume)"
          >
            <Sliders class="w-4 h-4" />
          </button>

          <div class="w-px h-5 bg-mnema-hairline mx-0.5"></div>

          <!-- Disconnect Button -->
          <button
            @click="leaveVoiceChannel"
            class="flex items-center gap-1.5 px-3.5 py-2.5 rounded-full text-xs font-semibold bg-mnema-danger text-white hover:bg-mnema-danger/90 transition shadow-sm"
            title="Sprachverbindung trennen"
          >
            <PhoneOff class="w-4 h-4" />
            <span class="text-[11px]">Trennen</span>
          </button>
        </div>
      </div>

      <!-- 2. Integrated Text Chat in the Center (Discord-Style in-talk chat) -->
      <div 
        v-if="layoutMode === 'split'" 
        class="flex-1 flex flex-col overflow-hidden bg-mnema-canvas"
      >
        <!-- Chat Message Timeline -->
        <div ref="chatContainer" class="flex-1 overflow-y-auto px-6 py-4 space-y-3.5">
          <!-- Empty State -->
          <div v-if="!chatStore.messages.length" class="h-full flex flex-col items-center justify-center text-center p-6">
            <div class="w-10 h-10 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-2 text-mnema-accent bg-mnema-surface/50">
              <MessageSquare class="w-4 h-4 opacity-80" />
            </div>
            <p class="font-semibold text-xs text-mnema-text">Chat in #{{ activeVoiceChannel?.name }}</p>
            <p class="text-[10px] text-mnema-tertiary mt-0.5 max-w-xs">
              Sende Nachrichten, Links und S3-Dateien direkt während des Voice-Talks.
            </p>
          </div>

          <!-- Message Rows -->
          <div
            v-for="msg in chatStore.messages"
            :key="msg.id"
            class="flex items-start gap-3 hover:bg-mnema-surface/40 -mx-3 px-3 py-1.5 rounded-lg transition-colors group"
          >
            <!-- User Avatar -->
            <UserAvatar 
              :user="msg" 
              size="sm" 
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
                <span class="text-[9px] text-mnema-tertiary font-mono">{{ formatTime(msg.created_at) }}</span>
              </div>

              <MarkdownContent v-if="msg.content" :content="msg.content" class="mt-0.5" />

              <!-- Attachments if any -->
              <div v-if="msg.attachments && msg.attachments.length" class="mt-2 space-y-2">
                <div 
                  v-for="att in msg.attachments" 
                  :key="att.id" 
                  class="max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-sm"
                >
                  <template v-if="att.mime_type.startsWith('image/')">
                    <img :src="att.url" :alt="att.original_filename" class="max-h-64 w-auto object-cover" loading="lazy" />
                  </template>
                  <div class="p-2 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline">
                    <span class="truncate text-mnema-text">{{ att.original_filename }}</span>
                    <span class="text-[9px] font-mono text-mnema-tertiary pl-2">{{ (att.size_bytes / 1024 / 1024).toFixed(2) }} MB</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Chat Composer Bar -->
        <div class="px-6 pb-4 flex-shrink-0">
          <div class="bg-mnema-elevated border border-mnema-border rounded-lg p-2 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition">
            <input 
              ref="fileInput" 
              type="file" 
              class="hidden" 
              @change="handleFileUpload" 
              accept="image/*,video/*"
            />
            <button
              @click="fileInput?.click()"
              :disabled="isUploading"
              class="p-1.5 rounded-md hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
              title="Datei oder Screenshot senden"
            >
              <Plus class="w-4 h-4" />
            </button>

            <input
              v-model="chatInput"
              @keydown.enter="sendChatMessage"
              :placeholder="`Nachricht an #${activeVoiceChannel?.name || 'talk'}...`"
              class="bg-transparent flex-1 outline-none text-xs text-mnema-text placeholder-mnema-tertiary"
            />

            <button
              @click="sendChatMessage"
              :disabled="!chatInput.trim() || isUploading"
              class="p-1.5 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20"
              title="Senden"
            >
              <Send class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </main>
</template>
