import { ref, watch } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { summarizeStats } from '../lib/rtcStats'
import * as voiceSession from '../lib/voiceSession'
import { createMeteringTrack } from '../lib/micMetering'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported, preloadNoiseSuppressor } from '../lib/noiseSuppressor'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// The SFU publishes a camera under this prefix plus the user ID as stream ID
// (screen share and audio use the plain user ID); see internal/sfu.
const CAMERA_STREAM_PREFIX = 'cam:'

// Module-level shared singletons across all components
const localAudioStream = ref(null)
const localScreenStream = ref(null)
const localCameraStream = ref(null)
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
// Screen audio mixed with the microphone into one sent track:
// { ctx, ownsContext, screenTrack, screenSource, micSource, destination }.
// The gate and mute switch the *mic track* off (track.enabled); a disabled
// track feeds silence into its source node, so only the mic part of the mix
// goes quiet while the screen audio keeps playing for the viewers.
let mix = null
let speakingInterval = null
let lastAboveThresholdTime = 0

// WebRTC Peer Connection and Remote Audio
let pc = null
let remoteAudioElements = []
// Senders of the three fixed outgoing lines, created with the connection:
// mic (or mix), screen share and camera. Tracks are swapped with replaceTrack,
// so starting or stopping them never renegotiates.
let audioSender = null
let screenSender = null
let cameraSender = null
// Playback above 100 % needs a gain stage: element.volume tops out at 1.
let playbackContext = null
const boosts = new WeakMap()
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

// Linear gain for one remote audio element: master volume x that user's volume.
function remoteGain(voiceStore, el) {
  if (voiceStore.isDeafened) return 0
  const master = voiceStore.outputVolume / 100
  const userId = el.dataset?.userId
  if (!userId) return master
  if (voiceStore.isUserLocalMuted(userId)) return 0
  return master * (voiceStore.getUserVolume(userId) / 100)
}

// Routes the element's stream through a GainNode (the element stays attached
// but muted so the stream keeps flowing). Only done once a gain above 1 is
// asked for; until then plain element.volume does the job.
function createBoost(el) {
  const AudioCtx = typeof window !== 'undefined' && (window.AudioContext || window.webkitAudioContext)
  if (!AudioCtx || !el.srcObject) return null
  try {
    if (!playbackContext || playbackContext.state === 'closed') {
      playbackContext = new AudioCtx()
    }
    if (playbackContext.state === 'suspended') playbackContext.resume().catch(() => {})
    const source = playbackContext.createMediaStreamSource(el.srcObject)
    const gain = playbackContext.createGain()
    source.connect(gain)
    gain.connect(playbackContext.destination)
    el.muted = true
    const boost = { source, gain }
    boosts.set(el, boost)
    return boost
  } catch (err) {
    console.warn('[WebRTC] Volume boost unavailable:', err)
    return null
  }
}

function applyRemoteGain(voiceStore, el) {
  const gain = remoteGain(voiceStore, el)
  let boost = boosts.get(el)
  if (!boost && gain > 1) boost = createBoost(el)
  if (boost) {
    boost.gain.gain.value = gain
  } else {
    el.volume = Math.min(1, Math.max(0, gain))
  }
}

function releaseBoost(el) {
  const boost = boosts.get(el)
  if (!boost) return
  try {
    boost.source.disconnect()
    boost.gain.disconnect()
  } catch { /* already disconnected */ }
  boosts.delete(el)
}

function updateRemoteVolume(voiceStore) {
  remoteAudioElements.forEach(el => applyRemoteGain(voiceStore, el))
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
    releaseBoost(el)
    try {
      el.pause()
      el.srcObject = null
    } catch {
      // element already detached / disposed
    }
  })
  remoteAudioElements = []
  if (playbackContext) {
    playbackContext.close().catch(() => {})
    playbackContext = null
  }
  audioSender = null
  screenSender = null
  cameraSender = null
  voiceStore?.resetRemoteMedia()
}

