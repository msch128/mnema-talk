<script setup lang="ts">
import type { PropType } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import type { TalkParticipant } from './presentationTypes'
import { computed, ref } from 'vue'
import { Volume2, MonitorOff, Maximize2, X, PictureInPicture2 } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import TalkRoster from './TalkRoster.vue'
import TalkPlaceholder from './TalkPlaceholder.vue'
import TalkHeader from './TalkHeader.vue'
import TalkStageView from './TalkStageView.vue'
import TalkScreenCards from './TalkScreenCards.vue'
import TalkStrip from './TalkStrip.vue'
import TalkControlBar from './TalkControlBar.vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState, buildMemberItems } from '../composables/useNavMenus'
import { useTalkStage, orderParticipants } from '../composables/useTalkStage'
import { usePictureInPicture } from '../composables/usePictureInPicture'
import { useTalkControls } from '../composables/useTalkControls'
import { useElementSize } from '../composables/useElementSize'
import { useTalkJoin } from '../composables/useTalkJoin'
import { useStageFullscreen } from '../composables/useStageFullscreen'
import { useVisibleCameras } from '../composables/useVisibleCameras'
import { COMPACT_BAR_WIDTH } from '../lib/talkLayout'
import { focusRing } from './talkButtons'
import { unreadBadge } from '../lib/voiceChatPanel'
import { t } from '../i18n'

const voiceStore = useVoiceStore()

// Right-click on a tile: the member menu with their volume (like the sidebar).
const menu = useMenuState()
function openMemberMenu(e: MouseEvent | KeyboardEvent, user: TalkParticipant) {
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
  channelId: { type: String as PropType<string | null>, default: null },
  showChat: { type: Boolean, default: false }
})
const emit = defineEmits<{ join: [id: string]; 'update:showChat': [show: boolean] }>()

const { stopScreenShare } = useWebRTC()
const { shownChannelId, isConnectedHere, join } = useTalkJoin(() => props.channelId, id => emit('join', id))

const stageView = ref<InstanceType<typeof TalkStageView> | null>(null)

// Unread messages in this Talk's chat while it is closed (like Discord's
// badge on the chat button). The open chat marks them read.
const chatUnread = computed(() => {
  const id = shownChannelId.value
  return props.showChat || !id ? '' : unreadBadge(chatStore.readStates[id]?.unread_count)
})

const roomStartedAt = computed(() => {
  const id = shownChannelId.value
  return id ? voiceStore.roomStartedAt[id] ?? '' : ''
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
  const list: TalkParticipant[] = orderParticipants(Object.values(voiceStore.channelUsers[id] || {}))

  // Ensure the current user is always included visually while connected
  if (isConnectedHere.value && authStore.user && !list.some(u => u.id === authStore.user?.id)) {
    list.unshift(authStore.user)
  }
  return list
})

// Camera stream of a participant: my own, or the one the SFU forwards.
function cameraStreamOf(user: TalkParticipant) {
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
  myId: () => authStore.user?.id ?? null,
  active: () => isConnectedHere.value,
  watch: userId => watchStream(userId)
})

async function watchStream(userId: string) {
  if (!isConnectedHere.value && !(await join())) return
  voiceStore.watchScreen(userId)
}

// "Hide participants without video" (Discord's "only videos"): only tiles
// with a camera I receive or a screen share, mine included.
function hasVideo(user: TalkParticipant) {
  if (user.id === authStore.user?.id) return !!voiceStore.localCameraStream || voiceStore.isScreenSharing
  const media = voiceStore.mediaState[user.id]
  return !!media?.screen || (!!media?.camera && !voiceStore.isCameraHidden(user.id))
}
const hidingNoVideo = computed(() => isConnectedHere.value && voiceStore.hideNoVideo)
const tileUsers = computed(() => (hidingNoVideo.value ? usersInVoice.value.filter(hasVideo) : usersInVoice.value))

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

// Picture-in-Picture: the stage plays in the browser's floating window (and
// on in a text channel).
const pip = usePictureInPicture()
const pipActive = computed(() => pip.active.value)

const root = ref<HTMLElement | null>(null)
useVisibleCameras(root)
const { isFullscreen, enterFullscreen, toggleFullscreen, onTileFocusCamera, fullscreenCamera, onAreaDblclick } = useStageFullscreen({
  container: () => stageView.value?.container ?? null,
  root,
  connected: () => isConnectedHere.value,
  stage: () => stage.value,
  toggleCameraFocus
})

