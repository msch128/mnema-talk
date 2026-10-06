<script setup>
// The Talk's floating call controls (mute, deafen, camera, screen, … leave).
// VoiceStage places it over the stage area and fades it out when idle
// (useAutoHide); `visible` is that state. It reports `pinned` while it must
// stay: the pointer or keyboard focus is on it, or its "more" menu is open.
// Compact (narrow Talk area): icons only, secondary toggles in "more".
import { computed, ref, watch } from 'vue'
import {
  Mic, MicOff, Headphones, HeadphoneOff, Video, VideoOff, Eye, EyeOff, Monitor,
  Volume2, VolumeX, Sparkles, Sliders, Settings, PhoneOff, Ellipsis
} from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { useMenuState } from '../composables/useNavMenus'
import ContextMenu from './ContextMenu.vue'
import { t } from '../i18n'

const props = defineProps({
  visible: { type: Boolean, default: true },
  compact: { type: Boolean, default: false },
  // In the stage's full screen: page dialogs and menus cannot show there.
  fullscreen: { type: Boolean, default: false }
})
const emit = defineEmits(['update:pinned'])

const voiceStore = useVoiceStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare, applyAudioSettings, toggleCamera } = useWebRTC()

function toggleScreenShare() {
  if (voiceStore.isScreenSharing) stopScreenShare()
  else if (props.fullscreen) startScreenShare()
  else voiceStore.openScreenShareModal()
}

// The quick toggle must swap the running mic, not just flip the setting.
function toggleNoiseCancelling() {
  voiceStore.toggleNoiseCancelling()
  // Without a mic in the call, applyAudioSettings would start a mic test instead.
  if (voiceStore.localAudioStream) applyAudioSettings()
}

// --- "More" (compact only): the secondary toggles as a menu ---
const more = useMenuState()
const moreButton = ref(null)
let moreWasOpen = false
function onMorePointerDown() {
  moreWasOpen = more.state.open
}
function toggleMore() {
  if (moreWasOpen || more.state.open) {
    moreWasOpen = false
    more.state.open = false
    return
  }
  more.show({ currentTarget: moreButton.value }, () => {
    const items = [
      { type: 'checkbox', id: 'all-cameras-off', label: t('talk.allCamerasOff'), checked: voiceStore.allCamerasOff, action: () => voiceStore.setAllCamerasOff(!voiceStore.allCamerasOff) },
      { type: 'checkbox', id: 'noise', label: t('audio.noise'), checked: !!voiceStore.noiseCancelling, action: toggleNoiseCancelling }
    ]
    if (voiceStore.isScreenSharing) {
      items.push({ type: 'checkbox', id: 'stream-audio', label: t('talk.muteStreamAudio'), checked: voiceStore.isScreenAudioMuted, action: () => voiceStore.toggleScreenAudioMute() })
      if (!props.fullscreen) {
        items.push({ id: 'stream-settings', label: t('talk.quality.streamModalTitle'), icon: Settings, action: () => voiceStore.openScreenShareModal() })
      }
    }
    if (!props.fullscreen) {
      items.push({ type: 'separator' }, { id: 'audio-settings', label: t('audio.settings'), icon: Sliders, action: () => { voiceStore.showAudioSettings = true } })
    }
    return items
  })
}

// --- Pinned: pointer on the bar, keyboard focus in it, or its menu open ---
const hovered = ref(false)
const keyboardFocus = ref(false)
let pointerFocus = false
function onPointerEnter(e) {
  if (e.pointerType !== 'touch') hovered.value = true
}
function onPointerLeave() {
  hovered.value = false
}
function onFocusIn(e) {
  let visibleFocus = !pointerFocus
  try {
    visibleFocus = e.target.matches(':focus-visible')
  } catch {
    // Older engines: the pointer flag decides.
  }
  keyboardFocus.value = visibleFocus
}
function onFocusOut(e) {
  if (!e.currentTarget.contains(e.relatedTarget)) keyboardFocus.value = false
}
const pinned = computed(() => hovered.value || keyboardFocus.value || more.state.open)
watch(pinned, v => emit('update:pinned', v), { immediate: true })

