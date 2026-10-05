<script setup>
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import {
  Volume2, VolumeX, Mic, MicOff, Headphones, Monitor, MonitorOff, PhoneOff,
  MessageSquare, Maximize2, Minimize2, Sparkles, Send,
  Plus, Users, Sliders, Video, VideoOff, Eye, EyeOff, X, UserRoundX
} from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'
import ParticipantTile from './ParticipantTile.vue'
import MarkdownContent from './MarkdownContent.vue'
import TalkParticipants from './TalkParticipants.vue'
import ScreenViewers from './ScreenViewers.vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState, buildMemberItems } from '../composables/useNavMenus'
import VoiceTimer from './VoiceTimer.vue'
import EmojiButton from './EmojiButton.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import MessageAttachments from './MessageAttachments.vue'
import ImageLightbox from './ImageLightbox.vue'
import { useComposerAssist } from '../composables/useComposerAssist'
import { useTalkStage } from '../composables/useTalkStage'
import { useVideoGrid } from '../composables/useVideoGrid'
import { useMessageActions, formatTime } from '../composables/useMessageActions'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'

const voiceStore = useVoiceStore()

// Right-click on a tile: the member menu with their volume (like the sidebar).
const menu = useMenuState()
function openMemberMenu(e, user) {
  menu.show(e, refresh => buildMemberItems(user, { refresh }))
}
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

// Resolves to whether the join went ahead (false when the user kept their
// current Talk in the switch confirmation).
async function join() {
  const id = shownChannelId.value
  if (!id) return false
  if (voiceStore.warnSwitchChannel && voiceStore.currentChannelId && voiceStore.currentChannelId !== id) {
    const ch = chatStore.allChannels.find(c => c.id === id)
    const name = ch?.name || ''
    const ok = await confirm({
      title: t('audio.switchChannelTitle'),
      body: t('audio.switchChannelPrompt', { channel: name }),
      confirmLabel: t('audio.switchChannelConfirm'),
      cancelLabel: t('common.cancel'),
      danger: false
    })
    if (!ok) return false
  }
  emit('join', id)
  joinVoiceChannel(id)
  return true
}

// The quick toggle must swap the running mic, not just flip the setting.
function toggleNoiseCancelling() {
  voiceStore.toggleNoiseCancelling()
  // Without a mic in the call, applyAudioSettings would start a mic test instead.
  if (voiceStore.localAudioStream) applyAudioSettings()
}

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
const { isUploading, upload } = useMessageActions({ container: chatContainer })
const selectedImage = ref(null)

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

// The stage shows one screen share or camera. Every other screen share is a
// card that puts it there (someone else's is only received after I opt in),
// a click on a camera tile puts that camera there.
const {
  stage,
  ownScreenOnStage: ownOnStage,
  remoteScreenUserId: stageUserId,
  cameraOnStage,
  cards: screenCards,
  selectCard: onScreenCard,
  canFocusCamera,
  isCameraFocused,
  toggleCamera: toggleCameraFocus
} = useTalkStage({
  users: () => usersInVoice.value,
  myId: () => authStore.user?.id,
  active: () => isConnectedHere.value,
  watch: userId => watchStream(userId)
})

async function watchStream(userId) {
  if (!isConnectedHere.value && !(await join())) return
  voiceStore.watchScreen(userId)
}

// Audio controls on the stage: only for someone else's screen share.
const currentStreamVolume = computed(() => {
  const uid = stageUserId.value
  return uid ? voiceStore.getUserVolume(uid) : 100
})

const isCurrentStreamMuted = computed(() => {
  const uid = stageUserId.value
  return uid ? voiceStore.isUserLocalMuted(uid) : false
})

function toggleCurrentStreamMute() {
  const uid = stageUserId.value
  if (uid) voiceStore.toggleLocalMute(uid)
}

function onStreamVolumeChange(e) {
  const uid = stageUserId.value
  if (uid) {
    voiceStore.setUserVolume(uid, Number(e.target.value))
  }
}