// The track that goes out as "audio": the mic/screen mix while sharing screen
// audio, otherwise the (processed) microphone.
function activeAudioTrack() {
  return mix?.destination.stream.getAudioTracks()[0] || localAudioStream.value?.getAudioTracks()[0] || null
}

function activeAudioStream() {
  return mix?.destination.stream || localAudioStream.value
}

function teardownScreenAudioMix() {
  if (!mix) return
  const m = mix
  mix = null
  m.screenTrack.onended = null
  for (const node of [m.screenSource, m.micSource]) {
    try { node?.disconnect() } catch { /* already disconnected */ }
  }
  try { m.destination.disconnect?.() } catch { /* ignore */ }
  m.destination.stream.getTracks().forEach(tr => tr.stop())
  if (m.ownsContext) m.ctx.close().catch(() => {})
}

// Mixes the screen's audio track with the microphone in the call's audio
// context (or a private one when the call has no mic).
function buildScreenAudioMix(screenTrack) {
  teardownScreenAudioMix()
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  let ctx = audioContext
  let ownsContext = false
  try {
    if (!ctx || ctx.state === 'closed') {
      ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
      ownsContext = true
    }
    if (ctx.state === 'suspended') ctx.resume().catch(() => {})
    const destination = ctx.createMediaStreamDestination()
    const screenSource = ctx.createMediaStreamSource(new MediaStream([screenTrack]))
    screenSource.connect(destination)
    let micSource = null
    if (localAudioStream.value) {
      micSource = ctx.createMediaStreamSource(localAudioStream.value)
      micSource.connect(destination)
    }
    mix = { ctx, ownsContext, screenTrack, screenSource, micSource, destination }
    return mix
  } catch (err) {
    console.warn('[WebRTC] Screen audio mixing unavailable:', err)
    return null
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
    // The SFU names the publishing user in the stream ID.
    const streamId = event.streams?.[0]?.id
    if (event.track.kind === 'audio') {
      const audioEl = new Audio()
      audioEl.srcObject = new MediaStream([event.track])
      audioEl.autoplay = true
      if (streamId) audioEl.dataset.userId = streamId
      applyRemoteGain(voiceStore, audioEl)
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
        releaseBoost(audioEl)
        audioEl.srcObject = null
        try { audioEl.remove() } catch { /* detached */ }
        remoteAudioElements = remoteAudioElements.filter(a => a !== audioEl)
      }
    } else if (event.track.kind === 'video') {
      const stream = new MediaStream([event.track])
      const isCamera = !!streamId?.startsWith(CAMERA_STREAM_PREFIX)
      const userId = isCamera ? streamId.slice(CAMERA_STREAM_PREFIX.length) : streamId
      let drop
      if (isCamera) {
        if (userId) voiceStore.setUserVideoStream(userId, stream)
        drop = () => { if (userId) voiceStore.removeUserVideoStream(userId, stream) }
      } else {
        if (userId) voiceStore.setRemoteScreen(userId, stream)
        drop = () => { if (userId) voiceStore.removeRemoteScreen(userId, stream) }
      }
      chatStore.sendWSEvent('webrtc_request_keyframe', {})

      event.track.onended = drop
      // When the SFU stops forwarding, the browser removes the track from its
      // stream instead of ending it.
      event.streams?.[0]?.addEventListener?.('removetrack', (e) => {
        if (e.track === event.track) drop()
      })
    }
  }

  // The three outgoing lines. They exist from the start (even without a
  // track) so that screen share and camera begin with replaceTrack only.
  const audioTrack = activeAudioTrack()
  audioSender = audioTrack
    ? pc.addTrack(audioTrack, activeAudioStream())
    : pc.addTransceiver('audio', { direction: 'sendrecv' }).sender
  screenSender = addVideoSender(pc, localScreenStream.value)
  cameraSender = addVideoSender(pc, localCameraStream.value)

  return pc
}