// A tap on the hidden bar only brings it back: no invisible button fires.
let swallowClick = false
function onPointerDownCapture(e) {
  pointerFocus = true
  setTimeout(() => { pointerFocus = false }, 0)
  swallowClick = !props.visible && e.pointerType === 'touch'
}
function onClickCapture(e) {
  if (!swallowClick) return
  swallowClick = false
  e.preventDefault()
  e.stopPropagation()
}

const btn = 'inline-flex items-center justify-center gap-1.5 rounded-full flex-shrink-0 transition-colors duration-150 motion-reduce:transition-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-elevated'
const icon = computed(() => `${btn} ${props.compact ? 'w-9 h-9' : 'w-10 h-10'}`)
const wide = computed(() => `${btn} ${props.compact ? 'w-9 h-9' : 'h-10 px-4 text-sm font-semibold'}`)
const neutral = 'bg-mnema-surface text-mnema-text hover:bg-mnema-hover'
const danger = 'bg-mnema-danger text-white hover:bg-mnema-danger/90'
const accent = 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover'
const soft = 'bg-mnema-accent-subtle text-mnema-accent ring-1 ring-inset ring-mnema-accent/30 hover:bg-mnema-accent/20'
</script>

<template>
  <div
    role="toolbar"
    data-testid="talk-controls"
    :data-visible="visible ? 'true' : 'false'"
    :aria-label="t('talk.controls')"
    :class="[
      'flex items-center gap-1.5 p-1.5 rounded-full bg-mnema-elevated/95 border border-mnema-border shadow-xl shadow-black/40 backdrop-blur-sm',
      'transition-[opacity,transform] duration-200 ease-out motion-reduce:transition-none',
      visible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-2'
    ]"
    @pointerenter="onPointerEnter"
    @pointerleave="onPointerLeave"
    @pointerdown.capture="onPointerDownCapture"
    @click.capture="onClickCapture"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
  >
    <button
      type="button"
      data-testid="talk-mute"
      :class="[icon, voiceStore.isMuted ? danger : neutral]"
      :aria-pressed="voiceStore.isMuted ? 'true' : 'false'"
      v-tooltip="voiceStore.isMuted ? t('voice.unmute') : t('voice.mute')"
      @click="voiceStore.toggleMute"
    >
      <MicOff v-if="voiceStore.isMuted" class="w-5 h-5" aria-hidden="true" />
      <Mic v-else class="w-5 h-5" aria-hidden="true" />
    </button>

    <button
      type="button"
      data-testid="talk-deafen"
      :class="[icon, voiceStore.isDeafened ? danger : neutral]"
      :aria-pressed="voiceStore.isDeafened ? 'true' : 'false'"
      v-tooltip="voiceStore.isDeafened ? t('voice.undeafen') : t('voice.deafen')"
      @click="voiceStore.toggleDeafen"
    >
      <HeadphoneOff v-if="voiceStore.isDeafened" class="w-5 h-5" aria-hidden="true" />
      <Headphones v-else class="w-5 h-5" aria-hidden="true" />
    </button>

    <button
      type="button"
      :class="[icon, voiceStore.isCameraOn ? accent : neutral]"
      :aria-pressed="voiceStore.isCameraOn ? 'true' : 'false'"
      v-tooltip="voiceStore.isCameraOn ? t('talk.stopCamera') : t('talk.startCamera')"
      @click="toggleCamera"
    >
      <Video v-if="voiceStore.isCameraOn" class="w-5 h-5" aria-hidden="true" />
      <VideoOff v-else class="w-5 h-5" aria-hidden="true" />
    </button>

    <button
      v-if="!compact"
      type="button"
      :class="[icon, voiceStore.allCamerasOff ? accent : neutral]"
      :aria-pressed="voiceStore.allCamerasOff ? 'true' : 'false'"
      v-tooltip="voiceStore.allCamerasOff ? t('talk.allCamerasOn') : t('talk.allCamerasOff')"
      @click="voiceStore.setAllCamerasOff(!voiceStore.allCamerasOff)"
    >
      <EyeOff v-if="voiceStore.allCamerasOff" class="w-5 h-5" aria-hidden="true" />
      <Eye v-else class="w-5 h-5" aria-hidden="true" />
    </button>

    <button
      type="button"
      :class="[wide, voiceStore.isScreenSharing ? accent : neutral]"
      :aria-pressed="voiceStore.isScreenSharing ? 'true' : 'false'"
      v-tooltip="voiceStore.isScreenSharing ? t('voice.stopShare') : t('voice.share')"
      @click="toggleScreenShare"
    >
      <Monitor class="w-5 h-5" aria-hidden="true" />
      <span v-if="!compact">{{ voiceStore.isScreenSharing ? t('talk.stopShareShort') : t('talk.shareShort') }}</span>
    </button>

    <template v-if="!compact">
      <button
        v-if="voiceStore.isScreenSharing"
        type="button"
        :class="[icon, voiceStore.isScreenAudioMuted ? 'bg-mnema-danger/15 text-mnema-danger ring-1 ring-inset ring-mnema-danger/30 hover:bg-mnema-danger/25' : neutral]"
        :aria-pressed="voiceStore.isScreenAudioMuted ? 'true' : 'false'"
        v-tooltip="voiceStore.isScreenAudioMuted ? t('talk.unmuteStreamAudio') : t('talk.muteStreamAudio')"
        @click="voiceStore.toggleScreenAudioMute"
      >
        <VolumeX v-if="voiceStore.isScreenAudioMuted" class="w-5 h-5" aria-hidden="true" />
        <Volume2 v-else class="w-5 h-5" aria-hidden="true" />
      </button>

      <button
        type="button"
        :class="[icon, voiceStore.noiseCancelling ? soft : `${neutral} !text-mnema-tertiary hover:!text-mnema-text`]"
        :aria-pressed="voiceStore.noiseCancelling ? 'true' : 'false'"
        v-tooltip="t('talk.noiseToggleTip')"
        @click="toggleNoiseCancelling"
      >
        <Sparkles class="w-5 h-5" aria-hidden="true" />
      </button>

      <button
        v-if="!fullscreen"
        type="button"
        :class="[icon, neutral]"
        v-tooltip="t('audio.settings')"
        @click="voiceStore.showAudioSettings = true"
      >
        <Sliders class="w-5 h-5" aria-hidden="true" />
      </button>
    </template>

    <button
      v-else
      ref="moreButton"
      type="button"
      data-testid="talk-more"
      :class="[icon, more.state.open ? 'bg-mnema-hover text-mnema-text' : neutral]"
      aria-haspopup="menu"
      :aria-expanded="more.state.open ? 'true' : 'false'"
      v-tooltip="t('talk.moreControls')"
      @pointerdown="onMorePointerDown"
      @click="toggleMore"
    >
      <Ellipsis class="w-5 h-5" aria-hidden="true" />
    </button>

    <div class="w-px h-6 bg-mnema-border mx-0.5 flex-shrink-0" aria-hidden="true"></div>

    <button
      type="button"
      data-testid="talk-leave"
      :class="[wide, danger]"
      v-tooltip="t('voice.leave')"
      @click="leaveVoiceChannel"
    >
      <PhoneOff class="w-5 h-5" aria-hidden="true" />
      <span v-if="!compact">{{ t('voice.leave') }}</span>
    </button>

    <ContextMenu
      v-model="more.state.open"
      :x="more.state.x"
      :y="more.state.y"
      :anchor="more.state.anchor"
      :items="more.items.value"
      :min-width="240"
      :aria-label="t('talk.moreControls')"
    />
  </div>
</template>
