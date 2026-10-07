<script setup>
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import {
  Volume2, VolumeX, Monitor, MonitorOff, MessageSquare, Maximize2, Minimize2,
  Users, Video, VideoOff, X, UserRoundX, PictureInPicture2, ChevronDown, ChevronUp
} from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import ParticipantTile from './ParticipantTile.vue'
import TalkControlBar from './TalkControlBar.vue'
import TalkParticipants from './TalkParticipants.vue'
import ScreenViewers from './ScreenViewers.vue'
import StreamQualityMenu from './StreamQualityMenu.vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState, buildMemberItems } from '../composables/useNavMenus'
import VoiceTimer from './VoiceTimer.vue'
import { useTalkStage } from '../composables/useTalkStage'
import { useVideoGrid } from '../composables/useVideoGrid'
import { usePictureInPicture } from '../composables/usePictureInPicture'
import { useAutoHide } from '../composables/useAutoHide'
import { useElementSize } from '../composables/useElementSize'
import { loadStripCollapsed, saveStripCollapsed, fitAspect, COMPACT_BAR_WIDTH } from '../lib/talkLayout'
import { unreadBadge } from '../lib/voiceChatPanel'
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
// Which Talk is shown. Without a prop it is the one the user is in.
// Not connected to it, the stage is a preview: who is there and a Join
// button. No microphone is requested before the user joins.
// showChat: the Talk's chat (VoiceChatPanel, under the stage) is open; the
// header's chat button toggles it.
const props = defineProps({
  channelId: { type: String, default: null },
  showChat: { type: Boolean, default: false }
})
const emit = defineEmits(['join', 'update:showChat'])

const { joinVoiceChannel, stopScreenShare } = useWebRTC()

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

const videoContainer = ref(null)
const screenVideoEl = ref(null)
// Actual resolution of the shared screen as decoded by the browser.
const videoResolution = ref('')
// Its shape: the stage takes it (16:9 until the first frame is known).
const videoAspect = ref(16 / 9)
function onVideoResize() {
  const el = screenVideoEl.value
  videoResolution.value = el?.videoWidth ? `${el.videoWidth}×${el.videoHeight}` : ''
  if (el?.videoWidth && el.videoHeight) videoAspect.value = el.videoWidth / el.videoHeight
}

// Unread messages in this Talk's chat while it is closed (like Discord's
// badge on the chat button). The open chat marks them read.
const chatUnread = computed(() => {
  const id = shownChannelId.value
  return props.showChat || !id ? '' : unreadBadge(chatStore.readStates[id]?.unread_count)
})

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

// Audio controls on the stage: only for someone else's screen share. They
// set the volume of the stream's sound (0..100 %), never the person's voice.
const currentStreamVolume = computed(() => {
  const uid = stageUserId.value
  return uid ? voiceStore.streamVolumeShown(uid) : 0
})

const isCurrentStreamMuted = computed(() => {
  const uid = stageUserId.value
  return uid ? voiceStore.isStreamMuted(uid) : false
})

function toggleCurrentStreamMute() {
  const uid = stageUserId.value
  if (uid) voiceStore.toggleStreamMute(uid)
}

