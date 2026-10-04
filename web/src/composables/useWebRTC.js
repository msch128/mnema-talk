import { ref, watch } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'

// Module-level shared singletons across all components
const localAudioStream = ref(null)
const localScreenStream = ref(null)
let audioContext = null
let analyser = null
// Clone of the mic track used only for level metering. The noise gate disables the
// transmitted track, and a disabled track feeds silence into Web Audio, so metering
// the original would lock the gate shut forever.
let analysisTrack = null
let speakingInterval = null
let lastAboveThresholdTime = 0

// WebRTC Peer Connection and Remote Audio
let pc = null
let remoteAudioElements = []

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let iceServers = []
fetch('/api/webrtc/config')
  .then(res => (res.ok ? res.json() : null))
  .then(data => { if (data?.ice_servers) iceServers = data.ice_servers })
  .catch(() => {})

function updateRemoteVolume(voiceStore) {
  const vol = voiceStore.isDeafened ? 0 : (voiceStore.outputVolume / 100)
  remoteAudioElements.forEach(el => {
    el.volume = vol
  })
}

function cleanupPeerConnection(voiceStore) {
  if (pc) {
    try { pc.close() } catch {}
    pc = null
  }
  remoteAudioElements.forEach(el => {
    try {
      el.pause()
      el.srcObject = null
    } catch {}
  })
  remoteAudioElements = []
  if (voiceStore) {
    voiceStore.remoteScreenStream = null
  }
}

function setupPeerConnection(voiceStore, chatStore) {
  cleanupPeerConnection(voiceStore)

  pc = new RTCPeerConnection({ iceServers })

  pc.onicecandidate = (event) => {
    if (event.candidate) {
      chatStore.sendWSEvent('webrtc_candidate', event.candidate.toJSON())
    }
  }

  pc.ontrack = (event) => {
    if (event.track.kind === 'audio') {
      const audioEl = new Audio()
      audioEl.srcObject = new MediaStream([event.track])
      audioEl.autoplay = true
      const vol = voiceStore.isDeafened ? 0 : (voiceStore.outputVolume / 100)
      audioEl.volume = vol
      audioEl.play().catch(e => console.warn('[WebRTC] Audio auto-play warning:', e))
      remoteAudioElements.push(audioEl)

      event.track.onended = () => {
        audioEl.srcObject = null
        remoteAudioElements = remoteAudioElements.filter(a => a !== audioEl)
      }
    } else if (event.track.kind === 'video') {
      voiceStore.remoteScreenStream = new MediaStream([event.track])
      chatStore.sendWSEvent('webrtc_request_keyframe', {})

      event.track.onended = () => {
        voiceStore.remoteScreenStream = null
      }
    }
  }

  // Attach local microphone if available
  if (localAudioStream.value) {
    const audioTrack = localAudioStream.value.getAudioTracks()[0]
    if (audioTrack) {
      pc.addTrack(audioTrack, localAudioStream.value)
    }
  }

  // Attach local screenshare if active
  if (localScreenStream.value) {
    const videoTrack = localScreenStream.value.getVideoTracks()[0]
    if (videoTrack) {
      pc.addTrack(videoTrack, localScreenStream.value)
    }
  }

  return pc
}

// Test stream for Settings Modal when not connected to a voice channel
let testAudioStream = null
let testAudioContext = null
let testAnalyser = null
let testSpeakingInterval = null

// Accurate RMS volume calculator (time domain PCM audio)
function calculateRMSLevel(analyserNode, buffer) {
  if (!analyserNode) return 0
  analyserNode.getByteTimeDomainData(buffer)
  let sum = 0
  for (let i = 0; i < buffer.length; i++) {
    const sample = (buffer[i] - 128) / 128
    sum += sample * sample
  }
  const rms = Math.sqrt(sum / buffer.length)

  // Map RMS to calibrated 0-100% volume
  // Ambient room noise: rms < 0.005
  // Quiet background speech: rms 0.01 - 0.04 (approx 25-45%)
  // Normal speaking into mic: rms 0.05 - 0.20 (approx 50-80%)
  // Loud speaking: rms > 0.25 (approx 90-100%)
  if (rms < 0.002) return 0
  const db = 20 * Math.log10(rms) // roughly -60dB to 0dB
  const minDb = -60
  const maxDb = -10
  const pct = Math.round(((db - minDb) / (maxDb - minDb)) * 100)
  return Math.max(0, Math.min(100, pct))
}

