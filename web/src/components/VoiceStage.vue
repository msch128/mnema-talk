<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { 
  Volume2, Mic, MicOff, Headphones, Monitor, PhoneOff, 
  MessageSquare, Maximize2, Sparkles, Send, 
  Plus, Users, Sliders, Video, VideoOff, Eye, EyeOff, X
} from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'
import ParticipantTile from './ParticipantTile.vue'
import MarkdownContent from './MarkdownContent.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()
const toasts = useToastStore()
// Which Talk is shown. Without a prop it is the one the user is in.
// Not connected to it, the stage is a preview: who is there, the chat and a
// Join button. No microphone is requested before the user joins.
const props = defineProps({
  channelId: { type: String, default: null },
  showChat: { type: Boolean, default: false }
})
const emit = defineEmits(['join', 'update:showChat'])

const { joinVoiceChannel, leaveVoiceChannel, startScreenShare, stopScreenShare, applyAudioSettings, toggleCamera } = useWebRTC()

const shownChannelId = computed(() => props.channelId || voiceStore.currentChannelId || null)
const isConnectedHere = computed(() => voiceStore.isConnected && !!shownChannelId.value && voiceStore.currentChannelId === shownChannelId.value)

function join() {
  const id = shownChannelId.value
  if (!id) return
  emit('join', id)
  joinVoiceChannel(id)
}

// The quick toggle must swap the running mic, not just flip the setting.
function toggleNoiseCancelling() {
  voiceStore.toggleNoiseCancelling()
  // Without a mic in the call, applyAudioSettings would start a mic test instead.
  if (voiceStore.localAudioStream) applyAudioSettings()
}

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
const chatInputEl = ref(null)
const assist = useComposerAssist(chatInputEl, chatInput)

function onChatKeydown(e) {
  if (assist.onKeydown(e)) return
  if (e.key === 'Enter' && !e.isComposing) {
    e.preventDefault()
    sendChatMessage()
  }
}
const chatContainer = ref(null)
const fileInput = ref(null)
const isUploading = ref(false)

// The shown channel object
const activeVoiceChannel = computed(() => {
  const id = shownChannelId.value
  if (!id) return null
  for (const cat of chatStore.categories) {
    const ch = cat.channels?.find(c => c.id === id)
    if (ch) return ch
  }
  return chatStore.uncategorized?.find(c => c.id === id) || null
})

// Participants of the shown channel (from the server's voice state)
const usersInVoice = computed(() => {
  const id = shownChannelId.value
  if (!id) return []
  const list = Object.values(voiceStore.channelUsers[id] || {})

  // Ensure the current user is always included visually while connected
  if (isConnectedHere.value && authStore.user && !list.some(u => u.id === authStore.user.id)) {
    list.unshift(authStore.user)
  }
  return list
})

// Camera stream of a participant: my own, or the one the SFU forwards.
function cameraStreamOf(user) {
  if (user.id === authStore.user?.id) return voiceStore.localCameraStream
  return voiceStore.userVideoStreams[user.id] || null
}

// Someone else's screen share is only received after I opt in. Until then
// (and for shares I watch but that are not on the stage) a card is shown.
const screenCards = computed(() => {
  if (!isConnectedHere.value) return []
  const cards = []
  for (const user of usersInVoice.value) {
    if (user.id === authStore.user?.id || !voiceStore.mediaState[user.id]?.screen) continue
    const watching = !!voiceStore.watchedScreens[user.id]
    const onStage = voiceStore.remoteScreenUserId === user.id && !!voiceStore.remoteScreenStream
    if (onStage) continue
    const state = !watching ? 'idle' : voiceStore.remoteScreenUserId === user.id ? 'pending' : 'queued'
    cards.push({ user, state })
  }
  return cards
})

function onScreenCard(card) {
  if (card.state === 'idle') voiceStore.watchScreen(card.user.id)
  else if (card.state === 'queued') voiceStore.focusScreen(card.user.id)
}

function cameraAvailable(user) {
  return !voiceStore.allCamerasOff && !!voiceStore.mediaState[user.id]?.camera
}