function onStreamVolumeChange(e) {
  const uid = stageUserId.value
  if (uid) {
    voiceStore.setStreamVolume(uid, Number(e.target.value))
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

// Picture-in-Picture: the stage plays in the browser's floating window (and
// on in a text channel). Meanwhile the stage itself decodes nothing.
const pip = usePictureInPicture()
const pipActive = computed(() => pip.active.value)

// Watch active screen stream and attach to video element
watch([activeScreenStream, isConnectedHere, ownPreviewPaused, pipActive], ([stream, , paused, inPip]) => {
  nextTick(() => {
    if (screenVideoEl.value) {
      screenVideoEl.value.srcObject = paused || inPip ? null : stream
    }
  })
}, { immediate: true })

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
  if (document.fullscreenElement || e.target?.closest?.('[data-testid="talk-controls"]')) return
  e.preventDefault()
  menu.show(e, () => {
    const items = [{
      id: 'fullscreen',
      label: t('talk.fullscreen'),
      icon: Maximize2,
      shortcut: 'F',
      action: () => enterFullscreen()
    }]
    if (pip.supported) {
      items.push({
        id: 'pip',
        label: pipActive.value ? t('talk.pipExit') : t('talk.pip'),
        icon: PictureInPicture2,
        action: () => pip.toggle()
      })
    }
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

// --- Layout: the stage takes the video's shape in the free area ---
// The stage, the share cards and the strip are one group, centred in the
// area: the stage gets what the others leave (no empty bands between them).
const area = ref(null) // the Talk area under the header (stage, strip, grid)
const cardsEl = ref(null)
const stripRegion = ref(null)
const areaSize = useElementSize(area)
const cardsSize = useElementSize(cardsEl)
const stripSize = useElementSize(stripRegion)
const rootSize = useElementSize(root)
const AREA_PAD_X = 16
const AREA_PAD_TOP = 16
const AREA_PAD_BOTTOM = 12
const AREA_GAP = 12
const hasStage = computed(() => !!activeScreenStream.value && isConnectedHere.value)
const stageBox = computed(() => {
  const a = areaSize.value
  let height = a.height - AREA_PAD_TOP - AREA_PAD_BOTTOM
  if (screenCards.value.length) height -= cardsSize.value.height + AREA_GAP
  if (tileUsers.value.length) height -= stripSize.value.height + AREA_GAP
  return fitAspect(a.width - 2 * AREA_PAD_X, height, videoAspect.value)
})
const stageStyle = computed(() => {
  const b = stageBox.value
  return isFullscreen.value || !b.width ? {} : { width: `${b.width}px`, height: `${b.height}px` }
})
// Narrow stage: fewer details in its overlays. Narrow header: no timer.
const stageNarrow = computed(() => !isFullscreen.value && stageBox.value.width > 0 && stageBox.value.width < 560)
// Tiny stage: no volume slider and no Picture-in-Picture button (its right-click menu has it).
const stageTiny = computed(() => stageNarrow.value && stageBox.value.width < 440)
const headerNarrow = computed(() => rootSize.value.width > 0 && rootSize.value.width < 560)
// Narrow Talk area: the control bar shows icons only (secondary ones in "more").
const compactBar = computed(() => {
  const w = isFullscreen.value ? window.innerWidth : areaSize.value.width
  return w > 0 && w < COMPACT_BAR_WIDTH
})

// The participant strip under a stream collapses (remembered per browser).
const stripCollapsed = ref(loadStripCollapsed())
function toggleStrip() {
  stripCollapsed.value = !stripCollapsed.value
  saveStripCollapsed(stripCollapsed.value)
}

// The floating controls (call bar, stage overlays) fade out after 3 s
// without activity in the Talk view; never in a preview, never while a menu
// from them is open or the pointer or keyboard focus is on them.
const barPinned = ref(false)
const overlayHover = ref(false)
const qualityMenuOpen = ref(false)
const controls = useAutoHide({
  enabled: () => isConnectedHere.value,
  pinned: () => barPinned.value || overlayHover.value || qualityMenuOpen.value || menu.state.open
})
const controlsShown = computed(() => controls.visible.value)
function onActivity() {
  controls.show()
}
function onOverlayPointer(e, on) {
  if (e.pointerType !== 'touch') overlayHover.value = on
}
const fade = computed(() => [
  'transition-opacity duration-200 motion-reduce:transition-none',
  controlsShown.value ? 'opacity-100' : 'opacity-0'
])

// Shared button looks: the header's square buttons, the stage's dark ones.
const focusRing = 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent'
function headerButton(active) {
  return [
    'relative w-8 h-8 flex items-center justify-center rounded-md border transition-colors motion-reduce:transition-none',
    focusRing,
    active
      ? 'border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent'
      : 'border-mnema-hairline bg-mnema-surface text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover'
  ]
}
const overlayButton = 'w-8 h-8 flex items-center justify-center rounded-lg text-white transition-colors motion-reduce:transition-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/80'

</script>

<template>
  <main
    ref="root"
    class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden select-none"
    @pointermove="onActivity"
    @pointerdown="onActivity"
    @focusin="onActivity"
  >
    <!-- Talk header -->
    <header class="h-12 px-4 border-b border-mnema-hairline bg-mnema-canvas flex items-center justify-between gap-3 flex-shrink-0 z-10">
      <div class="flex items-center gap-3 min-w-0">
        <div
          v-if="!headerNarrow"
          class="w-8 h-8 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint flex-shrink-0"
          aria-hidden="true"
        >
          <Volume2 class="w-4 h-4" />
        </div>
        <div class="flex items-center gap-2 min-w-0">
          <h2 class="font-semibold text-base leading-5 text-mnema-text truncate min-w-[3rem]">
            {{ activeVoiceChannel?.name || $t('voice.channelFallback') }}
          </h2>
          <TalkParticipants class="flex-shrink-0" :users="usersInVoice" :started-at="voiceStore.roomStartedAt[shownChannelId] || ''" />
          <VoiceTimer
            v-if="voiceStore.roomStartedAt[shownChannelId] && !headerNarrow"
            :since="voiceStore.roomStartedAt[shownChannelId]"
            data-testid="talk-timer"
            v-tooltip="$t('talk.runningForTip')"
            class="text-xs text-mnema-tertiary flex-shrink-0"
          />
        </div>
      </div>

      <!-- Top Right Actions -->
      <div class="flex items-center gap-2 flex-shrink-0">
        <!-- The Talk's chat (under the stage), with its unread count while closed -->
        <button
          type="button"
          data-testid="voice-chat-toggle"
          @click="emit('update:showChat', !showChat)"
          :class="headerButton(showChat)"
          v-tooltip.visual="showChat ? $t('talk.closeChat') : $t('talk.openChat')"
          :aria-label="chatUnread ? $t('talk.openChatUnread', { count: chatUnread }) : showChat ? $t('talk.closeChat') : $t('talk.openChat')"
          :aria-pressed="showChat ? 'true' : 'false'"
        >
          <MessageSquare class="w-4 h-4" />
          <span
            v-if="chatUnread"
            data-testid="voice-chat-unread"
            class="absolute -top-1.5 -right-1.5 min-w-[18px] h-[18px] px-1 rounded-full bg-mnema-danger text-white text-[11px] font-bold leading-[18px] text-center ring-2 ring-mnema-canvas"
            aria-hidden="true"
          >{{ chatUnread }}</span>
        </button>

        <!-- Only participants with video (per browser) -->
        <button
          v-if="isConnectedHere"
          type="button"
          data-testid="hide-no-video"
          @click="voiceStore.toggleHideNoVideo()"
          :class="headerButton(voiceStore.hideNoVideo)"
          v-tooltip="$t('talk.hideNoVideo')"
          :aria-pressed="voiceStore.hideNoVideo ? 'true' : 'false'"
        >
          <UserRoundX class="w-4 h-4" />
        </button>

        <!-- Toggle Member List Sidebar -->
        <button
          type="button"
          @click="chatStore.showMemberList = !chatStore.showMemberList"
          :class="headerButton(chatStore.showMemberList)"
          v-tooltip="$t('members.toggle')"
          :aria-pressed="chatStore.showMemberList ? 'true' : 'false'"
        >
          <Users class="w-4 h-4" />
        </button>
      </div>
    </header>

    <!-- The Talk area: the stage with its strip, or the grid; the floating
         controls belong to it (never over the chat panel below). -->
    <div
      ref="area"
      data-testid="talk-area"
      :class="[
        'flex-1 min-h-0 relative flex flex-col bg-gradient-to-b from-mnema-raised/40 to-transparent'
      ]"
      @dblclick="onAreaDblclick"
    >
      <div
        :class="[
          'flex-1 min-h-0 flex flex-col items-center gap-3 px-4 pt-4',
          hasStage ? 'pb-3 justify-center' : !isConnectedHere && usersInVoice.length ? 'pb-28' : isConnectedHere ? 'pb-20' : 'pb-4'
        ]"
      >
        <!-- The stage: one screen share (mine or one I watch) or one camera,
             as large as the free space allows in the video's own shape -->
        <div
          v-if="hasStage"
          :class="['w-full flex justify-center flex-shrink-0', stageBox.width ? '' : 'flex-1 min-h-0']"
          :style="stageBox.height ? { height: `${stageBox.height}px` } : null"
        >
          <div
            ref="videoContainer"
            data-testid="stage"
            :data-stage-source="stageSource"
            :data-fullscreen="isFullscreen ? 'true' : undefined"
            :style="stageStyle"
            @contextmenu="openStageMenu"
            :class="[
              'bg-black relative overflow-hidden flex items-center justify-center shadow-2xl shadow-black/40',
              isFullscreen ? '' : 'rounded-xl ring-1 ring-mnema-border',
              stageBox.width ? '' : 'w-full h-full',
              controlsShown ? '' : 'cursor-none'
            ]"
          >
            <video
              ref="screenVideoEl"
              autoplay
              playsinline
              :muted="true"
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

            <!-- Playing in the Picture-in-Picture window -->
            <div
              v-else-if="pipActive"
              data-testid="stage-in-pip"
              class="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-mnema-canvas/95 text-center px-6"
            >
              <PictureInPicture2 class="w-7 h-7 text-mnema-tertiary" aria-hidden="true" />
              <span class="text-base font-semibold text-mnema-text">{{ $t('talk.pipPlaying') }}</span>
              <button
                type="button"
                :class="['mt-1 h-8 px-3 rounded-md text-sm font-medium border border-mnema-hairline bg-mnema-surface text-mnema-text hover:bg-mnema-hover transition-colors', focusRing]"
                @click="pip.exit()"
              >
                {{ $t('talk.pipBack') }}
              </button>
            </div>

            <!-- Top: who is on the stage, and its controls (fade with the call bar) -->
            <div :class="['absolute top-3 inset-x-3 flex items-start justify-between gap-2 z-20 pointer-events-none', fade]">
              <div class="min-w-0 overflow-hidden pointer-events-auto bg-black/80 border border-white/10 h-8 px-2.5 rounded-lg flex items-center gap-2 text-sm text-white">
                <Video v-if="cameraOnStage" class="w-3.5 h-3.5 text-white/80 flex-shrink-0" />
                <span v-else class="px-1.5 py-0.5 rounded bg-red-600 text-white text-[10px] font-black uppercase tracking-wider flex items-center gap-1 flex-shrink-0">
                  <span class="w-1.5 h-1.5 rounded-full bg-white animate-pulse motion-reduce:animate-none"></span>
                  {{ $t('talk.live') }}
                </span>
                <ScreenViewers v-if="!cameraOnStage && stage" :user-id="stage.userId" :show-zero="ownOnStage" class="-mx-1 flex-shrink-0" />
                <span v-if="!stageTiny" class="font-semibold text-xs truncate">{{ stageName }}</span>
                <span v-if="videoResolution && !stageNarrow" class="text-white/60 text-xs font-mono flex-shrink-0">{{ videoResolution }}</span>
              </div>

              <div
                class="flex items-center gap-1.5 flex-shrink-0 pointer-events-auto"
                @pointerenter="onOverlayPointer($event, true)"
                @pointerleave="onOverlayPointer($event, false)"
              >
                <!-- Stream Audio Toggle for Streamer (Own Screen) -->
                <button
                  v-if="ownOnStage"
                  type="button"
                  data-testid="streamer-audio-toggle"
                  :class="[overlayButton, voiceStore.isScreenAudioMuted ? 'bg-mnema-danger/80 hover:bg-mnema-danger' : 'bg-black/75 hover:bg-black/90']"
                  v-tooltip="voiceStore.isScreenAudioMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
                  @click="voiceStore.toggleScreenAudioMute"
                >
                  <VolumeX v-if="voiceStore.isScreenAudioMuted" class="w-4 h-4" />
                  <Volume2 v-else class="w-4 h-4" />
                </button>
                <!-- Stream quality of my own share (its menu cannot show in full screen) -->
                <StreamQualityMenu v-if="ownOnStage && !isFullscreen" @open-change="qualityMenuOpen = $event" />

                <!-- Viewer Stream Audio Controls (Volume & Mute) -->
                <div
                  v-else-if="stageUserId"
                  class="h-8 flex items-center gap-1.5 bg-black/75 hover:bg-black/90 px-2 rounded-lg text-white group/vol"
                >
                  <button
                    type="button"
                    data-testid="viewer-stream-audio-mute"
                    class="p-0.5 rounded text-white hover:text-mnema-accent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/80"
                    :aria-label="isCurrentStreamMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
                    :aria-pressed="isCurrentStreamMuted ? 'true' : 'false'"
                    v-tooltip="isCurrentStreamMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
                    @click="toggleCurrentStreamMute"
                  >
                    <VolumeX v-if="isCurrentStreamMuted" class="w-4 h-4 text-mnema-danger" />
                    <Volume2 v-else class="w-4 h-4" />
                  </button>
                  <input
                    type="range"
                    min="0"
                    max="100"
                    data-testid="viewer-stream-volume-slider"
                    :aria-label="$t('talk.streamVolume')"
                    :aria-valuetext="`${currentStreamVolume}%`"
                    :value="currentStreamVolume"
                    v-if="!stageTiny"
                    :class="['h-1 accent-mnema-accent cursor-pointer opacity-80 group-hover/vol:opacity-100 transition-opacity', stageNarrow ? 'w-12' : 'w-16']"
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
                  :class="[overlayButton, 'bg-black/75 hover:bg-black/90']"
                  @click="voiceStore.unfocusCamera()"
                >
                  <X class="w-4 h-4" />
                </button>
                <!-- Stop Watching (Viewer) -->
                <button
                  v-else-if="!ownOnStage"
                  type="button"
                  @click="voiceStore.unwatchScreen(stageUserId)"
                  :aria-label="$t('talk.unwatchScreen')"
                  v-tooltip="$t('talk.stopWatching')"
                  :class="[overlayButton, 'bg-black/75 hover:bg-black/90 hover:text-mnema-danger']"
                >
                  <X class="w-4 h-4" />
                </button>
                <!-- Stop Sharing (Streamer) -->
                <button
                  v-else
                  type="button"
                  @click="stopScreenShare"
                  v-tooltip="$t('voice.stopShare')"
                  :class="[overlayButton, 'bg-mnema-danger/80 hover:bg-mnema-danger']"
                >
                  <MonitorOff class="w-4 h-4" />
                </button>

                <!-- Picture-in-Picture (only where the browser has it) -->
                <button
                  v-if="pip.supported && !stageTiny"
                  type="button"
                  data-testid="stage-pip"
                  @click="pip.toggle()"
                  v-tooltip="pipActive ? $t('talk.pipExit') : $t('talk.pip')"
                  :aria-pressed="pipActive ? 'true' : 'false'"
                  :class="[overlayButton, pipActive ? 'bg-mnema-accent/80 hover:bg-mnema-accent' : 'bg-black/75 hover:bg-black/90']"
                >
                  <PictureInPicture2 class="w-4 h-4" />
                </button>

                <!-- Fullscreen (also a double-click on the stage or F) -->
                <button
                  type="button"
                  data-testid="stage-fullscreen"
                  @click="toggleFullscreen"
                  v-tooltip="{ text: isFullscreen ? $t('talk.exitFullscreen') : $t('talk.fullscreen'), shortcut: 'F' }"
                  :aria-pressed="isFullscreen ? 'true' : 'false'"
                  :class="[overlayButton, 'bg-black/75 hover:bg-black/90']"
                >
                  <Minimize2 v-if="isFullscreen" class="w-4 h-4" />
                  <Maximize2 v-else class="w-4 h-4" />
                </button>
              </div>
            </div>

            <!-- The call controls float over the stage (in full screen too) -->
            <div class="absolute inset-x-0 bottom-3 px-3 flex justify-center pointer-events-none z-20">
              <TalkControlBar
                class="pointer-events-auto"
                :visible="controlsShown"
                :compact="compactBar"
                :fullscreen="isFullscreen"
                @update:pinned="barPinned = $event"
              />
            </div>
          </div>
        </div>

        <!-- Screen shares not on the stage: mine, ones I watch, ones I can opt into -->
        <div
          v-if="screenCards.length"
          ref="cardsEl"
          :class="['w-full flex flex-wrap justify-center gap-2 flex-shrink-0', hasStage ? '' : 'max-w-5xl']"
        >
          <div
            v-for="card in screenCards"
            :key="card.key"
            data-testid="screen-card"
            :data-screen-card="card.key"
            class="flex items-center gap-3 min-w-0 max-w-full rounded-lg border border-mnema-accent/30 bg-mnema-accent-subtle pl-3 pr-1.5 py-1.5"
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
              :class="['h-7 px-3 rounded-md text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition-colors disabled:opacity-60 flex-shrink-0', focusRing, 'focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-accent-subtle']"
              @click="onScreenCard(card)"
            >
              {{ card.state === 'idle' ? $t('talk.watchScreen') : card.state === 'queued' ? $t('talk.toStage') : $t('talk.screenConnecting') }}
            </button>
            <StreamQualityMenu v-if="card.kind === 'own'" variant="card" @open-change="qualityMenuOpen = $event" />
            <button
              v-if="card.kind === 'remote' && card.state !== 'idle'"
              type="button"
              :class="['w-7 h-7 flex items-center justify-center rounded-md text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition-colors flex-shrink-0', focusRing]"
              v-tooltip="$t('talk.unwatchScreen')"
              @click="voiceStore.unwatchScreen(card.user.id)"
            >
              <X class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- Participants under the stage: a strip that collapses (like
             Discord's "hide members"); the stage then takes the room -->
        <div
          v-if="hasStage && tileUsers.length"
          ref="stripRegion"
          data-testid="talk-strip-region"
          class="w-full flex-shrink-0 flex flex-col items-center"
        >
          <button
            type="button"
            data-testid="talk-strip-toggle"
            :aria-expanded="stripCollapsed ? 'false' : 'true'"
            aria-controls="talk-strip"
            v-tooltip="stripCollapsed ? $t('talk.showParticipants') : $t('talk.hideParticipants')"
            :class="['h-6 px-2 flex items-center gap-1 rounded-full text-xs font-semibold text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition-colors motion-reduce:transition-none', focusRing]"
            @click="toggleStrip"
          >
            <ChevronUp v-if="stripCollapsed" class="w-4 h-4" aria-hidden="true" />
            <ChevronDown v-else class="w-4 h-4" aria-hidden="true" />
            <template v-if="stripCollapsed">
              <Users class="w-3.5 h-3.5" aria-hidden="true" />
              <span>{{ tileUsers.length }}</span>
            </template>
          </button>
          <div
            :class="['w-full grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none', stripCollapsed ? 'grid-rows-[0fr]' : 'grid-rows-[1fr]']"
          >
            <div class="min-h-0 overflow-hidden">
              <div
                id="talk-strip"
                data-testid="talk-strip"
                :data-collapsed="stripCollapsed ? 'true' : 'false'"
                :inert="stripCollapsed"
                class="w-full overflow-x-auto pt-1.5 pb-1"
              >
                <div class="flex gap-2 w-max mx-auto px-0.5">
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
                    class="flex-shrink-0"
                    @open-profile="chatStore.openUserProfile"
                    @menu="openMemberMenu($event, user)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- The grid: every participant as a 16:9 tile, as large as the area allows -->
        <div
          v-else-if="!hasStage && tileUsers.length"
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
              :compact="gridSized && gridLayout.tileWidth < 300"
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
          v-else-if="!hasStage && hidingNoVideo && usersInVoice.length"
          data-testid="no-video-hint"
          class="flex-1 flex flex-col items-center justify-center text-center max-w-sm gap-1"
        >
          <div class="w-12 h-12 mb-2 rounded-full bg-mnema-surface border border-mnema-hairline flex items-center justify-center" aria-hidden="true">
            <VideoOff class="w-5 h-5 text-mnema-tertiary" />
          </div>
          <p class="font-semibold text-base text-mnema-text">{{ $t('talk.noVideoTitle') }}</p>
          <p class="text-sm text-mnema-tertiary">{{ $t('talk.noVideoHint') }}</p>
          <button
            type="button"
            :class="['mt-3 h-8 px-3 rounded-md text-sm font-medium border border-mnema-hairline bg-mnema-surface text-mnema-text hover:bg-mnema-hover transition-colors', focusRing]"
            @click="voiceStore.setHideNoVideo(false)"
          >
            {{ $t('talk.showAllParticipants') }}
          </button>
        </div>

        <!-- Nobody there yet (preview): what to do, right where one looks -->
        <div
          v-else-if="!hasStage"
          data-testid="talk-empty"
          class="flex-1 flex flex-col items-center justify-center text-center max-w-sm gap-1"
        >
          <div class="w-14 h-14 mb-3 rounded-full bg-mnema-band border border-mnema-mint/30 flex items-center justify-center" aria-hidden="true">
            <Volume2 class="w-6 h-6 text-mnema-mint" />
          </div>
          <p class="font-semibold text-base text-mnema-text">{{ $t('voice.noOneInVoice') }}</p>
          <p class="text-sm text-mnema-tertiary">{{ $t('voice.joinToTalk') }}</p>
          <button
            v-if="!isConnectedHere"
            type="button"
            @click="join"
            :disabled="!shownChannelId"
            :class="['mt-4 flex items-center gap-2 h-10 px-6 rounded-full text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition-colors shadow-lg disabled:opacity-40', focusRing, 'focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-canvas']"
          >
            <Volume2 class="w-4 h-4" />
            <span>{{ $t('voice.join') }}</span>
          </button>
        </div>
      </div>

      <!-- Preview with people there: join at the bottom -->
      <div
        v-if="!isConnectedHere && usersInVoice.length"
        class="absolute inset-x-0 bottom-4 px-4 flex flex-col items-center gap-2 z-20"
      >
        <p class="text-xs text-mnema-tertiary text-center max-w-md">{{ $t('talk.previewHint') }}</p>
        <button
          type="button"
          @click="join"
          :disabled="!shownChannelId"
          :class="['flex items-center gap-2 h-10 px-6 rounded-full text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition-colors shadow-xl disabled:opacity-40', focusRing, 'focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-canvas']"
        >
          <Volume2 class="w-4 h-4" />
          <span>{{ $t('voice.join') }}</span>
        </button>
      </div>

      <!-- Connected, no stage: the call controls float at the bottom -->
      <div
        v-else-if="isConnectedHere && !hasStage"
        class="absolute inset-x-0 bottom-3 px-3 flex justify-center pointer-events-none z-20"
      >
        <TalkControlBar
          class="pointer-events-auto"
          :visible="controlsShown"
          :compact="compactBar"
          @update:pinned="barPinned = $event"
        />
      </div>
    </div>

    <ContextMenu
      v-model="menu.state.open"
      :x="menu.state.x"
      :y="menu.state.y"
      :anchor="menu.state.anchor"
      :items="menu.items.value"
    />
  </main>
</template>
