import { computed, ref, shallowRef } from 'vue'
import { DEFAULT_STREAM_QUALITY, normalizeQuality, SCREEN_QUALITY_STORAGE_KEY } from '../../lib/streamQuality'
import type { ScreenQuality } from '../../lib/streamQuality'
import type { MediaState, ScreenLayer, SubscribeRequest, VoiceSnapshot } from '../../types/events'
import type { RTCConnectionStats } from '../../lib/rtcStats'
import type { MediaVerification } from '../../lib/media/verify'
import { usePlaybackPrefs, readJson } from '../../lib/voicePlayback'
import { useVoiceSettings } from '../voiceSettings'
import { useAuthStore } from '../auth'
import type { LiveReaction } from './stage'

// Cameras of others are on by default; what I opted out of is remembered.
export const HIDDEN_CAMERAS_KEY = 'mnema_hidden_cameras'
export const ALL_CAMERAS_OFF_KEY = 'mnema_all_cameras_off'
// "Hide participants without video" in the Talk view (per browser).
export const HIDE_NO_VIDEO_KEY = 'mnema_hide_no_video'

// My own share: its sound is not sent (the streamer's mute), whether it has
// sound at all, and the quality it is sent in (see useStreamQuality; every
// share starts at the default).
function loadSavedScreenQuality(): ScreenQuality {
  try {
    const raw = localStorage.getItem(SCREEN_QUALITY_STORAGE_KEY)
    if (raw) {
      const parsed: unknown = JSON.parse(raw)
      return normalizeQuality(parsed)
    }
  } catch (error) {
    void error
    // Storage blocked or invalid JSON: fall back to default quality.
  }
  return { ...DEFAULT_STREAM_QUALITY }
}

/** The streams, publications and rooms of the call (shallow refs for MediaStream instances so Vue doesn't deeply wrap them). */
function mediaState() {
  return {
    localScreenStream: shallowRef<MediaStream | null>(null),
    localAudioStream: shallowRef<MediaStream | null>(null),
    remoteScreenStream: shallowRef<MediaStream | null>(null),
    remoteScreenUserId: ref<string | null>(null),
    remoteScreenStreams: shallowRef<Record<string, MediaStream>>({}),
    ownScreenFocused: ref(false),
    focusedCamera: ref<string | null>(null),
    watchedScreens: ref<Record<string, boolean>>({}),
    mediaState: ref<Record<string, Omit<MediaState, 'user_id'>>>({}),
    screenViewers: ref<Record<string, string[]>>({}),
    screenLayers: ref<Record<string, ScreenLayer[]>>({}),
    receivedLayer: ref<Record<string, { layer: number, auto: boolean }>>({}),
    screenChoice: ref<Record<string, number | 'auto'>>({}),
    screenFit: ref<Record<string, number>>({}),
    serverScreenLayers: ref(0),
    layersWanted: ref<number[] | null>(null),
    localCameraStream: shallowRef<MediaStream | null>(null),
    userVideoStreams: shallowRef<Record<string, MediaStream>>({}),
    channelUsers: ref<VoiceSnapshot>({}),
    // Map of userId -> boolean (true if speaking)
    speakingUsers: ref<Record<string, boolean>>({}),
    // channelId -> ISO time the room got its first member (for the Talk timer).
    roomStartedAt: ref<Record<string, string>>({}),
    // Live reactions floating over the tiles of the Talk (a few seconds each).
    talkReactions: ref<LiveReaction[]>([])
  }
}

/** Everything the voice store keeps; the modules in this folder read and write it as `cs`. */
export function createVoiceState() {
  const pingHistory = ref<number[]>([])
  const base = {
    currentChannelId: ref<string | null>(null),
    isMuted: ref(false),
    isDeafened: ref(false),
    isScreenSharing: ref(false),
    isScreenAudioMuted: ref(false),
    hasScreenAudio: ref(false),
    // The browser could not apply the chosen quality to the running capture (see applyScreenQuality).
    screenQualityNeedsRestart: ref(false),
    screenQuality: ref(loadSavedScreenQuality()),
    showScreenShareModal: ref(false),
    isCameraOn: ref(false),
    isConnected: ref(false),
    activeView: ref<'chat' | 'voice'>('chat'),
    showStatsModal: ref(false),
    // The safety code of the call's current media keys (null without keys).
    mediaVerification: ref<MediaVerification | null>(null),
    showAudioSettings: ref(false),

    // Measured connection metrics. `ping` is the WebSocket round trip to the
    // server; `rtcStats` holds what RTCPeerConnection.getStats() reports while in
    // voice. Both stay empty until there is a real measurement.
    ping: ref<number | null>(null),
    pingHistory,
    rtcStats: ref<RTCConnectionStats | null>(null),
    // True when the browser blocked remote audio playback (autoplay policy after
    // a reload); the UI shows a click-to-enable banner.
    audioBlocked: ref(false),

    // Live voice activity state
    isPttPressed: ref(false),
    currentInputLevel: ref(0), // live meter 0-100
    // Mic test loopback state
    isMicTesting: ref(false),
    availableInputDevices: ref<MediaDeviceInfo[]>([]),
    availableOutputDevices: ref<MediaDeviceInfo[]>([]),

    ...mediaState(),

    // Persisted settings (inputMode, volumes, devices, noise handling, sound effects) and per-user playback.
    settings: useVoiceSettings(),
    playback: usePlaybackPrefs(),

    // --- Video subscriptions (enforced by the server's SFU) ---
    hiddenCameras: ref(readJson(HIDDEN_CAMERAS_KEY)),
    allCamerasOff: ref(readJson(ALL_CAMERAS_OFF_KEY).off === true),
    // The users whose camera tile is on screen (null: the Talk view does not report, every camera is received).
    visibleCameras: ref<Record<string, true> | null>(null),
    // The cameras the server was told to stop sending because their tile is off screen.
    offscreenCameras: ref<Record<string, true>>({}),
    // The cameras the server does not forward to me because I asked for more than its limit.
    cappedCameras: ref<Record<string, true>>({}),
    // Set by useWebRTC while in a call: sends a webrtc_subscribe event.
    subscriptionSink: null as ((request: SubscribeRequest) => void) | null,
    // Only tiles with a camera or a screen share in the Talk view.
    hideNoVideo: ref(readJson(HIDE_NO_VIDEO_KEY).on === true)
  }
  return {
    ...base,
    minPing: computed(() => (pingHistory.value.length ? Math.min(...pingHistory.value) : null)),
    maxPing: computed(() => (pingHistory.value.length ? Math.max(...pingHistory.value) : null)),
    avgPing: computed(() => {
      if (!pingHistory.value.length) return null
      return Math.round(pingHistory.value.reduce((a, b) => a + b, 0) / pingHistory.value.length)
    })
  }
}

export type VoiceState = ReturnType<typeof createVoiceState>

/** The signed-in user's id, null before sign-in or outside Pinia (tests). */
export function myUserId() {
  try {
    return useAuthStore()?.user?.id || null
  } catch {
    return null
  }
}