// The chat under the stage belongs to the shown channel, also in the preview.
watch([() => props.showChat, shownChannelId], ([show, id]) => {
  if (!show || !id || chatStore.activeChannel?.id === id) return
  if (activeVoiceChannel.value) chatStore.selectChannel(activeVoiceChannel.value)
}, { immediate: true })

// Active screen stream (local or remote)
const activeScreenStream = computed(() => {
  return voiceStore.localScreenStream || voiceStore.remoteScreenStream || null
})
const isSharingOwnScreen = computed(() => {
  return !!voiceStore.localScreenStream
})
const screenSharerName = computed(() => {
  if (isSharingOwnScreen.value) return t('talk.ownScreen')
  const sharer = usersInVoice.value.find(u => u.id === voiceStore.remoteScreenUserId)
  return sharer ? (sharer.display_name || sharer.username) : t('talk.sharedScreen')
})

// Watch active screen stream and attach to video element
watch([activeScreenStream, isConnectedHere], ([stream]) => {
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
    <!-- Talk header -->
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
              {{ $t('talk.participants', { count: usersInVoice.length }) }}
            </span>
          </div>
        </div>
      </div>

      <!-- Top Right Actions -->
      <div class="flex items-center gap-2 flex-shrink-0">
        <!-- View toggle (Talk + chat vs Talk only) -->
        <button
          @click="emit('update:showChat', !showChat)"
          :class="[
            'h-8 flex items-center gap-1.5 px-3 rounded-md text-sm font-medium border transition whitespace-nowrap',
            showChat
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
              : 'border-mnema-hairline bg-mnema-surface text-mnema-muted hover:text-mnema-text'
          ]"
          v-tooltip.visual="showChat ? $t('talk.hideChatTip') : $t('talk.showChatTip')"
          :aria-pressed="showChat ? 'true' : 'false'"
        >
          <MessageSquare class="w-4 h-4" />
          <span>{{ showChat ? $t('talk.chatShown') : $t('talk.chatHidden') }}</span>
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
          'transition-all flex flex-col items-center relative overflow-hidden bg-gradient-to-b from-mnema-raised/40 to-transparent flex-shrink-0',
          showChat ? 'h-80 p-4 pb-16 border-b border-mnema-hairline' : 'flex-1 p-6 pb-20 overflow-y-auto',
          activeScreenStream && isConnectedHere ? 'justify-start' : 'justify-center'
        ]"
      >
        <!-- Screen share spotlight (if sharing or viewing) -->
        <div
          v-if="activeScreenStream && isConnectedHere"
          ref="videoContainer"
          :class="[
            'w-full max-w-5xl bg-black rounded-xl border border-mnema-border relative overflow-hidden flex items-center justify-center shadow-2xl group',
            showChat ? 'h-44 mb-2 flex-shrink-0' : 'flex-1 min-h-0 mb-3'
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
            <span class="font-mono font-semibold text-xs">{{ screenSharerName }}</span>
            <span v-if="videoResolution" class="text-white/60 text-xs font-mono">{{ videoResolution }}</span>
          </div>

          <div class="absolute top-3 right-3 flex items-center gap-2">
            <button
              v-if="!isSharingOwnScreen"
              @click="voiceStore.unwatchScreen(voiceStore.remoteScreenUserId)"
              v-tooltip="$t('talk.unwatchScreen')"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition"
            >
              <X class="w-4 h-4" />
            </button>
            <button
              @click="toggleFullscreen"
              v-tooltip="$t('talk.fullscreen')"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition"
            >
              <Maximize2 class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Screen shares I can opt into -->
        <div
          v-if="screenCards.length"
          class="w-full max-w-5xl flex flex-wrap gap-2 flex-shrink-0 mb-3"
        >
          <div
            v-for="card in screenCards"
            :key="card.user.id"
            data-testid="screen-card"
            class="flex items-center gap-3 min-w-0 rounded-lg border border-mnema-accent/30 bg-mnema-accent-subtle pl-3 pr-1.5 py-1.5"
          >
            <Monitor class="w-4 h-4 text-mnema-accent flex-shrink-0" />
            <span class="text-sm text-mnema-text truncate">
              {{ $t('talk.screenShareCard', { name: card.user.display_name || card.user.username }) }}
            </span>
            <button
              type="button"
              :disabled="card.state === 'pending'"
              class="h-7 px-3 rounded-md text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition disabled:opacity-60 flex-shrink-0"
              @click="onScreenCard(card)"
            >
              {{ card.state === 'idle' ? $t('talk.watchScreen') : card.state === 'queued' ? $t('talk.toStage') : $t('talk.screenConnecting') }}
            </button>
            <button
              v-if="card.state !== 'idle'"
              type="button"
              class="w-7 h-7 flex items-center justify-center rounded-md text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition flex-shrink-0"
              v-tooltip="$t('talk.unwatchScreen')"
              @click="voiceStore.unwatchScreen(card.user.id)"
            >
              <X class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Participant tiles: a strip under the screen share, else the grid -->
        <div
          v-if="activeScreenStream && isConnectedHere"
          class="w-full max-w-5xl flex gap-2 overflow-x-auto flex-shrink-0 pb-1"
        >
          <ParticipantTile
            v-for="user in usersInVoice"
            :key="user.id"
            :user="user"
            :stream="cameraStreamOf(user)"
            :is-self="user.id === authStore.user?.id"
            :speaking="!!voiceStore.speakingUsers[user.id]"
            :local-muted="voiceStore.isUserLocalMuted(user.id)"
            :camera-available="cameraAvailable(user)"
            :camera-hidden="voiceStore.isCameraHidden(user.id)"
            @toggle-camera="voiceStore.toggleCameraHidden(user.id)"
            compact
            class="!w-36 !h-24 !p-1 flex-shrink-0"
            @open-profile="chatStore.openUserProfile"
          />
        </div>

        <div
          v-else-if="usersInVoice.length"
          :class="[
            'w-full max-w-5xl grid gap-3 items-center justify-center',
            showChat
              ? 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5'
              : 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4'
          ]"
        >
          <ParticipantTile
            v-for="user in usersInVoice"
            :key="user.id"
            :user="user"
            :stream="isConnectedHere ? cameraStreamOf(user) : null"
            :is-self="user.id === authStore.user?.id"
            :speaking="!!voiceStore.speakingUsers[user.id]"
            :local-muted="voiceStore.isUserLocalMuted(user.id)"
            :camera-available="cameraAvailable(user)"
            :camera-hidden="voiceStore.isCameraHidden(user.id)"
            @toggle-camera="voiceStore.toggleCameraHidden(user.id)"
            :compact="showChat"
            :show-status="isConnectedHere"
            @open-profile="chatStore.openUserProfile"
          />
        </div>

        <!-- Nobody there yet (preview) -->
        <div v-else class="text-center max-w-sm">
          <p class="font-semibold text-base text-mnema-text">{{ $t('voice.noOneInVoice') }}</p>
          <p class="text-sm text-mnema-tertiary mt-0.5">{{ $t('voice.joinToTalk') }}</p>
        </div>

        <!-- Preview: not connected to this Talk -->
        <div
          v-if="!isConnectedHere"
          class="absolute bottom-3 flex flex-col items-center gap-2 z-20"
        >
          <p class="text-xs text-mnema-tertiary text-center max-w-md px-4">{{ $t('talk.previewHint') }}</p>
          <button
            @click="join"
            :disabled="!shownChannelId"
            class="flex items-center gap-2 px-6 py-2.5 rounded-full text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition shadow-xl disabled:opacity-40"
          >
            <Volume2 class="w-4 h-4" />
            <span>{{ $t('voice.join') }}</span>
          </button>
        </div>

        <!-- Control dock -->
        <div v-else class="absolute bottom-3 flex items-center gap-1.5 p-1.5 rounded-full bg-mnema-elevated border border-mnema-border shadow-xl z-20">
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

          <!-- Camera -->
          <button
            @click="toggleCamera"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.isCameraOn
                ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover'
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :aria-pressed="voiceStore.isCameraOn ? 'true' : 'false'"
            v-tooltip="voiceStore.isCameraOn ? $t('talk.stopCamera') : $t('talk.startCamera')"
            :aria-label="voiceStore.isCameraOn ? $t('talk.stopCamera') : $t('talk.startCamera')"
          >
            <VideoOff v-if="!voiceStore.isCameraOn" class="w-4 h-4" />
            <Video v-else class="w-4 h-4" />
          </button>

          <!-- All other cameras -->
          <button
            @click="voiceStore.setAllCamerasOff(!voiceStore.allCamerasOff)"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.allCamerasOff
                ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover'
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :aria-pressed="voiceStore.allCamerasOff ? 'true' : 'false'"
            v-tooltip="voiceStore.allCamerasOff ? $t('talk.allCamerasOn') : $t('talk.allCamerasOff')"
          >
            <EyeOff v-if="voiceStore.allCamerasOff" class="w-4 h-4" />
            <Eye v-else class="w-4 h-4" />
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
            <span class="text-sm">{{ voiceStore.isScreenSharing ? $t('talk.stopShareShort') : $t('talk.shareShort') }}</span>
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
            v-tooltip="$t('talk.noiseToggleTip')"
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

      <!-- 2. Talk chat -->
      <div 
        v-if="showChat" 
        class="flex-1 flex flex-col overflow-hidden bg-mnema-canvas"
      >
        <!-- Chat Message Timeline -->
        <div ref="chatContainer" class="flex-1 overflow-y-auto overflow-x-hidden pt-2 pb-6">
          <!-- Empty State -->
          <div v-if="!chatStore.messages.length" class="h-full flex flex-col items-center justify-center text-center p-6">
            <div class="w-10 h-10 rounded-full border border-dashed border-mnema-border-strong flex items-center justify-center mb-2 text-mnema-accent bg-mnema-surface/50">
              <MessageSquare class="w-4 h-4 opacity-80" />
            </div>
            <p class="font-semibold text-base text-mnema-text">{{ $t('talk.chatTitle', { channel: activeVoiceChannel?.name || '' }) }}</p>
            <p class="text-sm text-mnema-tertiary mt-0.5 max-w-sm">
              {{ $t('talk.chatEmpty') }}
            </p>
          </div>

          <!-- Message Rows -->
          <div
            v-for="msg in chatStore.messages"
            :key="msg.id"
            :class="[
              'relative flex items-start gap-4 px-4 py-0.5 mt-[17px] first:mt-2 hover:bg-mnema-surface/50 transition-colors group',
              msg.user_id !== authStore.user?.id && chatStore.messageMentionsMe(msg) ? 'msg-mentions-me' : ''
            ]"
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
          <div class="relative min-h-[52px] bg-mnema-elevated border border-mnema-border rounded-lg pl-2 pr-2.5 py-2.5 flex items-center gap-2 shadow-sm focus-within:border-mnema-accent focus-within:ring-1 focus-within:ring-mnema-accent transition">
            <MentionSuggestions
              v-if="assist.open.value"
              id="talk-mentions"
              :items="assist.suggestions.value"
              :active="assist.active.value"
              @pick="assist.pick"
              @hover="assist.active.value = $event"
            />
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
              ref="chatInputEl"
              v-model="chatInput"
              @keydown="onChatKeydown"
              @input="assist.onInput"
              @click="assist.onInput"
              @keyup.left="assist.onInput"
              @keyup.right="assist.onInput"
              @blur="assist.close"
              aria-autocomplete="list"
              :aria-expanded="assist.open.value ? 'true' : 'false'"
              :aria-controls="assist.open.value ? 'talk-mentions' : undefined"
              :aria-activedescendant="assist.open.value ? `talk-mentions-${assist.active.value}` : undefined"
              :placeholder="$t('chat.placeholder', { channel: activeVoiceChannel?.name || '' })"
              :aria-label="$t('chat.placeholder', { channel: activeVoiceChannel?.name || '' })"
              class="bg-transparent flex-1 min-w-0 outline-none text-message text-mnema-text placeholder-mnema-tertiary"
            />

            <EmojiButton @pick="assist.insertText" />

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
