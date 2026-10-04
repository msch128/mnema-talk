import { ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'

export function useWebRTC() {
  const voiceStore = useVoiceStore()
  const chatStore = useChatStore()

  const localAudioStream = ref(null)
  const localScreenStream = ref(null)
  const audioContext = ref(null)
  const analyser = ref(null)
  let speakingInterval = null
  let lastAboveThresholdTime = 0

  async function refreshAudioDevices() {
    try {
      if (!navigator.mediaDevices?.enumerateDevices) return
      const devices = await navigator.mediaDevices.enumerateDevices()
      voiceStore.availableInputDevices = devices.filter(d => d.kind === 'audioinput')
      voiceStore.availableOutputDevices = devices.filter(d => d.kind === 'audiooutput')
    } catch (err) {
      console.warn('enumerateDevices error:', err)
    }
  }

  async function joinVoiceChannel(channelId) {
    voiceStore.setChannel(channelId)
    chatStore.sendWSEvent('voice_join', { channel_id: channelId })

    try {
      await refreshAudioDevices()

      const audioConstraints = {
        echoCancellation: voiceStore.echoCancellation,
        noiseSuppression: voiceStore.noiseCancelling,
        autoGainControl: voiceStore.autoGainControl, // Crucial: false prevents boosting background voice
        googEchoCancellation: voiceStore.echoCancellation,
        googAutoGainControl: voiceStore.autoGainControl,
        googNoiseSuppression: voiceStore.noiseCancelling,
        googHighpassFilter: true,
        googTypingNoiseDetection: true
      }

      if (voiceStore.selectedInputDeviceId) {
        audioConstraints.deviceId = { exact: voiceStore.selectedInputDeviceId }
      }

      // 1. Request microphone access with tuned acoustic constraints
      localAudioStream.value = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints
      })

      voiceStore.localAudioStream = localAudioStream.value

      // 2. Setup Web Audio API volume analyzer & Sensitivity Noise Gate
      setupSpeakingDetection(localAudioStream.value)

      // 3. Setup Push-to-Talk listeners if needed
      setupPttListeners()
    } catch (err) {
      console.warn('Microphone access denied or unavailable:', err)
    }
  }

  function setupSpeakingDetection(stream) {
    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext
      audioContext.value = new AudioCtx()
      analyser.value = audioContext.value.createAnalyser()
      analyser.value.fftSize = 512
      analyser.value.smoothingTimeConstant = 0.3

      const source = audioContext.value.createMediaStreamSource(stream)

      // Highpass filter at 85Hz to cut desk thuds and low frequency rumble
      const biquad = audioContext.value.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, audioContext.value.currentTime)

      source.connect(biquad)
      biquad.connect(analyser.value)

      const buffer = new Uint8Array(analyser.value.frequencyBinCount)
      let wasSpeaking = false

      speakingInterval = setInterval(() => {
        if (!analyser.value) return

        analyser.value.getByteFrequencyData(buffer)
        let sum = 0
        for (let i = 0; i < buffer.length; i++) {
          sum += buffer[i]
        }
        const average = sum / buffer.length // 0 to 128
        // Convert to calibrated 0-100 level
        const level = Math.min(100, Math.round((average / 75) * 100))
        voiceStore.currentInputLevel = level

        if (voiceStore.isMuted) {
          if (wasSpeaking) {
            wasSpeaking = false
            chatStore.sendWSEvent('voice_speaking', { active: false })
          }
          if (localAudioStream.value) {
            const track = localAudioStream.value.getAudioTracks()[0]
            if (track && track.enabled) track.enabled = false
          }
          return
        }

        // Noise Gate & Sensitivity Evaluation
        let shouldTransmit = false

        if (voiceStore.inputMode === 'ptt') {
          shouldTransmit = voiceStore.isPttPressed
        } else {
          // Voice Activity with Sensitivity Threshold
          const threshold = voiceStore.autoSensitivity ? 25 : voiceStore.sensitivityThreshold

          if (level >= threshold) {
            shouldTransmit = true
            lastAboveThresholdTime = Date.now()
          } else if (Date.now() - lastAboveThresholdTime < voiceStore.hangoverMs) {
            // Hangover hold time prevents cutting off trailing words
            shouldTransmit = true
          } else {
            shouldTransmit = false
          }
        }

        // Physical audio track gate (muting track when below threshold to eliminate background bleed)
        if (localAudioStream.value) {
          const track = localAudioStream.value.getAudioTracks()[0]
          if (track && track.enabled !== shouldTransmit) {
            track.enabled = shouldTransmit
          }
        }

        // Animated speaking halo state
        if (shouldTransmit !== wasSpeaking) {
          wasSpeaking = shouldTransmit
          chatStore.sendWSEvent('voice_speaking', { active: shouldTransmit })
        }
      }, 35)
    } catch (err) {
      console.warn('AudioContext speaking detector setup error:', err)
    }
  }

  function setupPttListeners() {
    window.addEventListener('keydown', handlePttKeyDown)
    window.addEventListener('keyup', handlePttKeyUp)
  }

  function removePttListeners() {
    window.removeEventListener('keydown', handlePttKeyDown)
    window.removeEventListener('keyup', handlePttKeyUp)
  }

  function handlePttKeyDown(e) {
    if (voiceStore.inputMode !== 'ptt') return
    // Ignore key repeat if already holding down
    if (e.repeat) return

    const key = e.code || e.key
    if (key === voiceStore.pttKey) {
      voiceStore.isPttPressed = true
    }
  }

  function handlePttKeyUp(e) {
    if (voiceStore.inputMode !== 'ptt') return
    const key = e.code || e.key
    if (key === voiceStore.pttKey) {
      voiceStore.isPttPressed = false
    }
  }

  // 4K 60 FPS Screen Sharing Pipeline
  async function startScreenShare() {
    try {
      localScreenStream.value = await navigator.mediaDevices.getDisplayMedia({
        video: {
          frameRate: { ideal: 60, max: 60 },
          width: { ideal: 3840, max: 3840 },
          height: { ideal: 2160, max: 2160 }
        },
        audio: true
      })

      const videoTrack = localScreenStream.value.getVideoTracks()[0]
      if (videoTrack && 'contentHint' in videoTrack) {
        videoTrack.contentHint = 'detail'
      }

      voiceStore.localScreenStream = localScreenStream.value
      voiceStore.isScreenSharing = true

      videoTrack.onended = () => {
        stopScreenShare()
      }
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
    }
  }

  function stopScreenShare() {
    if (localScreenStream.value) {
      localScreenStream.value.getTracks().forEach(t => t.stop())
      localScreenStream.value = null
    }
    voiceStore.localScreenStream = null
    voiceStore.isScreenSharing = false
  }

  function leaveVoiceChannel() {
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }

    removePttListeners()

    if (localAudioStream.value) {
      localAudioStream.value.getTracks().forEach(t => t.stop())
      localAudioStream.value = null
    }
    voiceStore.localAudioStream = null

    if (audioContext.value) {
      audioContext.value.close().catch(() => {})
      audioContext.value = null
    }

    stopScreenShare()
    chatStore.sendWSEvent('voice_leave', {})
    voiceStore.disconnect()
  }

  return {
    localAudioStream,
    localScreenStream,
    refreshAudioDevices,
    joinVoiceChannel,
    leaveVoiceChannel,
    startScreenShare,
    stopScreenShare
  }
}
