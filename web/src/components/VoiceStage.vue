<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { 
  Volume2, Mic, MicOff, Headphones, Monitor, PhoneOff, 
  MessageSquare, Maximize2, Minimize2, Radio, Sparkles, Send, Plus
} from 'lucide-vue-next'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare } = useWebRTC()

const showSideChat = ref(true)
const isFullscreen = ref(false)
const videoContainer = ref(null)
const screenVideoEl = ref(null)
const sideChatInput = ref('')
const sideChatContainer = ref(null)

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

function sendSideChatMessage() {
  if (!sideChatInput.value.trim() || !voiceStore.currentChannelId) return
  chatStore.sendMessage(sideChatInput.value)
  sideChatInput.value = ''
  nextTick(() => {
    if (sideChatContainer.value) {
      sideChatContainer.value.scrollTop = sideChatContainer.value.scrollHeight
    }
  })
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="flex-1 bg-mnema-canvas flex h-full overflow-hidden select-none">
    <!-- Main Voice & Screen Space -->
    <div class="flex-1 flex flex-col h-full overflow-hidden relative">
      <!-- Top Stage Bar -->
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
            <p class="text-[10px] text-mnema-tertiary font-mono">
              Opus 48kHz • Ping: {{ voiceStore.ping }}ms • E2E Pion SFU
            </p>
          </div>
        </div>

        <!-- Top Right Actions -->
        <div class="flex items-center gap-2">
          <!-- Toggle Side Chat -->
          <button
            @click="showSideChat = !showSideChat"
            :class="[
              'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition',
              showSideChat 
                ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent' 
                : 'border-mnema-hairline bg-mnema-surface text-mnema-muted hover:text-mnema-text'
            ]"
            title="Kanal-Chat ein-/ausblenden"
          >
            <MessageSquare class="w-3.5 h-3.5" />
            <span>Chat</span>
          </button>

          <!-- Back to Text Channels (Switch View) -->
          <button
            @click="voiceStore.activeView = 'chat'"
            class="px-3 py-1.5 rounded-md text-xs font-medium border border-mnema-hairline bg-mnema-surface text-mnema-muted hover:text-mnema-text transition"
            title="Zur Text-Ansicht wechseln (Voice bleibt aktiv)"
          >
            Zu Textkanälen
          </button>
        </div>
      </header>

      <!-- Center Stage Area -->
      <div class="flex-1 p-6 overflow-y-auto flex flex-col justify-center items-center relative">
        <!-- 1. Spotlight Mode: Screen Share Active -->
        <div 
          v-if="voiceStore.isScreenSharing && voiceStore.localScreenStream" 
          ref="videoContainer"
          class="w-full max-w-5xl h-[65vh] bg-black rounded-xl border border-mnema-border relative overflow-hidden flex items-center justify-center shadow-2xl mb-4 group"
        >
          <video 
            ref="screenVideoEl" 
            autoplay 
            playsinline 
            muted 
            class="w-full h-full object-contain"
          ></video>

          <!-- Quality & Meta Badge -->
          <div class="absolute top-3 left-3 bg-black/70 backdrop-blur-md border border-white/10 px-3 py-1 rounded-md flex items-center gap-2 text-xs text-white">
            <span class="w-2 h-2 rounded-full bg-mnema-accent animate-pulse"></span>
            <span class="font-mono font-semibold text-[11px]">4K 60 FPS</span>
            <span class="text-white/60 text-[10px]">Source Quality</span>
          </div>

          <!-- Fullscreen Action Overlay -->
          <div class="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity">
            <button 
              @click="toggleFullscreen"
              class="p-2 rounded-md bg-black/70 hover:bg-black/90 text-white transition backdrop-blur-md"
              title="Vollbild"
            >
              <Maximize2 v-if="!isFullscreen" class="w-4 h-4" />
              <Minimize2 v-else class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- 2. Participant Grid -->
        <div 
          :class="[
            'w-full max-w-5xl grid gap-4 transition-all',
            voiceStore.isScreenSharing 
              ? 'grid-cols-3 sm:grid-cols-4 md:grid-cols-6 max-h-32' 
              : 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3'
          ]"
        >
          <div
            v-for="user in usersInVoice"
            :key="user.id"
            :class="[
              'rounded-xl border transition-all duration-150 flex flex-col items-center justify-center relative overflow-hidden bg-mnema-elevated',
              voiceStore.isScreenSharing ? 'p-3 aspect-video' : 'p-8 aspect-[4/3] shadow-md',
              voiceStore.speakingUsers[user.id] 
                ? 'border-mnema-accent ring-2 ring-mnema-accent/50 shadow-lg shadow-mnema-accent/15' 
                : 'border-mnema-border'
            ]"
          >
            <!-- Large Avatar -->
            <div 
              :class="[
                'rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center font-bold text-mnema-accent transition-all duration-150',
                voiceStore.isScreenSharing ? 'w-10 h-10 text-sm' : 'w-20 h-20 text-2xl',
                voiceStore.speakingUsers[user.id] ? 'scale-105 ring-4 ring-mnema-accent/30' : ''
              ]"
            >
              {{ user.display_name?.charAt(0).toUpperCase() || '?' }}
            </div>

            <!-- User Info Label -->
            <div class="mt-3 text-center min-w-0 px-2">
              <p class="font-semibold text-xs text-mnema-text truncate">
                {{ user.display_name }}
              </p>
              <span v-if="user.role === 'admin'" class="text-[9px] px-1.5 py-0.2 rounded bg-amber-500/10 text-amber-400 font-mono inline-block mt-0.5">
                Admin
              </span>
            </div>

            <!-- Speaking Waveform Indicator (Mint Dots) -->
            <div 
              v-if="voiceStore.speakingUsers[user.id]" 
              class="absolute bottom-2.5 flex items-center gap-1"
            >
              <span class="w-1 h-3 rounded-full bg-mnema-accent animate-bounce"></span>
              <span class="w-1 h-4 rounded-full bg-mnema-accent animate-bounce [animation-delay:100ms]"></span>
              <span class="w-1 h-2 rounded-full bg-mnema-accent animate-bounce [animation-delay:200ms]"></span>
            </div>
          </div>
        </div>
      </div>

      <!-- Floating Bottom Control Dock -->
      <div class="h-20 flex items-center justify-center pb-4 flex-shrink-0 z-20">
        <div class="bg-mnema-raised/95 border border-mnema-border px-5 py-2.5 rounded-full flex items-center gap-3 shadow-2xl backdrop-blur-md">
          <!-- Mic Mute Toggle -->
          <button
            @click="voiceStore.toggleMute"
            :class="[
              'p-2.5 rounded-full transition flex items-center justify-center',
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
              'p-2.5 rounded-full transition flex items-center justify-center',
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
              'flex items-center gap-2 px-3.5 py-2 rounded-full text-xs font-semibold transition',
              voiceStore.isScreenSharing 
                ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            title="Bildschirm in 4K bei 60 FPS übertragen"
          >
            <Monitor class="w-4 h-4" />
            <span>{{ voiceStore.isScreenSharing ? 'Übertragung beenden' : '4K Bildschirm teilen' }}</span>
          </button>

          <div class="w-px h-6 bg-mnema-hairline mx-1"></div>

          <!-- Disconnect Button -->
          <button
            @click="leaveVoiceChannel"
            class="flex items-center gap-1.5 px-4 py-2 rounded-full text-xs font-semibold bg-mnema-danger text-white hover:bg-mnema-danger/90 transition shadow-sm"
            title="Sprachverbindung trennen"
          >
            <PhoneOff class="w-4 h-4" />
            <span>Verlassen</span>
          </button>
        </div>
      </div>
    </div>

    <!-- Integrated Side Chat Panel -->
    <aside 
      v-if="showSideChat" 
      class="w-80 border-l border-mnema-hairline bg-mnema-raised flex flex-col h-full flex-shrink-0 transition-all"
    >
      <!-- Chat Header -->
      <div class="h-14 px-4 border-b border-mnema-hairline flex items-center justify-between flex-shrink-0">
        <div class="flex items-center gap-2">
          <MessageSquare class="w-4 h-4 text-mnema-accent" />
          <span class="text-xs font-semibold text-mnema-text">Kanal-Chat</span>
        </div>
        <span class="text-[10px] text-mnema-tertiary font-mono">Live</span>
      </div>

      <!-- Messages Timeline in Voice -->
      <div ref="sideChatContainer" class="flex-1 overflow-y-auto p-4 space-y-3">
        <div v-if="!chatStore.messages.length" class="text-center py-8 text-xs text-mnema-tertiary">
          Noch keine Nachrichten in diesem Kanal.
        </div>
        <div 
          v-for="msg in chatStore.messages" 
          :key="msg.id"
          class="space-y-0.5 text-xs"
        >
          <div class="flex items-baseline gap-1.5">
            <span class="font-semibold text-mnema-text text-[11px]">{{ msg.display_name || msg.username }}</span>
            <span class="text-[9px] text-mnema-tertiary font-mono">{{ formatTime(msg.created_at) }}</span>
          </div>
          <p class="text-mnema-body-ink break-words leading-relaxed">{{ msg.content }}</p>
        </div>
      </div>

      <!-- Composer Bar in Voice -->
      <div class="p-3 border-t border-mnema-hairline flex-shrink-0">
        <div class="bg-mnema-surface border border-mnema-border-field rounded-md p-1.5 flex items-center gap-2">
          <input
            v-model="sideChatInput"
            @keydown.enter="sendSideChatMessage"
            type="text"
            placeholder="Nachricht im Hangout..."
            class="bg-transparent flex-1 outline-none text-xs text-mnema-text placeholder-mnema-tertiary px-1"
          />
          <button
            @click="sendSideChatMessage"
            :disabled="!sideChatInput.trim()"
            class="p-1 rounded bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition disabled:opacity-30"
          >
            <Send class="w-3 h-3" />
          </button>
        </div>
      </div>
    </aside>
  </div>
</template>