function cameraAvailable(user) {
  return !voiceStore.allCamerasOff && !!voiceStore.mediaState[user.id]?.camera
}

// "Hide participants without video" (Discord's "only videos"): only tiles
// with a camera I receive or a screen share, mine included.
function hasVideo(user) {
  if (user.id === authStore.user?.id) return !!voiceStore.localCameraStream || voiceStore.isScreenSharing
  const media = voiceStore.mediaState[user.id]
  return !!media?.screen || (!!media?.camera && !voiceStore.isCameraHidden(user.id))
}
const hidingNoVideo = computed(() => isConnectedHere.value && voiceStore.hideNoVideo)
const tileUsers = computed(() => (hidingNoVideo.value ? usersInVoice.value.filter(hasVideo) : usersInVoice.value))

// The grid fits every tile into the free area at 16:9, like Discord. Tiles
// without any camera stay smaller: a huge avatar tile only looks empty.
const gridArea = ref(null)
const GRID_GAP = 12
const gridHasVideo = computed(() => isConnectedHere.value && tileUsers.value.some(u => !!cameraStreamOf(u)))
const { layout: gridLayout, gridStyle, tileStyle } = useVideoGrid(gridArea, {
  count: () => tileUsers.value.length,
  gap: GRID_GAP,
  maxTileWidth: () => (gridHasVideo.value ? 1280 : 640),
  minTileWidth: 160
})
const gridSized = computed(() => gridLayout.value.tileWidth > 0)

// The chat under the stage belongs to the shown channel, also in the preview.
watch([() => props.showChat, shownChannelId], ([show, id]) => {
  if (!show || !id || chatStore.activeChannel?.id === id) return
  if (activeVoiceChannel.value) chatStore.selectChannel(activeVoiceChannel.value)
}, { immediate: true })

const activeScreenStream = computed(() => stage.value?.stream || null)
// What the stage shows, as data-stage-source: 'own', a user ID (their screen)
// or 'camera:<user ID>'.
const stageSource = computed(() => {
  const s = stage.value
  if (!s) return null
  if (s.kind === 'camera') return `camera:${s.userId}`
  return s.own ? 'own' : s.userId
})
const stageName = computed(() => {
  const s = stage.value
  if (s?.kind === 'camera' && s.own) return t('talk.ownCamera')
  if (ownOnStage.value) return t('talk.ownScreen')
  const sharer = usersInVoice.value.find(u => u.id === s?.userId)
  if (sharer) return sharer.display_name || sharer.username
  return cameraOnStage.value ? t('talk.camera') : t('talk.sharedScreen')
})

// My own share is only previewed while I look at this tab: decoding my own
// screen costs CPU/GPU while I'm busy elsewhere, and the stream keeps going.
const pageActive = ref(isPageActive())
function isPageActive() {
  if (typeof document === 'undefined') return true
  return document.visibilityState === 'visible' && (typeof document.hasFocus !== 'function' || document.hasFocus())
}
function updatePageActive() {
  pageActive.value = isPageActive()
}
onMounted(() => {
  document.addEventListener('visibilitychange', updatePageActive)
  window.addEventListener('focus', updatePageActive)
  window.addEventListener('blur', updatePageActive)
})
onUnmounted(() => {
  document.removeEventListener('visibilitychange', updatePageActive)
  window.removeEventListener('focus', updatePageActive)
  window.removeEventListener('blur', updatePageActive)
})
const ownPreviewPaused = computed(() => ownOnStage.value && !pageActive.value)