// Sender limits (bits per second). The browser's own congestion control still
// lowers the bitrate on a weak uplink; these decide what it gives up while
// doing so and cap the ceiling. A shared screen keeps its resolution and drops
// frames instead (text stays readable); a camera may trade either.
export const SCREEN_MAX_BITRATE = 12_000_000
export const CAMERA_MAX_BITRATE = 2_500_000

async function tuneSender(sender, { maxBitrate, degradationPreference }) {
  try {
    const params = sender.getParameters?.()
    if (!params) return
    if (!params.encodings?.length) params.encodings = [{}]
    params.encodings[0].maxBitrate = maxBitrate
    if (degradationPreference) params.degradationPreference = degradationPreference
    await sender.setParameters(params)
  } catch (err) {
    // Not every browser accepts every field; the defaults still work.
    console.debug('[WebRTC] Could not tune sender:', err)
  }
}

function addVideoSender(conn, stream) {
  const track = stream?.getVideoTracks()[0]
  if (track) return conn.addTransceiver(track, { direction: 'sendrecv', streams: [stream] }).sender
  return conn.addTransceiver('video', { direction: 'sendrecv' }).sender
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

    teardownScreenAudioMix()
    teardownMicPipeline()
    voiceStore.currentInputLevel = 0
  }

  watch([
    () => voiceStore.outputVolume,
    () => voiceStore.isDeafened,
    () => voiceStore.userVolumes,
    () => voiceStore.localMutedUsers
  ], () => {
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

    const audioTrack = activeAudioTrack()
    if (audioTrack && !audioSender) {
      audioSender = pc.addTrack(audioTrack, activeAudioStream())
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
    attachSubscriptions()

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
    attachSubscriptions()
  }

  // Which video I receive is decided on the server: tell it my camera opt-outs
  // and screen opt-ins after every join, and forward later changes.
  function attachSubscriptions() {
    voiceStore.setSubscriptionSink(msg => chatStore.sendWSEvent('webrtc_subscribe', msg))
    voiceStore.resendSubscriptions()
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
    const old = localAudioStream.value
    // The mix lives in the old audio context: rebuilt on the new one below.
    const screenAudio = mix?.screenTrack
    teardownScreenAudioMix()
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
    if (screenAudio && screenAudio.readyState !== 'ended') {
      if (buildScreenAudioMix(screenAudio)) screenAudio.onended = detachScreenAudio
    }
    const outgoing = activeAudioTrack()
    if (audioSender && outgoing) await audioSender.replaceTrack(outgoing).catch(() => {})
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

  // Screen audio ended (or sharing stopped): back to the plain microphone.
  async function detachScreenAudio() {
    if (!mix) return
    teardownScreenAudioMix()
    if (audioSender) await audioSender.replaceTrack(localAudioStream.value?.getAudioTracks()[0] ?? null).catch(() => {})
  }

  async function startScreenShare() {
    let stream
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({
        video: {
          frameRate: { ideal: 60, max: 60 },
          width: { ideal: 3840, max: 3840 },
          height: { ideal: 2160, max: 2160 }
        },
        audio: true
      })
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
      return
    }
    localScreenStream.value = stream
    voiceStore.localScreenStream = stream
    voiceStore.isScreenSharing = true

    const videoTrack = stream.getVideoTracks()[0]
    if (videoTrack && 'contentHint' in videoTrack) {
      videoTrack.contentHint = 'detail'
    }
    if (videoTrack) videoTrack.onended = () => stopScreenShare()

    if (screenSender && videoTrack) {
      await screenSender.replaceTrack(videoTrack).catch(() => {})
      await tuneSender(screenSender, { maxBitrate: SCREEN_MAX_BITRATE, degradationPreference: 'maintain-resolution' })
      chatStore.sendWSEvent('webrtc_request_keyframe', {})
    }

    // Share the screen's sound too: one mixed track replaces the mic track.
    const screenAudio = stream.getAudioTracks()[0]
    if (screenAudio && buildScreenAudioMix(screenAudio)) {
      screenAudio.onended = detachScreenAudio
      const mixed = activeAudioTrack()
      if (audioSender && mixed) await audioSender.replaceTrack(mixed).catch(() => {})
    }
  }

  function stopScreenShare() {
    const wasSharing = !!localScreenStream.value
    if (screenSender) screenSender.replaceTrack(null).catch(() => {})
    detachScreenAudio()

    if (localScreenStream.value) {
      localScreenStream.value.getTracks().forEach(tr => tr.stop())
      localScreenStream.value = null
    }
    voiceStore.localScreenStream = null
    voiceStore.isScreenSharing = false
    if (wasSharing) chatStore.sendWSEvent('webrtc_screenshare_stop', {})
  }

  function toggleScreenShare() {
    return voiceStore.isScreenSharing ? stopScreenShare() : startScreenShare()
  }

  // Tells the user why the camera failed instead of always blaming permissions.
  function cameraErrorKey(err) {
    switch (err?.name) {
      case 'NotAllowedError':
      case 'SecurityError':
        return 'voice.cameraDenied'
      case 'NotFoundError':
      case 'OverconstrainedError':
        return 'voice.cameraNotFound'
      case 'NotReadableError':
      case 'AbortError':
        return 'voice.cameraBusy'
      default:
        return 'voice.cameraFailed'
    }
  }

  async function startCamera() {
    if (!voiceStore.currentChannelId || localCameraStream.value) return
    const gen = joinGeneration
    let stream
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: {
          width: { ideal: 1280 },
          height: { ideal: 720 },
          frameRate: { ideal: 30, max: 30 }
        }
      })
    } catch (err) {
      console.warn('Camera unavailable:', err)
      useToastStore().error(t(cameraErrorKey(err)))
      return
    }
    // Left the call (or toggled twice) while the browser was asking.
    if (gen !== joinGeneration || localCameraStream.value) {
      stream.getTracks().forEach(tr => tr.stop())
      return
    }
    localCameraStream.value = stream
    voiceStore.localCameraStream = stream
    voiceStore.isCameraOn = true

    const track = stream.getVideoTracks()[0]
    if (track) {
      if ('contentHint' in track) track.contentHint = 'motion'
      track.onended = () => stopCamera()
      if (cameraSender) {
        await cameraSender.replaceTrack(track).catch(() => {})
        await tuneSender(cameraSender, { maxBitrate: CAMERA_MAX_BITRATE, degradationPreference: 'balanced' })
        chatStore.sendWSEvent('webrtc_request_keyframe', {})
      }
    }
  }

  function stopCamera() {
    const wasOn = !!localCameraStream.value
    if (cameraSender) cameraSender.replaceTrack(null).catch(() => {})
    if (localCameraStream.value) {
      localCameraStream.value.getTracks().forEach(tr => tr.stop())
      localCameraStream.value = null
    }
    voiceStore.localCameraStream = null
    voiceStore.isCameraOn = false
    if (wasOn) chatStore.sendWSEvent('webrtc_camera_stop', {})
  }

  function toggleCamera() {
    return voiceStore.isCameraOn ? stopCamera() : startCamera()
  }

  function leaveVoiceChannel() {
    joinGeneration++
    voiceSession.forget()
    cleanupPeerConnection(voiceStore)
    cleanupVoiceAudio()
    stopScreenShare()
    stopCamera()
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
    playbackContext?.resume?.().catch(() => {})
    remoteAudioElements.forEach(el => el.play().catch(() => { voiceStore.audioBlocked = true }))
  }

  return {
    localAudioStream,
    localScreenStream,
    localCameraStream,
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
    startCamera,
    stopCamera,
    toggleCamera,
    resumeVoiceSession,
    resumeRemoteAudio
  }
}
