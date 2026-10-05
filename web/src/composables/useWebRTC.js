import { ref, watch } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { summarizeStats } from '../lib/rtcStats'
import * as voiceSession from '../lib/voiceSession'
import { createMeteringTrack } from '../lib/micMetering'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported, preloadNoiseSuppressor } from '../lib/noiseSuppressor'

// Module-level shared singletons across all components
const localAudioStream = ref(null)
const localScreenStream = ref(null)
let audioContext = null
let analyser = null
// Always-enabled clone of the mic track that only feeds the level meter (see lib/micMetering).
let meteringTrack = null
// With AI noise suppression the captured mic stream only feeds the model;
// localAudioStream is then the processed stream that is sent, gated and muted.
let rawMicStream = null
let suppressorNode = null

// Noise mode -> model in lib/noiseSuppressor (null: no AI filter).
function aiModel(noiseMode) {
  return { ai: 'dfn3', 'ai-lite': 'gtcrn' }[noiseMode] ?? null
}
let speakingInterval = null
let lastAboveThresholdTime = 0

// WebRTC Peer Connection and Remote Audio
let pc = null
let remoteAudioElements = []
// Offers are applied one after another; candidates that arrive before the
// remote description is set wait in pendingCandidates.
let signalingChain = Promise.resolve()
let pendingCandidates = []
// Bumped by every join and leave: an older join that is still awaiting the
// microphone sees the change and backs out instead of finishing.
let joinGeneration = 0
// Push-to-talk listeners are module-level so any component can remove them.
let pttStore = null

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let iceServers = []
let iceConfigLoaded = null

// Loaded once, on the first voice join rather than at import time.
function loadIceServers() {
  if (!iceConfigLoaded) {
    iceConfigLoaded = fetch('/api/webrtc/config', { credentials: 'same-origin' })
      .then(res => (res.ok ? res.json() : null))
      .then(data => { if (data?.ice_servers) iceServers = data.ice_servers })
      .catch(() => { iceConfigLoaded = null })
  }
  return iceConfigLoaded
}

// Connection statistics measured by the browser (RTCPeerConnection.getStats).
let statsTimer = null
let lastStatsSample = null

function startStatsPolling(voiceStore) {
  stopStatsPolling(voiceStore)
  statsTimer = setInterval(async () => {
    if (!pc) return
    try {
      const report = await pc.getStats()
      const summary = summarizeStats(report.values(), lastStatsSample)
      lastStatsSample = summary.sample
      voiceStore.rtcStats = summary
    } catch {
      // The connection may be closing; the next tick retries.
    }
  }, 2000)
}

function stopStatsPolling(voiceStore) {
  if (statsTimer) {
    clearInterval(statsTimer)
    statsTimer = null
  }
  lastStatsSample = null
  if (voiceStore) voiceStore.rtcStats = null
}

function updateRemoteVolume(voiceStore) {
  const vol = voiceStore.isDeafened ? 0 : (voiceStore.outputVolume / 100)
  remoteAudioElements.forEach(el => {
    el.volume = vol
  })
}

function cleanupPeerConnection(voiceStore) {
  stopStatsPolling(voiceStore)
  signalingChain = Promise.resolve()
  pendingCandidates = []
  if (pc) {
    try { pc.close() } catch { /* connection already closed */ }
    pc = null
  }
  remoteAudioElements.forEach(el => {
    try {
      el.pause()
      el.srcObject = null
    } catch {
      // element already detached / disposed
    }
  })
  remoteAudioElements = []
  if (voiceStore) {
    voiceStore.remoteScreenStream = null
  }
}