// Watch active screen stream and attach to video element
watch([activeScreenStream, isConnectedHere, ownPreviewPaused], ([stream, , paused]) => {
  nextTick(() => {
    if (screenVideoEl.value) {
      screenVideoEl.value.srcObject = paused ? null : stream
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

// --- Full screen: the stage's button, a double-click and the F key ---
const isFullscreen = ref(false)
function updateFullscreen() {
  isFullscreen.value = !!document.fullscreenElement && document.fullscreenElement === videoContainer.value
}

async function enterFullscreen() {
  await nextTick() // the stage may only just have appeared
  const el = videoContainer.value
  if (!el || document.fullscreenElement === el || typeof el.requestFullscreen !== 'function') return
  Promise.resolve(el.requestFullscreen()).catch(() => {})
}

function toggleFullscreen() {
  if (document.fullscreenElement) Promise.resolve(document.exitFullscreen?.()).catch(() => {})
  else enterFullscreen()
}

// A double-click on a camera puts it on the stage in full screen. Its first
// click already put it on the stage (or took it off) without waiting, and
// moved the tiles: the second click and the dblclick may land elsewhere, so
// the camera clicked last decides.
const DOUBLE_CLICK_MS = 600
let lastCameraClick = null
function onTileFocusCamera(user) {
  lastCameraClick = { userId: user.id, at: Date.now() }
  toggleCameraFocus(user)
}
function recentCameraClick() {
  return lastCameraClick && Date.now() - lastCameraClick.at < DOUBLE_CLICK_MS ? lastCameraClick.userId : null
}
function fullscreenCamera(userId) {
  const id = recentCameraClick() || userId
  lastCameraClick = null
  if (!isConnectedHere.value || !id) return
  if (!(stage.value?.kind === 'camera' && stage.value.userId === id)) voiceStore.focusCamera(id)
  if (stage.value?.kind === 'camera' && stage.value.userId === id) enterFullscreen()
}

// Double-clicks in the Talk area: on the stage they toggle full screen.
function onAreaDblclick(e) {
  if (e.target?.closest?.('button, input, a, [data-testid="screen-viewers"]')) return
  const recent = recentCameraClick()
  if (recent) {
    fullscreenCamera(recent)
    return
  }
  if (videoContainer.value?.contains(e.target)) toggleFullscreen()
}

function isTypingTarget(el) {
  return !!el && (el.isContentEditable || !!el.closest?.('input, textarea, select, [contenteditable]:not([contenteditable="false"])'))
}

// F toggles the stage's full screen, like in a video player: while the
// focus is in the Talk view (or nowhere), not while typing, with a dialog or
// menu open, or when F is the push-to-talk key.
const root = ref(null)
function inTalkView(el) {
  return !el || el === document.body || el === document.documentElement || !!root.value?.contains(el)
}
function onKeydown(e) {
  if (e.key !== 'f' && e.key !== 'F') return
  if (e.ctrlKey || e.metaKey || e.altKey || e.repeat || e.isComposing || e.defaultPrevented) return
  if (!inTalkView(e.target) || isTypingTarget(e.target)) return
  if (e.target?.closest?.('[role="dialog"], [role="menu"]') || document.querySelector('[aria-modal="true"]')) return
  if (voiceStore.inputMode === 'ptt' && voiceStore.pttKey === e.code) return
  if (!videoContainer.value) return
  e.preventDefault()
  toggleFullscreen()
}

onMounted(() => {
  document.addEventListener('fullscreenchange', updateFullscreen)
  document.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  document.removeEventListener('fullscreenchange', updateFullscreen)
  document.removeEventListener('keydown', onKeydown)
})

// Right-click on the stage: its actions as a menu (outside full screen,
// where the page's menus cannot show).
function openStageMenu(e) {
  if (document.fullscreenElement) return
  e.preventDefault()
  menu.show(e, () => {
    const items = [{
      id: 'fullscreen',
      label: t('talk.fullscreen'),
      icon: Maximize2,
      shortcut: 'F',
      action: () => enterFullscreen()
    }]
    if (cameraOnStage.value) {
      items.push({ id: 'unfocus', label: t('talk.unfocusCamera'), icon: X, action: () => voiceStore.unfocusCamera() })
    } else if (ownOnStage.value) {
      items.push({ id: 'stop-share', label: t('voice.stopShare'), icon: MonitorOff, danger: true, action: () => stopScreenShare() })
    } else if (stageUserId.value) {
      const uid = stageUserId.value
      items.push({ id: 'unwatch', label: t('talk.stopWatching'), icon: X, action: () => voiceStore.unwatchScreen(uid) })
    }
    return items
  })
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
  if (await upload(e.target, file => chatStore.uploadMedia(file))) scrollChatToBottom()
}
</script>

<template>
  <main ref="root" class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden select-none">
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
            <TalkParticipants :users="usersInVoice" :started-at="voiceStore.roomStartedAt[shownChannelId] || ''" />
            <VoiceTimer
              v-if="voiceStore.roomStartedAt[shownChannelId]"
              :since="voiceStore.roomStartedAt[shownChannelId]"
              data-testid="talk-timer"
              v-tooltip="$t('talk.runningForTip')"
              class="text-xs text-mnema-tertiary"
            />
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

        <!-- Only participants with video (per browser) -->
        <button
          v-if="isConnectedHere"
          type="button"
          data-testid="hide-no-video"
          @click="voiceStore.toggleHideNoVideo()"
          :class="[
            'w-8 h-8 flex items-center justify-center rounded-md border transition',
            voiceStore.hideNoVideo
              ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
              : 'border-mnema-hairline bg-mnema-surface text-mnema-tertiary hover:text-mnema-text'
          ]"
          v-tooltip="$t('talk.hideNoVideo')"
          :aria-pressed="voiceStore.hideNoVideo ? 'true' : 'false'"
        >
          <UserRoundX class="w-4 h-4" />
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
        @dblclick="onAreaDblclick"
      >
        <!-- The stage: one screen share (mine or one I watch) or one camera -->
        <div
          v-if="activeScreenStream && isConnectedHere"
          ref="videoContainer"
          data-testid="stage"
          :data-stage-source="stageSource"
          :data-fullscreen="isFullscreen ? 'true' : undefined"
          @contextmenu="openStageMenu"
          :class="[
            'w-full max-w-5xl bg-black border-mnema-border relative overflow-hidden flex items-center justify-center shadow-2xl group',
            isFullscreen ? '' : 'rounded-xl border',
            showChat ? 'h-44 mb-2 flex-shrink-0' : 'flex-1 min-h-0 mb-3'
          ]"
        >
          <video
            ref="screenVideoEl"
            autoplay
            playsinline
            :muted="ownOnStage || cameraOnStage"
            :class="['w-full h-full object-contain', stage?.kind === 'camera' && stage.own ? '-scale-x-100' : '']"
            @resize="onVideoResize"
            @loadedmetadata="onVideoResize"
          ></video>

          <!-- Own share while I'm elsewhere: no preview, but it keeps running -->
          <div
            v-if="ownPreviewPaused"
            data-testid="own-stream-paused"
            class="absolute inset-0 flex flex-col items-center justify-center gap-1.5 bg-mnema-canvas/95 text-center px-6"
          >
            <span class="text-base font-semibold text-mnema-text">{{ $t('talk.ownStreamRunning') }}</span>
            <span class="text-sm text-mnema-tertiary">{{ $t('talk.ownStreamRunningHint') }}</span>
          </div>

          <div class="absolute top-3 left-3 bg-black/85 border border-white/10 px-2.5 py-1 rounded-md flex items-center gap-2 text-sm text-white">
            <Video v-if="cameraOnStage" class="w-3.5 h-3.5 text-white/80" />
            <span v-else class="px-1.5 py-0.5 rounded bg-red-600 text-white text-[10px] font-black uppercase tracking-wider flex items-center gap-1 shadow-sm">
              <span class="w-1.5 h-1.5 rounded-full bg-white animate-pulse"></span>
              {{ $t('talk.live') }}
            </span>
            <ScreenViewers v-if="!cameraOnStage && stage" :user-id="stage.userId" :show-zero="ownOnStage" class="-mx-1" />
            <span class="font-mono font-semibold text-xs">{{ stageName }}</span>
            <span v-if="videoResolution" class="text-white/60 text-xs font-mono">{{ videoResolution }}</span>
          </div>

          <div class="absolute top-3 right-3 flex items-center gap-2 z-20">
            <!-- Stream Audio Toggle for Streamer (Own Screen) -->
            <button
              v-if="ownOnStage"
              type="button"
              data-testid="streamer-audio-toggle"
              :class="[
                'p-2 rounded-lg transition text-white',
                voiceStore.isScreenAudioMuted ? 'bg-mnema-danger/80 hover:bg-mnema-danger' : 'bg-black/75 hover:bg-black/90'
              ]"
              v-tooltip="voiceStore.isScreenAudioMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
              @click="voiceStore.toggleScreenAudioMute"
            >
              <VolumeX v-if="voiceStore.isScreenAudioMuted" class="w-4 h-4" />
              <Volume2 v-else class="w-4 h-4" />
            </button>

            <!-- Viewer Stream Audio Controls (Volume & Mute) -->
            <div
              v-else-if="stageUserId"
              class="flex items-center gap-1.5 bg-black/75 hover:bg-black/90 px-2 py-1.5 rounded-lg text-white group/vol"
            >
              <button
                type="button"
                data-testid="viewer-stream-audio-mute"
                class="p-0.5 rounded text-white hover:text-mnema-accent transition"
                v-tooltip="isCurrentStreamMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
                @click="toggleCurrentStreamMute"
              >
                <VolumeX v-if="isCurrentStreamMuted" class="w-4 h-4 text-mnema-danger" />
                <Volume2 v-else class="w-4 h-4" />
              </button>
              <input
                type="range"
                min="0"
                max="200"
                data-testid="viewer-stream-volume-slider"
                :value="currentStreamVolume"
                class="w-16 h-1 accent-mnema-accent cursor-pointer opacity-80 group-hover/vol:opacity-100 transition"
                v-tooltip="`${currentStreamVolume}%`"
                @input="onStreamVolumeChange"
              />
            </div>

            <!-- A camera leaves the stage (back to the screen share or the grid) -->
            <button
              v-if="cameraOnStage"
              type="button"
              data-testid="stage-unfocus-camera"
              v-tooltip="$t('talk.unfocusCamera')"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition"
              @click="voiceStore.unfocusCamera()"
            >
              <X class="w-4 h-4" />
            </button>
            <!-- Stop Watching (Viewer) -->
            <button
              v-else-if="!ownOnStage"
              @click="voiceStore.unwatchScreen(stageUserId)"
              :aria-label="$t('talk.unwatchScreen')"
              v-tooltip="$t('talk.stopWatching')"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition hover:text-mnema-danger"
            >
              <X class="w-4 h-4" />
            </button>
            <!-- Stop Sharing (Streamer) -->
            <button
              v-else
              @click="stopScreenShare"
              v-tooltip="$t('voice.stopShare')"
              class="p-2 rounded-lg bg-mnema-danger/80 hover:bg-mnema-danger text-white transition"
            >
              <MonitorOff class="w-4 h-4" />
            </button>

            <!-- Fullscreen (also a double-click on the stage or F) -->
            <button
              type="button"
              data-testid="stage-fullscreen"
              @click="toggleFullscreen"
              v-tooltip="{ text: isFullscreen ? $t('talk.exitFullscreen') : $t('talk.fullscreen'), shortcut: 'F' }"
              :aria-pressed="isFullscreen ? 'true' : 'false'"
              class="p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition"
            >
              <Minimize2 v-if="isFullscreen" class="w-4 h-4" />
              <Maximize2 v-else class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Screen shares not on the stage: mine, ones I watch, ones I can opt into -->
        <div
          v-if="screenCards.length"
          class="w-full max-w-5xl flex flex-wrap gap-2 flex-shrink-0 mb-3"
        >
          <div
            v-for="card in screenCards"
            :key="card.key"
            data-testid="screen-card"
            :data-screen-card="card.key"
            class="flex items-center gap-3 min-w-0 rounded-lg border border-mnema-accent/30 bg-mnema-accent-subtle pl-3 pr-1.5 py-1.5"
          >
            <Monitor class="w-4 h-4 text-mnema-accent flex-shrink-0" />
            <span class="text-sm text-mnema-text truncate">
              {{ card.kind === 'own' ? $t('talk.ownScreen') : $t('talk.screenShareCard', { name: card.user.display_name || card.user.username }) }}
            </span>
            <ScreenViewers
              :user-id="card.kind === 'own' ? (authStore.user?.id || '') : card.user.id"
              :show-zero="card.kind === 'own'"
              variant="card"
            />
            <button
              type="button"
              data-testid="screen-card-action"
              :disabled="card.state === 'pending'"
              class="h-7 px-3 rounded-md text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition disabled:opacity-60 flex-shrink-0"
              @click="onScreenCard(card)"
            >
              {{ card.state === 'idle' ? $t('talk.watchScreen') : card.state === 'queued' ? $t('talk.toStage') : $t('talk.screenConnecting') }}
            </button>
            <button
              v-if="card.kind === 'remote' && card.state !== 'idle'"
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
          data-testid="talk-strip"
          :class="['w-full max-w-5xl flex gap-2 overflow-x-auto flex-shrink-0 pb-1', tileUsers.length ? '' : 'hidden']"
        >
          <ParticipantTile
            v-for="user in tileUsers"
            :key="user.id"
            :user="user"
            :stream="cameraStreamOf(user)"
            :is-self="user.id === authStore.user?.id"
            :speaking="voiceStore.isSpeaking(user.id)"
            :muted="voiceStore.muteStateOf(user.id).muted"
            :deafened="voiceStore.muteStateOf(user.id).deafened"
            :local-muted="voiceStore.isUserLocalMuted(user.id)"
            :camera-available="cameraAvailable(user)"
            :camera-hidden="voiceStore.isCameraHidden(user.id)"
            :is-screensharing="!!voiceStore.mediaState[user.id]?.screen"
            :is-watching="!!voiceStore.watchedScreens[user.id]"
            :is-connecting="voiceStore.remoteScreenUserId === user.id && !voiceStore.remoteScreenStream"
            :camera-focusable="canFocusCamera(user)"
            :camera-focused="isCameraFocused(user)"
            @toggle-camera="voiceStore.toggleCameraHidden(user.id)"
            @focus-camera="onTileFocusCamera(user)"
            @fullscreen-camera="fullscreenCamera(user.id)"
            @watch-stream="watchStream(user.id)"
            @stop-watching="voiceStore.unwatchScreen(user.id)"
            compact
            class="!w-36 !h-24 !p-1 flex-shrink-0"
            @open-profile="chatStore.openUserProfile"
            @menu="openMemberMenu($event, user)"
          />
        </div>

        <!-- The grid: every participant as a 16:9 tile, as large as the area allows -->
        <div
          v-else-if="tileUsers.length"
          ref="gridArea"
          data-testid="talk-grid"
          :data-grid-cols="gridLayout.cols"
          :class="[
            'w-full flex-1 min-h-0 flex justify-center',
            gridLayout.overflow ? 'items-start overflow-y-auto' : 'items-center'
          ]"
        >
          <div
            :class="['flex flex-wrap justify-center content-center', gridSized ? '' : 'w-full max-w-5xl gap-3']"
            :style="gridStyle"
          >
            <ParticipantTile
              v-for="user in tileUsers"
              :key="user.id"
              :user="user"
              :stream="isConnectedHere ? cameraStreamOf(user) : null"
              :is-self="user.id === authStore.user?.id"
              :speaking="voiceStore.isSpeaking(user.id)"
              :muted="voiceStore.muteStateOf(user.id).muted"
              :deafened="voiceStore.muteStateOf(user.id).deafened"
              :local-muted="voiceStore.isUserLocalMuted(user.id)"
              :camera-available="cameraAvailable(user)"
              :camera-hidden="voiceStore.isCameraHidden(user.id)"
              :is-screensharing="!!voiceStore.mediaState[user.id]?.screen"
              :is-watching="!!voiceStore.watchedScreens[user.id]"
              :is-connecting="voiceStore.remoteScreenUserId === user.id && !voiceStore.remoteScreenStream"
              :camera-focusable="canFocusCamera(user)"
              :camera-focused="isCameraFocused(user)"
              @toggle-camera="voiceStore.toggleCameraHidden(user.id)"
              @focus-camera="onTileFocusCamera(user)"
              @fullscreen-camera="fullscreenCamera(user.id)"
              @watch-stream="watchStream(user.id)"
              @stop-watching="voiceStore.unwatchScreen(user.id)"
              fill
              :compact="showChat || (gridSized && gridLayout.tileWidth < 300)"
              :show-status="isConnectedHere"
              :style="tileStyle"
              :class="gridSized ? 'flex-shrink-0' : 'w-56'"
              @open-profile="chatStore.openUserProfile"
              @menu="openMemberMenu($event, user)"
            />
          </div>
        </div>

        <!-- Everyone is hidden: nobody has video on -->
        <div
          v-else-if="hidingNoVideo && usersInVoice.length"
          data-testid="no-video-hint"
          class="flex-1 flex flex-col items-center justify-center text-center max-w-sm gap-1"
        >
          <VideoOff class="w-6 h-6 text-mnema-tertiary mb-1" aria-hidden="true" />
          <p class="font-semibold text-base text-mnema-text">{{ $t('talk.noVideoTitle') }}</p>
          <p class="text-sm text-mnema-tertiary">{{ $t('talk.noVideoHint') }}</p>
          <button
            type="button"
            class="mt-2 h-8 px-3 rounded-md text-sm font-medium border border-mnema-hairline bg-mnema-surface text-mnema-text hover:bg-mnema-hover transition"
            @click="voiceStore.setHideNoVideo(false)"
          >
            {{ $t('talk.showAllParticipants') }}
          </button>
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

          <!-- Stream audio mute toggle for streamer -->
          <button
            v-if="voiceStore.isScreenSharing"
            @click="voiceStore.toggleScreenAudioMute"
            :class="[
              'p-2.5 rounded-full transition-all',
              voiceStore.isScreenAudioMuted
                ? 'bg-mnema-danger/20 text-mnema-danger border border-mnema-danger/30'
                : 'bg-mnema-surface hover:bg-mnema-hover text-mnema-text'
            ]"
            :aria-pressed="voiceStore.isScreenAudioMuted ? 'true' : 'false'"
            v-tooltip="voiceStore.isScreenAudioMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
          >
            <VolumeX v-if="voiceStore.isScreenAudioMuted" class="w-4 h-4" />
            <Volume2 v-else class="w-4 h-4" />
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
                <button
                  type="button"
                  data-testid="author-name"
                  @click="chatStore.openUserProfile(msg)"
                  class="font-semibold text-message text-mnema-text hover:text-mnema-accent hover:underline transition-colors cursor-pointer truncate text-left focus-visible:underline focus-visible:text-mnema-accent"
                >
                  {{ msg.display_name || msg.username }}
                </button>
                <span class="text-xs text-mnema-tertiary flex-shrink-0 tabular-nums">{{ formatTime(msg.created_at) }}</span>
              </div>

              <MarkdownContent v-if="msg.content" :content="msg.content" />

              <!-- Attachments if any -->
              <MessageAttachments :attachments="msg.attachments" @open-image="selectedImage = $event" />
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
    <!-- Image lightbox -->
    <ImageLightbox v-if="selectedImage" :src="selectedImage" @close="selectedImage = null" />

    <ContextMenu
      v-model="menu.state.open"
      :x="menu.state.x"
      :y="menu.state.y"
      :anchor="menu.state.anchor"
      :items="menu.items.value"
    />
  </main>
</template>
