import { defineStore } from 'pinia'
import { ref, shallowRef, computed } from 'vue'

export const NOISE_MODES = ['ai', 'ai-lite', 'browser', 'off']

// Per-user playback settings live in the browser only (what I hear of whom).
const USER_VOLUMES_KEY = 'mnema_user_volumes'
const USER_MUTED_KEY = 'mnema_user_muted'
export const USER_VOLUME_MIN = 0
export const USER_VOLUME_MAX = 200
export const USER_VOLUME_DEFAULT = 100

function readJson(key) {
  try {
    const parsed = JSON.parse(localStorage.getItem(key) || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function writeJson(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Storage blocked or full: the setting then only lasts for this session.
  }
}

export const useVoiceStore = defineStore('voice', () => {
  const currentChannelId = ref(null)
  const isMuted = ref(false)
  const isDeafened = ref(false)
  const isScreenSharing = ref(false)
  const isCameraOn = ref(false)
  const isConnected = ref(false)
  const activeView = ref('chat') // 'chat' | 'voice'
  const showStatsModal = ref(false)
  const showAudioSettings = ref(false)

  // Measured connection metrics. `ping` is the WebSocket round trip to the
  // server; `rtcStats` holds what RTCPeerConnection.getStats() reports while in
  // voice. Both stay empty until there is a real measurement.
  const ping = ref(null)
  const pingHistory = ref([])
  const rtcStats = ref(null)
  // True when the browser blocked remote audio playback (autoplay policy after
  // a reload); the UI shows a click-to-enable banner.
  const audioBlocked = ref(false)

  // --- Voice Activity & Sensitivity (Noise Gate) Settings ---
  const inputMode = ref(localStorage.getItem('mnema_input_mode') || 'activity') // 'activity' | 'ptt'
  const pttKey = ref(localStorage.getItem('mnema_ptt_key') || 'Space')
  const isPttPressed = ref(false)
  const autoSensitivity = ref(localStorage.getItem('mnema_auto_sens') === 'true')
  const sensitivityThreshold = ref(parseInt(localStorage.getItem('mnema_sens_threshold') || '30', 10)) // 0 to 100
  const currentInputLevel = ref(0) // live meter 0-100
  const hangoverMs = ref(parseInt(localStorage.getItem('mnema_hangover_ms') || '250', 10))

  // Hardware Audio Processing Settings
  // 'ai' (DeepFilterNet3), 'ai-lite' (GTCRN, see lib/noiseSuppressor),
  // 'browser' (getUserMedia noiseSuppression) or 'off'. Older clients stored
  // only 'true'/'false'.
  const storedNoise = localStorage.getItem('mnema_noise')
  const noiseMode = ref(
    NOISE_MODES.includes(storedNoise) ? storedNoise : storedNoise === 'false' ? 'off' : 'ai'
  )
  const noiseCancelling = computed(() => noiseMode.value !== 'off')
  const autoGainControl = ref(localStorage.getItem('mnema_agc') === 'true') // Default false to avoid boosting background voices
  const echoCancellation = ref(localStorage.getItem('mnema_echo') !== 'false')
  const inputVolume = ref(parseInt(localStorage.getItem('mnema_input_volume') || '100', 10))
  const outputVolume = ref(parseInt(localStorage.getItem('mnema_output_volume') || '100', 10))

  const selectedInputDeviceId = ref(localStorage.getItem('mnema_input_dev') || '')
  const selectedOutputDeviceId = ref(localStorage.getItem('mnema_output_dev') || '')
  const availableInputDevices = ref([])
  const availableOutputDevices = ref([])

  function saveSettings() {
    localStorage.setItem('mnema_input_mode', inputMode.value)
    localStorage.setItem('mnema_ptt_key', pttKey.value)
    localStorage.setItem('mnema_auto_sens', String(autoSensitivity.value))
    localStorage.setItem('mnema_sens_threshold', String(sensitivityThreshold.value))
    localStorage.setItem('mnema_hangover_ms', String(hangoverMs.value))
    localStorage.setItem('mnema_noise', noiseMode.value)
    localStorage.setItem('mnema_agc', String(autoGainControl.value))
    localStorage.setItem('mnema_echo', String(echoCancellation.value))
    localStorage.setItem('mnema_input_volume', String(inputVolume.value))
    localStorage.setItem('mnema_output_volume', String(outputVolume.value))
    localStorage.setItem('mnema_input_dev', selectedInputDeviceId.value)
    localStorage.setItem('mnema_output_dev', selectedOutputDeviceId.value)
  }

  const minPing = computed(() => (pingHistory.value.length ? Math.min(...pingHistory.value) : null))
  const maxPing = computed(() => (pingHistory.value.length ? Math.max(...pingHistory.value) : null))
  const avgPing = computed(() => {
    if (!pingHistory.value.length) return null
    return Math.round(pingHistory.value.reduce((a, b) => a + b, 0) / pingHistory.value.length)
  })

  function recordPing(rtt) {
    if (typeof rtt !== 'number' || isNaN(rtt) || rtt < 0) return
    const rounded = Math.min(9999, Math.round(rtt))
    ping.value = rounded

    const updated = [...pingHistory.value, rounded]
    if (updated.length > 30) updated.shift()
    pingHistory.value = updated
  }

  // Shallow refs for MediaStream instances so Vue doesn't deeply wrap them
  const localScreenStream = shallowRef(null)
  const localAudioStream = shallowRef(null)
  const remoteScreenStream = shallowRef(null)
  // Who publishes the remote screen share (user ID), null when nobody does.
  const remoteScreenUserId = ref(null)
  const localCameraStream = shallowRef(null)
  // userId -> MediaStream of that participant's camera (replaced, never mutated).
  const userVideoStreams = shallowRef({})

  // Map of channelId -> Map of userId -> User object
  const channelUsers = ref({})
  // Map of userId -> boolean (true if speaking)
  const speakingUsers = ref({})

  function setVoiceSnapshot(snapshot) {
    channelUsers.value = snapshot || {}
  }

  // --- Per-user playback (volume 0..200 %, local mute) ---
  const userVolumes = ref(readJson(USER_VOLUMES_KEY))
  const localMutedUsers = ref(readJson(USER_MUTED_KEY))

  function getUserVolume(userId) {
    const v = userVolumes.value[userId]
    return typeof v === 'number' ? v : USER_VOLUME_DEFAULT
  }

  function setUserVolume(userId, volume) {
    if (!userId) return
    const n = Number(volume)
    if (!Number.isFinite(n)) return
    const clamped = Math.min(USER_VOLUME_MAX, Math.max(USER_VOLUME_MIN, Math.round(n)))
    const next = { ...userVolumes.value }
    // The default needs no entry.
    if (clamped === USER_VOLUME_DEFAULT) delete next[userId]
    else next[userId] = clamped
    userVolumes.value = next
    writeJson(USER_VOLUMES_KEY, next)
  }

  function isUserLocalMuted(userId) {
    return !!localMutedUsers.value[userId]
  }

  function toggleLocalMute(userId) {
    if (!userId) return
    const next = { ...localMutedUsers.value }
    if (next[userId]) delete next[userId]
    else next[userId] = true
    localMutedUsers.value = next
    writeJson(USER_MUTED_KEY, next)
  }

  function setUserVideoStream(userId, stream) {
    userVideoStreams.value = { ...userVideoStreams.value, [userId]: stream }
  }

  function removeUserVideoStream(userId, stream) {
    const current = userVideoStreams.value[userId]
    // A stale track ending must not drop a newer stream of the same user.
    if (!current || (stream && current !== stream)) return
    const next = { ...userVideoStreams.value }
    delete next[userId]
    userVideoStreams.value = next
  }

  function handleVoiceStateUpdate(update) {
    const { action, channel_id, user, user_id } = update
    if (!channelUsers.value[channel_id]) {
      channelUsers.value[channel_id] = {}
    }

    if (action === 'join' && user) {
      channelUsers.value[channel_id][user.id] = user
    } else if (action === 'leave' && user_id) {
      delete channelUsers.value[channel_id][user_id]
      delete speakingUsers.value[user_id]
      removeUserVideoStream(user_id)
    }
  }

  function handleSpeakingEvent(event) {
    const { user_id, active } = event || {}
    if (!user_id) return
    if (active) {
      if (!speakingUsers.value[user_id]) {
        speakingUsers.value = { ...speakingUsers.value, [user_id]: true }
      }
    } else {
      if (speakingUsers.value[user_id]) {
        const next = { ...speakingUsers.value }
        delete next[user_id]
        speakingUsers.value = next
      }
    }
  }

  function toggleMute() {
    isMuted.value = !isMuted.value
    if (localAudioStream.value) {
      localAudioStream.value.getAudioTracks().forEach(track => {
        track.enabled = !isMuted.value
      })
    }
  }

  function toggleDeafen() {
    isDeafened.value = !isDeafened.value
    if (isDeafened.value) {
      isMuted.value = true
      if (localAudioStream.value) {
        localAudioStream.value.getAudioTracks().forEach(track => {
          track.enabled = false
        })
      }
    }
  }

  function setNoiseMode(mode) {
    noiseMode.value = mode
    saveSettings()
  }

  // Quick toggle in the call bar: off <-> AI filter.
  function toggleNoiseCancelling() {
    setNoiseMode(noiseMode.value === 'off' ? 'ai' : 'off')
  }

  function setChannel(channelId) {
    currentChannelId.value = channelId
    isConnected.value = !!channelId
    if (channelId) {
      activeView.value = 'voice'
    }
  }

  function disconnect() {
    currentChannelId.value = null
    isConnected.value = false
    isScreenSharing.value = false
    isCameraOn.value = false
    localCameraStream.value = null
    remoteScreenStream.value = null
    remoteScreenUserId.value = null
    userVideoStreams.value = {}
    activeView.value = 'chat'
  }

  return {
    currentChannelId,
    isMuted,
    isDeafened,
    isScreenSharing,
    isCameraOn,
    localCameraStream,
    remoteScreenUserId,
    userVideoStreams,
    setUserVideoStream,
    removeUserVideoStream,
    userVolumes,
    localMutedUsers,
    getUserVolume,
    setUserVolume,
    isUserLocalMuted,
    toggleLocalMute,
    isConnected,
    ping,
    pingHistory,
    minPing,
    avgPing,
    maxPing,
    rtcStats,
    audioBlocked,
    recordPing,
    showStatsModal,
    showAudioSettings,
    // Voice activity & Sensitivity settings
    inputMode,
    pttKey,
    isPttPressed,
    autoSensitivity,
    sensitivityThreshold,
    currentInputLevel,
    hangoverMs,
    noiseMode,
    noiseCancelling,
    setNoiseMode,
    autoGainControl,
    echoCancellation,
    inputVolume,
    outputVolume,
    selectedInputDeviceId,
    selectedOutputDeviceId,
    availableInputDevices,
    availableOutputDevices,
    saveSettings,
    toggleNoiseCancelling,
    activeView,
    localScreenStream,
    localAudioStream,
    remoteScreenStream,
    channelUsers,
    speakingUsers,
    setVoiceSnapshot,
    handleVoiceStateUpdate,
    handleSpeakingEvent,
    toggleMute,
    toggleDeafen,
    setChannel,
    disconnect
  }
})
