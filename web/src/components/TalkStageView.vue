<script setup lang="ts">
import type { PropType } from 'vue'
import type { VoiceStore } from '../stores/voice'
import type { usePictureInPicture } from '../composables/usePictureInPicture'
import { computed, ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { Maximize2, Minimize2, MonitorOff, PictureInPicture2, Video, Volume2, VolumeX, X } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { fitAspect } from '../lib/talkLayout'
import { useScreenFit } from '../composables/useScreenFit'
import { useCameraFit } from '../composables/useCameraFit'
import ScreenViewers from './ScreenViewers.vue'
import StreamQualityMenu from './StreamQualityMenu.vue'
import ScreenLayerMenu from './ScreenLayerMenu.vue'
import TalkControlBar from './TalkControlBar.vue'
import { focusRing, overlayButton } from './talkButtons'

// The stage: one screen share (mine or one I watch) or one camera, as large
// as the free space allows in the video's own shape, with who is on it, its
// controls and the call bar floating over it (in full screen too).
const props = defineProps({
  stage: { type: Object as PropType<NonNullable<VoiceStore['stage']>>, required: true },
  // The room the stage may take, in pixels (the Talk area minus cards and strip).
  space: { type: Object as PropType<{ width: number; height: number }>, required: true },
  stageName: { type: String, default: '' },
  stageSource: { type: String as PropType<string | null>, default: null },
  cameraOnStage: { type: Boolean, default: false },
  ownOnStage: { type: Boolean, default: false },
  stageUserId: { type: String as PropType<string | null>, default: null },
  fullscreen: { type: Boolean, default: false },
  controlsShown: { type: Boolean, default: true },
  compactBar: { type: Boolean, default: false },
  pip: { type: Object as PropType<ReturnType<typeof usePictureInPicture>>, required: true }
})
const emit = defineEmits<{
  'toggle-fullscreen': []
  contextmenu: [event: MouseEvent]
  'overlay-hover': [on: boolean]
  'quality-open': [open: boolean]
  'bar-pinned': [pinned: boolean]
}>()

const voiceStore = useVoiceStore()
const { stopScreenShare } = useWebRTC()

const videoContainer = ref<HTMLElement | null>(null)
const screenVideoEl = ref<HTMLVideoElement | null>(null)
defineExpose({ container: videoContainer })
useScreenFit(() => (props.ownOnStage || props.cameraOnStage ? null : props.stageUserId), videoContainer)
useCameraFit(() => (props.cameraOnStage && !props.ownOnStage ? props.stageUserId : null), screenVideoEl, 'stage')

// Actual resolution of the shared screen as decoded by the browser.
const videoResolution = ref('')
// Its shape: the stage takes it (16:9 until the first frame is known).
const videoAspect = ref(16 / 9)
function onVideoResize() {
  const el = screenVideoEl.value
  videoResolution.value = el?.videoWidth ? `${el.videoWidth}×${el.videoHeight}` : ''
  if (el?.videoWidth && el.videoHeight) videoAspect.value = el.videoWidth / el.videoHeight
}

const box = computed(() => fitAspect(props.space.width, props.space.height, videoAspect.value))
const boxStyle = computed(() => props.fullscreen || !box.value.width ? {} : { width: `${box.value.width}px`, height: `${box.value.height}px` })
// Narrow stage: fewer details in its overlays.
const narrow = computed(() => !props.fullscreen && box.value.width > 0 && box.value.width < 560)
// Tiny stage: no volume slider and no Picture-in-Picture button (its right-click menu has it).
const tiny = computed(() => narrow.value && box.value.width < 440)
const fade = computed(() => [
  'transition-opacity duration-200 motion-reduce:transition-none',
  props.controlsShown ? 'opacity-100' : 'opacity-0'
])

// My own share is only previewed while I look at this tab: decoding my own
// screen costs CPU/GPU while I'm busy elsewhere, and the stream keeps going.
function isPageActive() {
  if (typeof document === 'undefined') return true
  return document.visibilityState === 'visible' && (typeof document.hasFocus !== 'function' || document.hasFocus())
}
const pageActive = ref(isPageActive())
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
const ownPreviewPaused = computed(() => props.ownOnStage && !pageActive.value)

// Picture-in-Picture: the stage plays in the browser's floating window, and
// meanwhile decodes nothing itself.
const pipActive = computed(() => props.pip.active.value)
watch([() => props.stage.stream, ownPreviewPaused, pipActive], ([stream, paused, inPip]) => {
  nextTick(() => {
    if (screenVideoEl.value) {
      screenVideoEl.value.srcObject = paused || inPip ? null : stream
    }
  })
}, { immediate: true })

// Audio controls on the stage: only for someone else's screen share. They
// set the volume of the stream's sound (0..100 %), never the person's voice.
const currentStreamVolume = computed(() => (props.stageUserId ? voiceStore.streamVolumeShown(props.stageUserId) : 0))
const isCurrentStreamMuted = computed(() => (props.stageUserId ? voiceStore.isStreamMuted(props.stageUserId) : false))
function toggleCurrentStreamMute() {
  if (props.stageUserId) voiceStore.toggleStreamMute(props.stageUserId)
}
function onStreamVolumeChange(e: Event) {
  if (props.stageUserId) voiceStore.setStreamVolume(props.stageUserId, Number((e.target as HTMLInputElement).value))
}
function onOverlayPointer(e: PointerEvent, on: boolean) {
  if (e.pointerType !== 'touch') emit('overlay-hover', on)
}
</script>

<template>
  <div
    :class="['w-full flex justify-center shrink-0', box.width ? '' : 'flex-1 min-h-0']"
    :style="box.height ? { height: `${box.height}px` } : null"
  >
    <div
      ref="videoContainer"
      data-testid="stage"
      :data-stage-source="stageSource"
      :data-fullscreen="fullscreen ? 'true' : undefined"
      :style="boxStyle"
      @contextmenu="emit('contextmenu', $event)"
      :class="[
        'bg-black relative overflow-hidden flex items-center justify-center shadow-2xl shadow-black/40',
        fullscreen ? '' : 'rounded-xl ring-1 ring-mnema-border',
        box.width ? '' : 'w-full h-full',
        controlsShown ? '' : 'cursor-none'
      ]"
    >
      <video
        ref="screenVideoEl"
        autoplay
        playsinline
        :muted="true"
        :class="['w-full h-full object-contain', stage.kind === 'camera' && stage.own ? '-scale-x-100' : '']"
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
          <Video v-if="cameraOnStage" class="w-3.5 h-3.5 text-white/80 shrink-0" />
          <span v-else class="px-1.5 py-0.5 rounded-sm bg-red-600 text-white text-[10px] font-black uppercase tracking-wider flex items-center gap-1 shrink-0">
            <span class="w-1.5 h-1.5 rounded-full bg-white animate-pulse motion-reduce:animate-none"></span>
            {{ $t('talk.live') }}
          </span>
          <ScreenViewers v-if="!cameraOnStage && stage.userId" :user-id="stage.userId" :show-zero="ownOnStage" class="-mx-1 shrink-0" />
          <span v-if="!tiny" class="font-semibold text-xs truncate">{{ stageName }}</span>
          <span v-if="videoResolution && !narrow" class="text-white/60 text-xs font-mono shrink-0">{{ videoResolution }}</span>
        </div>

        <div
          class="flex items-center gap-1.5 shrink-0 pointer-events-auto"
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
          <!-- The layer of someone's share I receive -->
          <ScreenLayerMenu
            v-if="!ownOnStage && !cameraOnStage && stageUserId"
            :user-id="stageUserId"
            :host="fullscreen ? videoContainer : null"
            :show-label="!tiny"
            @open-change="emit('quality-open', $event)"
          />
          <!-- Stream quality of my own share (its menu cannot show in full screen) -->
          <StreamQualityMenu v-if="ownOnStage && !fullscreen" @open-change="emit('quality-open', $event)" />

          <!-- Viewer Stream Audio Controls (Volume & Mute) -->
          <div
            v-else-if="stageUserId"
            class="h-8 flex items-center gap-1.5 bg-black/75 hover:bg-black/90 px-2 rounded-lg text-white group/vol"
          >
            <button
              type="button"
              data-testid="viewer-stream-audio-mute"
              class="btn btn-icon btn-sm text-white hover:text-mnema-accent"
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
              v-if="!tiny"
              :class="['cursor-pointer opacity-80 group-hover/vol:opacity-100 transition-opacity', narrow ? 'w-12' : 'w-16']"
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
            v-if="pip.supported && !tiny"
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
            @click="emit('toggle-fullscreen')"
            v-tooltip="{ text: fullscreen ? $t('talk.exitFullscreen') : $t('talk.fullscreen'), shortcut: 'F' }"
            :aria-pressed="fullscreen ? 'true' : 'false'"
            :class="[overlayButton, 'bg-black/75 hover:bg-black/90']"
          >
            <Minimize2 v-if="fullscreen" class="w-4 h-4" />
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
          :fullscreen="fullscreen"
          @update:pinned="emit('bar-pinned', $event)"
        />
      </div>
    </div>
  </div>
</template>