function setupPeerConnection(voiceStore, chatStore) {
  cleanupPeerConnection(voiceStore)

  pc = new RTCPeerConnection({ iceServers })
  startStatsPolling(voiceStore)

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
      audioEl.play().catch(e => {
        // After a reload without a user gesture the browser may refuse to play;
        // the UI then offers a click to enable sound (resumeRemoteAudio).
        if (e?.name === 'NotAllowedError') voiceStore.audioBlocked = true
        console.warn('[WebRTC] Audio auto-play warning:', e)
      })
      if (typeof document !== 'undefined' && document.body) {
        let sink = document.getElementById('mnema-audio-sink')
        if (!sink) {
          sink = document.createElement('div')
          sink.id = 'mnema-audio-sink'
          sink.style.display = 'none'
          document.body.appendChild(sink)
        }
        sink.appendChild(audioEl)
      }
      remoteAudioElements.push(audioEl)

      event.track.onended = () => {
        audioEl.srcObject = null
        try { audioEl.remove() } catch { /* detached */ }
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

function isTypingTarget(el) {
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}

function handlePttKeyDown(e) {
  const voiceStore = pttStore
  if (!voiceStore || voiceStore.inputMode !== 'ptt' || e.repeat) return
  // Holding the key while typing in the composer must not open the mic.
  if (isTypingTarget(e.target)) return
  if ((e.code || e.key) === voiceStore.pttKey) voiceStore.isPttPressed = true
}

function handlePttKeyUp(e) {
  const voiceStore = pttStore
  if (!voiceStore || voiceStore.inputMode !== 'ptt') return
  if ((e.code || e.key) === voiceStore.pttKey) voiceStore.isPttPressed = false
}

// A keyup that happens while the window is in the background never arrives;
// release the key so the mic does not stay open.
function releasePtt() {
  if (pttStore) pttStore.isPttPressed = false
}

function onVisibilityChange() {
  if (document.visibilityState === 'hidden') {
    releasePtt()
  } else if (document.visibilityState === 'visible') {
    if (audioContext && audioContext.state === 'suspended') {
      audioContext.resume().catch(() => {})
    }
    if (pttStore?.isConnected && pttStore?.currentChannelId) {
      remoteAudioElements.forEach(el => {
        if (el.paused) el.play().catch(() => {})
      })
    }
  }
}

function setupPttListeners(voiceStore) {
  removePttListeners()
  pttStore = voiceStore
  window.addEventListener('keydown', handlePttKeyDown)
  window.addEventListener('keyup', handlePttKeyUp)
  window.addEventListener('blur', releasePtt)
  document.addEventListener('visibilitychange', onVisibilityChange)
}

function removePttListeners() {
  window.removeEventListener('keydown', handlePttKeyDown)
  window.removeEventListener('keyup', handlePttKeyUp)
  window.removeEventListener('blur', releasePtt)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  releasePtt()
  pttStore = null
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

  // The AI filter wants the unprocessed signal; without worklet support the
  // browser's own suppression stands in for it.
  function useBrowserNoiseSuppression() {
    const mode = voiceStore.noiseMode
    return mode === 'browser' || (aiModel(mode) !== null && !isNoiseSuppressionSupported())
  }

  function getAudioConstraints() {
    const audioConstraints = {
      channelCount: 1,
      sampleRate: 48000,
      echoCancellation: voiceStore.echoCancellation,
      noiseSuppression: useBrowserNoiseSuppression(),
      autoGainControl: voiceStore.autoGainControl, // Crucial: false prevents boosting background voice
      googEchoCancellation: voiceStore.echoCancellation,
      googAutoGainControl: voiceStore.autoGainControl,
      googNoiseSuppression: useBrowserNoiseSuppression(),
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

      const ctx = testAudioContext
      let source = ctx.createMediaStreamSource(testAudioStream)
      // Meter the filtered signal so the gate threshold calibrates like in a call.
      const model = aiModel(voiceStore.noiseMode)
      if (model) {
        const node = await createNoiseSuppressorNode(ctx, model)
        if (testAudioContext !== ctx) return
        if (node) {
          source.connect(node)
          source = node
        }
      }
      const biquad = ctx.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, ctx.currentTime)

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

    teardownMicPipeline()
    voiceStore.currentInputLevel = 0
  }

  watch([() => voiceStore.outputVolume, () => voiceStore.isDeafened], () => {
    updateRemoteVolume(voiceStore)
  })

  // Offers are queued so overlapping renegotiations never interleave.
  function handleRemoteOffer(offer) {
    signalingChain = signalingChain.then(() => applyRemoteOffer(offer))
    return signalingChain
  }

  async function applyRemoteOffer(offer) {
    // A late offer after leaving must not resurrect a connection.
    if (!voiceStore.currentChannelId) return
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
      const queued = pendingCandidates
      pendingCandidates = []
      for (const c of queued) await pc.addIceCandidate(new RTCIceCandidate(c)).catch(() => {})
      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      chatStore.sendWSEvent('webrtc_answer', answer)
    } catch (err) {
      console.warn('[WebRTC] Offer/Answer negotiation error:', err)
    }
  }

  async function handleRemoteCandidate(candidate) {
    if (!candidate || !voiceStore.currentChannelId) return
    if (!pc || !pc.remoteDescription) {
      pendingCandidates.push(candidate)
      return
    }
    {
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

    const gen = ++joinGeneration
    voiceStore.setChannel(channelId)
    voiceSession.track(channelId)
    await loadIceServers()
    if (gen !== joinGeneration) return

    const model = aiModel(voiceStore.noiseMode)
    if (model) preloadNoiseSuppressor(model)
    let stream = null
    try {
      await refreshAudioDevices()
      stream = await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints() })
    } catch (err) {
      console.warn('Microphone access denied or unavailable:', err)
    }
    // Left (or joined elsewhere) while the browser was asking for the mic.
    if (gen !== joinGeneration) {
      stream?.getTracks().forEach(t => t.stop())
      return
    }

    const sendStream = stream ? await setupMicPipeline(stream) : null
    if (gen !== joinGeneration) return

    localAudioStream.value = sendStream
    voiceStore.localAudioStream = sendStream
    setupPeerConnection(voiceStore, chatStore)
    chatStore.sendWSEvent('voice_join', { channel_id: channelId })

    if (sendStream) {
      startSpeakingDetection()
      setupPttListeners(voiceStore)
    }
  }

  // After the WebSocket reconnected the server may have dropped our media
  // peer; start a fresh connection and announce the join again. The server
  // treats it as a resume (no leave/join for the others) within its grace time.
  function rejoinAfterReconnect() {
    const channelId = voiceStore.currentChannelId
    if (!channelId) return
    setupPeerConnection(voiceStore, chatStore)
    chatStore.sendWSEvent('voice_join', { channel_id: channelId })
  }

  // Applies changed input settings (device, AGC, noise suppression, echo
  // cancellation). In a call the mic is re-acquired and swapped into the
  // running connection without renegotiation; otherwise the mic test restarts.
  async function applyAudioSettings() {
    if (!localAudioStream.value) return startMicTest()
    const gen = joinGeneration
    let stream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints() })
    } catch (err) {
      console.warn('Could not switch microphone:', err)
      return
    }
    if (gen !== joinGeneration || !localAudioStream.value) {
      stream.getTracks().forEach(t => t.stop())
      return
    }
    const sender = pc?.getSenders().find(s => s.track?.kind === 'audio')
    const old = localAudioStream.value
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }
    teardownMicPipeline()
    old.getTracks().forEach(t => t.stop())
    const sendStream = await setupMicPipeline(stream)
    if (gen !== joinGeneration || !sendStream) return
    const track = sendStream.getAudioTracks()[0]
    track.enabled = !voiceStore.isMuted
    localAudioStream.value = sendStream
    voiceStore.localAudioStream = sendStream
    if (sender && track) await sender.replaceTrack(track).catch(() => {})
    startSpeakingDetection()
  }

  // Creates the audio context for a freshly captured mic stream and returns
  // the stream to send. With an AI filter that is the model's output: the raw
  // capture stays enabled and only feeds the model, so the meter (tapping the
  // model output) keeps working while the gate or mute disables the sent
  // track, and typing or clicks don't count as speaking. Otherwise the raw
  // stream is sent and metered through an always-enabled clone (see
  // lib/micMetering).
  async function setupMicPipeline(stream) {
    const AudioCtx = window.AudioContext || window.webkitAudioContext
    let ctx
    try {
      ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
    } catch (err) {
      console.warn('AudioContext setup error:', err)
      return stream
    }
    audioContext = ctx
    if (ctx.state === 'suspended') {
      await ctx.resume().catch(() => {})
    }

    let sendStream = stream
    let meterSource = null
    const model = aiModel(voiceStore.noiseMode)
    if (model) {
      const node = await createNoiseSuppressorNode(ctx, model)
      // Torn down (left or settings changed again) while the model loaded.
      if (audioContext !== ctx) {
        stream.getTracks().forEach(t => t.stop())
        return null
      }
      if (node) {
        const destination = ctx.createMediaStreamDestination()
        ctx.createMediaStreamSource(stream).connect(node)
        node.connect(destination)
        suppressorNode = node
        rawMicStream = stream
        sendStream = destination.stream
        meterSource = node
      }
    }

    try {
      analyser = ctx.createAnalyser()
      analyser.fftSize = 256
      analyser.smoothingTimeConstant = 0.2
      if (!meterSource) {
        meteringTrack = createMeteringTrack(stream)
        meterSource = ctx.createMediaStreamSource(meteringTrack ? new MediaStream([meteringTrack]) : stream)
      }
      const biquad = ctx.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, ctx.currentTime)
      meterSource.connect(biquad)
      biquad.connect(analyser)
    } catch (err) {
      console.warn('AudioContext speaking detector setup error:', err)
    }
    return sendStream
  }

  function teardownMicPipeline() {
    if (suppressorNode) {
      try {
        suppressorNode.disconnect()
        suppressorNode.destroy?.()
      } catch { /* ignore */ }
      suppressorNode = null
    }
    if (rawMicStream) {
      rawMicStream.getTracks().forEach(t => t.stop())
      rawMicStream = null
    }
    if (meteringTrack) {
      meteringTrack.stop()
      meteringTrack = null
    }
    if (audioContext) {
      audioContext.close().catch(() => {})
      audioContext = null
    }
    analyser = null
  }

  function startSpeakingDetection() {
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }
    if (!analyser) return

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
      // Every branch below assigns it, so no initial value is needed.
      let shouldTransmit

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
        const videoTransceiver = pc.getTransceivers().find(t => t.receiver?.track?.kind === 'video')
        const videoSender = videoTransceiver?.sender || pc.getSenders().find(s => s.track && s.track.kind === 'video')
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
      const videoTransceiver = pc.getTransceivers().find(t => t.receiver?.track?.kind === 'video')
      const videoSender = videoTransceiver?.sender || pc.getSenders().find(s => s.track && s.track.kind === 'video')
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
    chatStore.sendWSEvent('webrtc_screenshare_stop', {})
  }

  function toggleScreenShare() {
    return voiceStore.isScreenSharing ? stopScreenShare() : startScreenShare()
  }

  function leaveVoiceChannel() {
    joinGeneration++
    voiceSession.forget()
    cleanupPeerConnection(voiceStore)
    cleanupVoiceAudio()
    stopScreenShare()
    chatStore.sendWSEvent('voice_leave', {})
    voiceStore.disconnect()
    voiceStore.audioBlocked = false
  }

  // Rejoins the channel of a call interrupted by a reload less than 30 s ago.
  // Must run once the WebSocket is connected (voice_join goes over it).
  async function resumeVoiceSession() {
    const channelId = voiceSession.recent()
    if (!channelId || voiceStore.currentChannelId) return false
    const exists = chatStore.categories.some(c => (c.channels || []).some(ch => ch.id === channelId && ch.type === 'voice')) ||
      chatStore.uncategorized.some(ch => ch.id === channelId && ch.type === 'voice')
    if (!exists) {
      voiceSession.forget()
      return false
    }
    await joinVoiceChannel(channelId)
    return true
  }

  // Called from a user click when autoplay was blocked after a reload.
  function resumeRemoteAudio() {
    voiceStore.audioBlocked = false
    remoteAudioElements.forEach(el => el.play().catch(() => { voiceStore.audioBlocked = true }))
  }

  return {
    localAudioStream,
    localScreenStream,
    refreshAudioDevices,
    startMicTest,
    stopMicTest,
    joinVoiceChannel,
    leaveVoiceChannel,
    rejoinAfterReconnect,
    applyAudioSettings,
    startScreenShare,
    stopScreenShare,
    toggleScreenShare,
    resumeVoiceSession,
    resumeRemoteAudio
  }
}