// Right-click on the stage: its actions as a menu (outside full screen,
// where the page's menus cannot show).
function shouldIgnoreStageMenu(e: MouseEvent): boolean {
  if (document.fullscreenElement) return true
  return e.target instanceof Element && Boolean(e.target.closest('[data-testid="talk-controls"]'))
}

function getStageStreamActionItem(): ContextMenuItem | null {
  if (cameraOnStage.value) {
    return { id: 'unfocus', label: t('talk.unfocusCamera'), icon: X, action: () => voiceStore.unfocusCamera() }
  }
  if (ownOnStage.value) {
    return { id: 'stop-share', label: t('voice.stopShare'), icon: MonitorOff, danger: true, action: () => stopScreenShare() }
  }
  if (stageUserId.value) {
    const uid = stageUserId.value
    return { id: 'unwatch', label: t('talk.stopWatching'), icon: X, action: () => voiceStore.unwatchScreen(uid) }
  }
  return null
}

function buildStageMenuItems(): ContextMenuItem[] {
  const items: ContextMenuItem[] = [{
    id: 'fullscreen', label: t('talk.fullscreen'), icon: Maximize2, shortcut: 'F', action: () => enterFullscreen()
  }]
  if (pip.supported) {
    items.push({
      id: 'pip', label: pipActive.value ? t('talk.pipExit') : t('talk.pip'), icon: PictureInPicture2, action: () => pip.toggle()
    })
  }
  const streamItem = getStageStreamActionItem()
  if (streamItem) items.push(streamItem)
  return items
}

function openStageMenu(e: MouseEvent) {
  if (shouldIgnoreStageMenu(e)) return
  e.preventDefault()
  menu.show(e, buildStageMenuItems)
}

// --- Layout: the stage takes the video's shape in the free area ---
// The stage, the share cards and the strip are one group, centred in the
// area: the stage gets what the others leave (no empty bands between them).
const area = ref<HTMLElement | null>(null) // the Talk area under the header (stage, strip, grid)
const cardsEl = ref<HTMLElement | null>(null)
const stripRegion = ref<HTMLElement | null>(null)
const areaSize = useElementSize(area)
const cardsSize = useElementSize(cardsEl)
const stripSize = useElementSize(stripRegion)
const rootSize = useElementSize(root)
const [AREA_PAD_X, AREA_PAD_TOP, AREA_PAD_BOTTOM, AREA_GAP] = [16, 16, 12, 12]
const hasStage = computed(() => !!activeScreenStream.value && isConnectedHere.value)
const stageSpace = computed(() => {
  const a = areaSize.value
  let height = a.height - AREA_PAD_TOP - AREA_PAD_BOTTOM
  if (screenCards.value.length) height -= cardsSize.value.height + AREA_GAP
  if (tileUsers.value.length) height -= stripSize.value.height + AREA_GAP
  return { width: a.width - 2 * AREA_PAD_X, height }
})
// Narrow header: no timer.
const headerNarrow = computed(() => rootSize.value.width > 0 && rootSize.value.width < 560)
// Narrow Talk area: the control bar shows icons only (secondary ones in "more").
const compactBar = computed(() => {
  const w = isFullscreen.value ? window.innerWidth : areaSize.value.width
  return w > 0 && w < COMPACT_BAR_WIDTH
})

const { barPinned, overlayHover, qualityMenuOpen, controlsShown, showTapHint, showMoveHint, onActivity, onTapStart, onTapCapture } = useTalkControls({
  connected: isConnectedHere,
  others: () => usersInVoice.value.length,
  menuOpen: () => menu.state.open
})
</script>

