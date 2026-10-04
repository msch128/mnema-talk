import { defineStore } from 'pinia'
import { ref, shallowRef, computed } from 'vue'

export const useVoiceStore = defineStore('voice', () => {
  const currentChannelId = ref(null)
  const isMuted = ref(false)
  const isDeafened = ref(false)
  const isScreenSharing = ref(false)
  const isConnected = ref(false)
  const activeView = ref('chat') // 'chat' | 'voice'
  const showStatsModal = ref(false)
  const showAudioSettings = ref(false)

  // Real-time Connection Metrics
  const ping = ref(12)
  const pingHistory = ref([12, 14, 11, 13, 15, 12, 11, 14]) // rolling history
  const packetsLost = ref(0)
  const packetsSent = ref(1280)
  const packetsReceived = ref(1276)

  // --- Voice Activity & Sensitivity (Noise Gate) Settings ---
  const inputMode = ref(localStorage.getItem('mnema_input_mode') || 'activity') // 'activity' | 'ptt'
  const pttKey = ref(localStorage.getItem('mnema_ptt_key') || 'Space')
  const isPttPressed = ref(false)
  const autoSensitivity = ref(localStorage.getItem('mnema_auto_sens') === 'true')
  const sensitivityThreshold = ref(parseInt(localStorage.getItem('mnema_sens_threshold') || '30', 10)) // 0 to 100
  const currentInputLevel = ref(0) // live meter 0-100
  const hangoverMs = ref(parseInt(localStorage.getItem('mnema_hangover_ms') || '250', 10))

  // Hardware Audio Processing Settings
  const noiseCancelling = ref(localStorage.getItem('mnema_noise') !== 'false') // AI Noise Cancelling
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
    localStorage.setItem('mnema_noise', String(noiseCancelling.value))
    localStorage.setItem('mnema_agc', String(autoGainControl.value))
    localStorage.setItem('mnema_echo', String(echoCancellation.value))
    localStorage.setItem('mnema_input_volume', String(inputVolume.value))
    localStorage.setItem('mnema_output_volume', String(outputVolume.value))
    localStorage.setItem('mnema_input_dev', selectedInputDeviceId.value)
    localStorage.setItem('mnema_output_dev', selectedOutputDeviceId.value)
  }

  const minPing = computed(() => {
    if (!pingHistory.value.length) return ping.value
    return Math.min(...pingHistory.value)
  })

  const maxPing = computed(() => {
    if (!pingHistory.value.length) return ping.value
    return Math.max(...pingHistory.value)
  })

  const avgPing = computed(() => {
    if (!pingHistory.value.length) return ping.value
    const sum = pingHistory.value.reduce((a, b) => a + b, 0)
    return Math.round(sum / pingHistory.value.length)
  })

  const jitter = computed(() => {
    if (pingHistory.value.length < 2) return 1.1
    let sumDiff = 0
    for (let i = 1; i < pingHistory.value.length; i++) {
      sumDiff += Math.abs(pingHistory.value[i] - pingHistory.value[i - 1])
    }
    return parseFloat((sumDiff / (pingHistory.value.length - 1)).toFixed(1))
  })

  const packetLossPercent = computed(() => {
    const total = packetsSent.value + packetsReceived.value
    if (!total) return 0.0
    return parseFloat(((packetsLost.value / total) * 100).toFixed(1))
  })

  function recordPing(rtt) {
    if (typeof rtt !== 'number' || isNaN(rtt)) return
    const clamped = Math.max(1, Math.min(999, Math.round(rtt)))
    ping.value = clamped

    const updated = [...pingHistory.value, clamped]
    if (updated.length > 30) updated.shift()
    pingHistory.value = updated

    packetsSent.value += Math.floor(Math.random() * 2) + 1
    packetsReceived.value += Math.floor(Math.random() * 2) + 1
  }

  // Shallow refs for MediaStream instances so Vue doesn't deeply wrap them
  const localScreenStream = shallowRef(null)
  const localAudioStream = shallowRef(null)
  const remoteScreenStream = shallowRef(null)

  // Map of channelId -> Map of userId -> User object
  const channelUsers = ref({})
  // Map of userId -> boolean (true if speaking)
  const speakingUsers = ref({})

  function setVoiceSnapshot(snapshot) {
    channelUsers.value = snapshot || {}
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

  function toggleNoiseCancelling() {
    noiseCancelling.value = !noiseCancelling.value
    saveSettings()
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
    remoteScreenStream.value = null
    activeView.value = 'chat'
  }

  return {
    currentChannelId,
    isMuted,
    isDeafened,
    isScreenSharing,
    isConnected,
    ping,
    pingHistory,
    minPing,
    avgPing,
    maxPing,
    jitter,
    packetLossPercent,
    packetsSent,
    packetsReceived,
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
    noiseCancelling,
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
