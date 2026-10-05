<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { 
  Volume2, Mic, MicOff, Headphones, Monitor, PhoneOff, 
  MessageSquare, Maximize2, Sparkles, Send, 
  Plus, Users, Activity, Sliders
} from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()
const toasts = useToastStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare, applyAudioSettings } = useWebRTC()

// The quick toggle must swap the running mic, not just flip the setting.
function toggleNoiseCancelling() {
  voiceStore.toggleNoiseCancelling()
  // Without a mic in the call, applyAudioSettings would start a mic test instead.
  if (voiceStore.localAudioStream) applyAudioSettings()
}

const layoutMode = ref('split') // 'split' (Tafelrunde + chat) | 'focus' (Tafelrunde only)
const isFullscreen = ref(false)
const videoContainer = ref(null)
const screenVideoEl = ref(null)
// Actual resolution of the shared screen as decoded by the browser.
const videoResolution = ref('')
function onVideoResize() {
  const el = screenVideoEl.value
  videoResolution.value = el?.videoWidth ? `${el.videoWidth}×${el.videoHeight}` : ''
}
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

// Active screen stream (local or remote)
const activeScreenStream = computed(() => {
  return voiceStore.localScreenStream || voiceStore.remoteScreenStream || null
})
const isSharingOwnScreen = computed(() => {
  return !!voiceStore.localScreenStream
})