<template>
  <main
    id="main-content"
    tabindex="-1"
    ref="root"
    class="flex-1 min-w-0 bg-mnema-canvas flex flex-col h-full overflow-hidden select-none focus:outline-hidden"
    @pointermove="onActivity"
    @pointerdown.capture="onTapStart"
    @click.capture="onTapCapture"
    @pointerdown="onActivity"
    @focusin="onActivity"
  >
    <TalkHeader
      :channel-name="activeVoiceChannel?.name || ''" :channel="activeVoiceChannel ?? null"
      :users="usersInVoice"
      :started-at="roomStartedAt"
      :narrow="headerNarrow"
      :connected="isConnectedHere"
      :show-chat="showChat"
      :chat-unread="chatUnread"
      :can-join="!!shownChannelId"
      @join="join"
      @update:show-chat="emit('update:showChat', $event)"
    />

    <!-- The Talk area: the stage with its strip, or the grid; the floating
         controls belong to it (never over the chat panel below). -->
    <div
      ref="area"
      data-testid="talk-area"
      class="flex-1 min-h-0 relative flex flex-col bg-linear-to-b from-mnema-raised/40 to-transparent"
      @dblclick="onAreaDblclick"
    >
      <div
        :class="[
          'flex-1 min-h-0 flex flex-col items-center gap-3 px-4 pt-4',
          hasStage || (showChat && isConnectedHere) ? 'pb-3' : !isConnectedHere && usersInVoice.length ? 'pb-28' : isConnectedHere ? 'pb-20' : 'pb-4',
          hasStage ? 'justify-center' : ''
        ]"
      >
        <TalkStageView
          v-if="hasStage && stage"
          ref="stageView"
          :stage="stage"
          :space="stageSpace"
          :stage-name="stageName"
          :stage-source="stageSource"
          :camera-on-stage="cameraOnStage"
          :own-on-stage="ownOnStage"
          :stage-user-id="stageUserId"
          :fullscreen="isFullscreen"
          :controls-shown="controlsShown"
          :compact-bar="compactBar"
          :pip="pip"
          @toggle-fullscreen="toggleFullscreen"
          @contextmenu="openStageMenu"
          @overlay-hover="overlayHover = $event"
          @quality-open="qualityMenuOpen = $event"
          @bar-pinned="barPinned = $event"
        />

        <!-- Screen shares not on the stage: mine, ones I watch, ones I can opt into -->
        <div
          v-if="screenCards.length"
          ref="cardsEl"
          :class="['w-full flex flex-wrap justify-center gap-2 shrink-0', hasStage ? '' : 'max-w-5xl']"
        >
          <TalkScreenCards :cards="screenCards" @select="onScreenCard" @open-change="qualityMenuOpen = $event" />
        </div>

        <!-- Participants under the stage: a strip that collapses (like
             Discord's "hide members"); the stage then takes the room -->
        <div
          v-if="hasStage && tileUsers.length"
          ref="stripRegion"
          data-testid="talk-strip-region"
          class="w-full shrink-0 flex flex-col items-center"
        >
          <TalkStrip
            :users="tileUsers"
            :stream-of="cameraStreamOf"
            :can-focus-camera="canFocusCamera"
            :is-camera-focused="isCameraFocused"
            @focus-camera="onTileFocusCamera"
            @fullscreen-camera="fullscreenCamera"
            @watch-stream="watchStream"
            @menu="openMemberMenu"
          />
        </div>

        <TalkRoster
          v-else-if="!hasStage && tileUsers.length"
          :channel-id="shownChannelId ?? ''"
          :users="tileUsers"
          :connected="isConnectedHere"
          :stream-of="cameraStreamOf"
          :can-focus-camera="canFocusCamera"
          :is-camera-focused="isCameraFocused"
          @focus-camera="onTileFocusCamera"
          @fullscreen-camera="fullscreenCamera"
          @watch-stream="watchStream"
          @menu="openMemberMenu"
        />
        <TalkPlaceholder
          v-else-if="!hasStage"
          :variant="hidingNoVideo && usersInVoice.length ? 'no-video' : 'empty'"
          :connected="isConnectedHere"
          :can-join="!!shownChannelId"
          @join="join"
        />
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

      <!-- Connected, no stage: the call controls float at the bottom, or with
           the chat open sit in a row of their own between tiles and chat -->
      <div
        v-else-if="isConnectedHere && !hasStage"
        :class="['px-3 flex justify-center', showChat ? 'relative shrink-0 pb-3' : 'absolute inset-x-0 bottom-3 pointer-events-none z-20']"
      >
        <TalkControlBar
          class="pointer-events-auto"
          :visible="controlsShown"
          :compact="compactBar"
          @update:pinned="barPinned = $event"
        />
      </div>

      <p
        v-if="showTapHint || showMoveHint"
        data-testid="talk-tap-hint"
        class="pointer-events-none absolute inset-x-0 bottom-2 z-10 text-center text-xs text-mnema-tertiary"
      >{{ showTapHint ? $t('talk.tapForControls') : $t('talk.moveForControls') }}</p>
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