export function useWebRTC() {
  const voiceStore = useVoiceStore()
  const chatStore = useChatStore()

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

  function getAudioConstraints() {
    const audioConstraints = {
      channelCount: 1,
      sampleRate: 48000,
      echoCancellation: voiceStore.echoCancellation,
      noiseSuppression: voiceStore.noiseCancelling,
      autoGainControl: voiceStore.autoGainControl, // Crucial: false prevents boosting background voice
      googEchoCancellation: voiceStore.echoCancellation,
      googAutoGainControl: voiceStore.autoGainControl,
      googNoiseSuppression: voiceStore.noiseCancelling,
      googHighpassFilter: true
    }
    if (voiceStore.selectedInputDeviceId) {
      audioConstraints.deviceId = { exact: voiceStore.selectedInputDeviceId }
    }
    return audioConstraints
  }

  async function startMicTest() {
    // If we're already connected to a voice channel, ensure audioContext is active
    if (localAudioStream.value && audioContext) {
      if (audioContext.state === 'suspended') {
        await audioContext.resume().catch(() => {})
      }
      return
    }

    // Stop any existing test first
    stopMicTest()

    try {
      const constraints = getAudioConstraints()
      testAudioStream = await navigator.mediaDevices.getUserMedia({ audio: constraints })

      const AudioCtx = window.AudioContext || window.webkitAudioContext
      testAudioContext = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
      if (testAudioContext.state === 'suspended') {
        await testAudioContext.resume().catch(() => {})
      }

      testAnalyser = testAudioContext.createAnalyser()
      testAnalyser.fftSize = 256
      testAnalyser.smoothingTimeConstant = 0.2

      const source = testAudioContext.createMediaStreamSource(testAudioStream)
      const biquad = testAudioContext.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, testAudioContext.currentTime)

      source.connect(biquad)
      biquad.connect(testAnalyser)

      const buffer = new Uint8Array(testAnalyser.fftSize)

      testSpeakingInterval = setInterval(() => {
        if (!testAnalyser) return
        voiceStore.currentInputLevel = calculateRMSLevel(testAnalyser, buffer)
      }, 50)

      // Refresh devices after permission is granted so device labels are available
      await refreshAudioDevices()
    } catch (err) {
      console.warn('Mic test failed (permission denied or no device):', err)
      voiceStore.currentInputLevel = 0
    }
  }

  function stopMicTest() {
    if (testSpeakingInterval) {
      clearInterval(testSpeakingInterval)
      testSpeakingInterval = null
    }
    if (testAudioStream) {
      testAudioStream.getTracks().forEach(t => t.stop())
      testAudioStream = null
    }
    if (testAudioContext) {
      testAudioContext.close().catch(() => {})
      testAudioContext = null
    }
    testAnalyser = null

    // If not in voice, reset input level
    if (!localAudioStream.value) {
      voiceStore.currentInputLevel = 0
    }
  }

  function cleanupVoiceAudio() {
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

    if (analysisTrack) {
      analysisTrack.stop()
      analysisTrack = null
    }

    if (audioContext) {
      audioContext.close().catch(() => {})
      audioContext = null
    }
    analyser = null
    voiceStore.currentInputLevel = 0
  }

  watch([() => voiceStore.outputVolume, () => voiceStore.isDeafened], () => {
    updateRemoteVolume(voiceStore)
  })

  async function handleRemoteOffer(offer) {
    if (!pc) {
      setupPeerConnection(voiceStore, chatStore)
    }

    if (localAudioStream.value) {
      const audioTrack = localAudioStream.value.getAudioTracks()[0]
      const hasAudioSender = pc.getSenders().some(s => s.track && s.track.kind === 'audio')
      if (audioTrack && !hasAudioSender) {
        pc.addTrack(audioTrack, localAudioStream.value)
      }
    }

    try {
      await pc.setRemoteDescription(new RTCSessionDescription(offer))
      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      chatStore.sendWSEvent('webrtc_answer', answer)
    } catch (err) {
      console.warn('[WebRTC] Offer/Answer negotiation error:', err)
    }
  }

  async function handleRemoteCandidate(candidate) {
    if (pc && candidate) {
      try {
        await pc.addIceCandidate(new RTCIceCandidate(candidate))
      } catch (err) {
        console.warn('[WebRTC] ICE candidate error:', err)
      }
    }
  }

  async function joinVoiceChannel(channelId) {
    // If already in this channel, don't re-create audio graphs
    if (channelId === voiceStore.currentChannelId && localAudioStream.value) {
      return
    }

    // Stop standalone mic test before joining voice
    stopMicTest()

    // Clean up any previous voice session to prevent audio thread leak
    cleanupVoiceAudio()

    chatStore.setWebRTCHandlers({
      onOffer: handleRemoteOffer,
      onCandidate: handleRemoteCandidate
    })

    voiceStore.setChannel(channelId)

    try {
      await refreshAudioDevices()

      localAudioStream.value = await navigator.mediaDevices.getUserMedia({
        audio: getAudioConstraints()
      })

      voiceStore.localAudioStream = localAudioStream.value

      setupPeerConnection(voiceStore, chatStore)
      chatStore.sendWSEvent('voice_join', { channel_id: channelId })

      await setupSpeakingDetection(localAudioStream.value)
      setupPttListeners()
    } catch (err) {
      console.warn('Microphone access denied or unavailable:', err)
      setupPeerConnection(voiceStore, chatStore)
      chatStore.sendWSEvent('voice_join', { channel_id: channelId })
    }
  }

  async function setupSpeakingDetection(stream) {
    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext
      audioContext = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
      if (audioContext.state === 'suspended') {
        await audioContext.resume().catch(() => {})
      }

      analyser = audioContext.createAnalyser()
      analyser.fftSize = 256
      analyser.smoothingTimeConstant = 0.2

      analysisTrack = stream.getAudioTracks()[0].clone()
      analysisTrack.enabled = true
      const source = audioContext.createMediaStreamSource(new MediaStream([analysisTrack]))
      const biquad = audioContext.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, audioContext.currentTime)

      source.connect(biquad)
      biquad.connect(analyser)

      const buffer = new Uint8Array(analyser.fftSize)
      let wasSpeaking = false

      speakingInterval = setInterval(() => {
        if (!analyser) return

        const level = calculateRMSLevel(analyser, buffer)

        // PERFORMANCE FIX: Only update reactive Pinia store if settings modal is open
        if (voiceStore.showAudioSettings) {
          voiceStore.currentInputLevel = level
        }

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

        // Noise Gate & Sensitivity Evaluation with Hysteresis
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
      }, 60)
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

      if (pc && videoTrack) {
        const videoSender = pc.getSenders().find(s => (s.track && s.track.kind === 'video') || (!s.track && s.kind === 'video'))
        if (videoSender) {
          await videoSender.replaceTrack(videoTrack)
        } else {
          pc.addTrack(videoTrack, localScreenStream.value)
        }
        chatStore.sendWSEvent('webrtc_request_keyframe', {})
      }

      videoTrack.onended = () => {
        stopScreenShare()
      }
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
    }
  }

  function stopScreenShare() {
    if (pc) {
      const videoSender = pc.getSenders().find(s => s.track && s.track.kind === 'video')
      if (videoSender) {
        videoSender.replaceTrack(null).catch(() => {})
      }
    }

    if (localScreenStream.value) {
      localScreenStream.value.getTracks().forEach(t => t.stop())
      localScreenStream.value = null
    }
    voiceStore.localScreenStream = null
    voiceStore.isScreenSharing = false
  }

  function leaveVoiceChannel() {
    cleanupPeerConnection(voiceStore)
    cleanupVoiceAudio()
    stopScreenShare()
    chatStore.sendWSEvent('voice_leave', {})
    voiceStore.disconnect()
  }

  return {
    localAudioStream,
    localScreenStream,
    refreshAudioDevices,
    startMicTest,
    stopMicTest,
    joinVoiceChannel,
    leaveVoiceChannel,
    startScreenShare,
    stopScreenShare
  }
}