// Watch active screen stream and attach to video element
watch(activeScreenStream, (stream) => {
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
    toasts.error(err.message || t('chat.sendFailed'))
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
    toasts.error(err.message || t('chat.uploadFailed'))
  } finally {
    isUploading.value = false
  }
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleTimeString([locale.value], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <main class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden select-none">
    <!-- Tafelrunde header -->
    <header class="h-12 px-4 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 flex-shrink-0 z-10">
      <div class="flex items-center gap-3 min-w-0">
        <div class="w-8 h-8 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-sm flex-shrink-0">
          <Volume2 class="w-4 h-4" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <h2 class="font-semibold text-base leading-5 text-mnema-text truncate">
              {{ activeVoiceChannel?.name || $t('voice.channelFallback') }}
            </h2>
            <span class="text-xs leading-4 px-1.5 rounded-full border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent whitespace-nowrap flex-shrink-0">
              {{ $t('tafelrunde.participants', { count: usersInVoice.length }) }}
            </span>
          </div>
          <div class="flex items-center gap-2 text-xs text-mnema-tertiary font-mono min-w-0 whitespace-nowrap overflow-hidden">
            <!-- Live Clickable Ping Indicator -->
            <button
              @click="voiceStore.showStatsModal = true"
              class="flex items-center gap-1 text-mnema-accent hover:underline font-semibold"
              v-tooltip="$t('voice.panel.details')"
            >
              <Activity class="w-3.5 h-3.5 text-mnema-accent" />
              <span>{{ $t('tafelrunde.ping', { ms: voiceStore.rtcStats?.rttMs ?? voiceStore.ping ?? '–' }) }}</span>
            </button>
            <span>•</span>
            <!-- Noise Cancelling Status -->
            <button
              @click="toggleNoiseCancelling"
              :class="[
                'flex items-center gap-1 transition',
                voiceStore.noiseCancelling ? 'text-mnema-mint' : 'text-mnema-tertiary hover:text-mnema-text'
              ]"
              v-tooltip="voiceStore.noiseCancelling ? $t('tafelrunde.noiseOnTip') : $t('tafelrunde.noiseOffTip')"
              :aria-pressed="voiceStore.noiseCancelling ? 'true' : 'false'"
            >
              <Sparkles class="w-3.5 h-3.5" />
              <span>{{ voiceStore.noiseCancelling ? $t('tafelrunde.noiseOn') : $t('tafelrunde.noiseOff') }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- Top Right Actions -->
      <div class="flex items-center gap-2 flex-shrink-0">
        <!-- View toggle (Tafelrunde + chat vs Tafelrunde only) -->
        <button
          @click="layoutMode = layoutMode === 'split' ? 'focus' : 'split'"
          :class="[
            'h-8 flex items-center gap-1.5 px-3 rounded-md text-sm font-medium border transition whitespace-nowrap',
            layoutMode === 'split'
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
              : 'border-mnema-hairline bg-mnema-surface text-mnema-muted hover:text-mnema-text'
          ]"
          v-tooltip.visual="layoutMode === 'split' ? $t('tafelrunde.hideChatTip') : $t('tafelrunde.showChatTip')"
          :aria-pressed="layoutMode === 'split' ? 'true' : 'false'"
        >
          <MessageSquare class="w-4 h-4" />
          <span>{{ layoutMode === 'split' ? $t('tafelrunde.chatShown') : $t('tafelrunde.chatHidden') }}</span>
        </button>

        <!-- Toggle Member List Sidebar -->
        <button
          @click="chatStore.showMemberList = !chatStore.showMemberList"
          :class="[
            'w-8 h-8 flex items-center justify-center rounded-md border transition',
            chatStore.showMemberList 
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent' 
              : 'border-mnema-hairline bg-mnema-surface text-mnema-tertiary hover:text-mnema-text'
          ]"
          v-tooltip="$t('members.toggle')"
          :aria-pressed="chatStore.showMemberList ? 'true' : 'false'"
        >
          <Users class="w-4 h-4" />
        </button>
      </div>
    </header>

    <!-- Center container -->
    <div class="flex-1 flex flex-col overflow-hidden relative">
      <!-- 1. Participants and shared screen -->
      <div 
        :class="[
          'transition-all flex flex-col justify-center items-center relative overflow-hidden bg-gradient-to-b from-mnema-raised/40 to-transparent flex-shrink-0',
          layoutMode === 'split' ? 'h-64 p-4 border-b border-mnema-hairline' : 'flex-1 p-6 overflow-y-auto'
        ]"
      >
        <!-- Screen share spotlight (if sharing or viewing) -->
        <div 
          v-if="activeScreenStream" 
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
            :muted="isSharingOwnScreen"
            class="w-full h-full object-contain"
            @resize="onVideoResize"
            @loadedmetadata="onVideoResize"
          ></video>

          <div class="absolute top-3 left-3 bg-black/85 border border-white/10 px-3 py-1 rounded-md flex items-center gap-2 text-sm text-white">
            <span class="w-2 h-2 rounded-full bg-mnema-accent shadow-[0_0_6px_rgba(45,167,113,0.8)]"></span>
            <span class="font-mono font-semibold text-xs">{{ isSharingOwnScreen ? $t('tafelrunde.ownScreen') : $t('tafelrunde.sharedScreen') }}</span>
            <span v-if="videoResolution" class="text-white/60 text-xs font-mono">{{ videoResolution }}</span>
          </div>

          <div class="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity">
            <button 
              @click="toggleFullscreen" 
              v-tooltip="$t('tafelrunde.fullscreen')"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition"
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
              layoutMode === 'split' ? 'p-3 h-32 bg-mnema-surface/90' : 'p-6 h-52 bg-mnema-surface',
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
                class="text-base font-semibold text-mnema-text hover:text-mnema-accent transition cursor-pointer truncate"
              >
                {{ user.display_name || user.username }}
              </span>
              <span v-if="user.role === 'admin'" class="text-xs px-1 rounded bg-amber-500/10 text-amber-400 font-mono flex-shrink-0">
                {{ $t('role.admin') }}
              </span>
            </div>

            <!-- Speaking State Text -->
            <div class="text-xs font-mono mt-0.5">
              <span v-if="voiceStore.speakingUsers[user.id]" class="text-mnema-accent font-semibold flex items-center gap-1">
                <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent shadow-[0_0_4px_rgba(45,167,113,0.8)]"></span>
                {{ $t('tafelrunde.speaking') }}
              </span>
              <span v-else class="text-mnema-tertiary">
                {{ $t('tafelrunde.ready') }}
              </span>
            </div>
          </div>
        </div>

        <!-- Control dock -->
        <div class="absolute bottom-3 flex items-center gap-1.5 p-1.5 rounded-full bg-mnema-elevated border border-mnema-border shadow-xl z-20">
          <!-- Mute Toggle -->
          <button
            @click="voiceStore.toggleMute"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.isMuted 
                ? 'bg-mnema-danger text-white' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :aria-pressed="voiceStore.isMuted ? 'true' : 'false'"
            v-tooltip="voiceStore.isMuted ? $t('voice.unmute') : $t('voice.mute')"
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
            :aria-pressed="voiceStore.isDeafened ? 'true' : 'false'"
            v-tooltip="voiceStore.isDeafened ? $t('voice.undeafen') : $t('voice.deafen')"
          >
            <Headphones class="w-4 h-4" />
          </button>

          <!-- Screen share -->
          <button
            @click="toggleScreenShare"
            :class="[
              'flex items-center gap-1.5 px-3.5 py-2.5 rounded-full text-sm font-semibold transition',
              voiceStore.isScreenSharing 
                ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :aria-pressed="voiceStore.isScreenSharing ? 'true' : 'false'"
            v-tooltip="voiceStore.isScreenSharing ? $t('voice.stopShare') : $t('voice.share')"
          >
            <Monitor class="w-4 h-4" />
            <span class="text-sm">{{ voiceStore.isScreenSharing ? $t('tafelrunde.stopShareShort') : $t('tafelrunde.shareShort') }}</span>
          </button>

          <!-- Noise filter toggle -->
          <button
            @click="toggleNoiseCancelling"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.noiseCancelling 
                ? 'bg-mnema-accent/20 text-mnema-accent border border-mnema-accent/30' 
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-tertiary'
            ]"
            :aria-pressed="voiceStore.noiseCancelling ? 'true' : 'false'"
            v-tooltip="$t('tafelrunde.noiseToggleTip')"
          >
            <Sparkles class="w-4 h-4" />
          </button>

          <!-- Audio settings -->
          <button
            @click="voiceStore.showAudioSettings = true"
            class="p-2.5 rounded-full transition-all bg-mnema-surface hover:bg-mnema-hover text-mnema-text"
            v-tooltip="$t('audio.settings')"
          >
            <Sliders class="w-4 h-4" />
          </button>

          <div class="w-px h-5 bg-mnema-hairline mx-0.5"></div>

          <!-- Leave -->
          <button
            @click="leaveVoiceChannel"
            class="flex items-center gap-1.5 px-3.5 py-2.5 rounded-full text-sm font-semibold bg-mnema-danger text-white hover:bg-mnema-danger/90 transition shadow-sm"
            v-tooltip="$t('voice.leave')"
          >
            <PhoneOff class="w-4 h-4" />
            <span class="text-sm">{{ $t('voice.leave') }}</span>
          </button>
        </div>
      </div>

      <!-- 2. Tafelrunde chat -->
      <div 
        v-if="layoutMode === 'split'" 
        class="flex-1 flex flex-col overflow-hidden bg-mnema-canvas"
      >
        <!-- Chat Message Timeline -->
        <div ref="chatContainer" class="flex-1 overflow-y-auto overflow-x-hidden pt-2 pb-6">
          <!-- Empty State -->
          <div v-if="!chatStore.messages.length" class="h-full flex flex-col items-center justify-center text-center p-6">
            <div class="w-10 h-10 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-2 text-mnema-accent bg-mnema-surface/50">
              <MessageSquare class="w-4 h-4 opacity-80" />
            </div>
            <p class="font-semibold text-base text-mnema-text">{{ $t('tafelrunde.chatTitle', { channel: activeVoiceChannel?.name || '' }) }}</p>
            <p class="text-sm text-mnema-tertiary mt-0.5 max-w-sm">
              {{ $t('tafelrunde.chatEmpty') }}
            </p>
          </div>

          <!-- Message Rows -->
          <div
            v-for="msg in chatStore.messages"
            :key="msg.id"
            class="relative flex items-start gap-4 px-4 py-0.5 mt-[17px] first:mt-2 hover:bg-mnema-surface/50 transition-colors group"
          >
            <!-- User Avatar -->
            <UserAvatar 
              :user="msg"
              size="md"
              class="cursor-pointer hover:opacity-85 transition mt-0.5" 
              @click="chatStore.openUserProfile(msg)" 
            />

            <!-- Content Body -->
            <div class="flex-1 min-w-0">
              <div class="flex items-baseline gap-2 min-w-0">
                <span 
                  @click="chatStore.openUserProfile(msg)"
                  class="font-semibold text-message text-mnema-text hover:text-mnema-accent hover:underline transition-colors cursor-pointer truncate"
                >
                  {{ msg.display_name || msg.username }}
                </span>
                <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(msg.created_at) }}</span>
              </div>

              <MarkdownContent v-if="msg.content" :content="msg.content" />

              <!-- Attachments if any -->
              <div v-if="msg.attachments && msg.attachments.length" class="mt-2 space-y-2">
                <div 
                  v-for="att in msg.attachments" 
                  :key="att.id" 
                  class="max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-sm"
                >
                  <template v-if="att.mime_type.startsWith('image/')">
                    <img :src="att.url" :alt="att.original_filename" class="max-h-64 w-auto max-w-full object-cover" loading="lazy" />
                  </template>
                  <div class="p-2 flex items-center justify-between text-sm bg-mnema-raised border-t border-mnema-hairline">
                    <span class="truncate text-mnema-text">{{ att.original_filename }}</span>
                    <span class="text-xs font-mono text-mnema-tertiary pl-2">{{ $t('media.sizeMb', { size: (att.size_bytes / 1024 / 1024).toFixed(2) }) }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Chat Composer Bar -->
        <div class="px-4 pb-6 flex-shrink-0">
          <div class="min-h-[52px] bg-mnema-elevated border border-mnema-border rounded-lg pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition">
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
              class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition"
              v-tooltip="$t('chat.upload')"
            >
              <Plus class="w-5 h-5" />
            </button>

            <input
              v-model="chatInput"
              @keydown.enter="sendChatMessage"
              :placeholder="$t('chat.placeholder', { channel: activeVoiceChannel?.name || '' })"
              :aria-label="$t('chat.placeholder', { channel: activeVoiceChannel?.name || '' })"
              class="bg-transparent flex-1 min-w-0 outline-none text-message text-mnema-text placeholder-mnema-tertiary"
            />

            <button
              @click="sendChatMessage"
              :disabled="!chatInput.trim() || isUploading"
              class="w-8 h-8 flex items-center justify-center flex-shrink-0 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold transition disabled:opacity-20"
              v-tooltip="$t('chat.send')"
            >
              <Send class="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </main>
</template>
