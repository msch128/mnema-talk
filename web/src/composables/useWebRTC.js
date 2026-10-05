import { ref, watch, effectScope } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { summarizeStats } from '../lib/rtcStats'
import * as voiceSession from '../lib/voiceSession'
import { createMeteringTrack } from '../lib/micMetering'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported, preloadNoiseSuppressor } from '../lib/noiseSuppressor'
import { useToastStore } from '../stores/toast'
import { playSound } from '../lib/soundEffects'
import { applyOutputDevice } from '../lib/audioOutput'
import { createVoiceGate } from '../lib/levelMeter'
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
// The mic-test monitor: a gain stage to the speakers on the context it was
// built on (the call's or the standalone test's).
let loopbackGain = null
let loopbackCtx = null
let callBiquad = null
let unmutedZeroAudioStartTime = 0
let lastNoAudioWarningToastTime = 0

// WebRTC Peer Connection and Remote Audio
let pc = null
// Remote audio elements by track id. The SFU reuses its transceivers: the
// same track may come back in a new stream (another speaker), so elements
// are keyed by track and retagged instead of piling up.
const remoteAudioElements = new Map()
// Senders of the three fixed outgoing lines, created with the connection:
// mic (or mix), screen share and camera. Tracks are swapped with replaceTrack,
// so starting or stopping them never renegotiates.
let audioSender = null
let screenSender = null
let cameraSender = null
// Playback above 100 % needs a gain stage: element.volume tops out at 1.
let playbackContext = null
// Input volume: a gain stage in the mic pipeline, only built when the input
// volume is not 100 % (so the default path stays the plain microphone).
let inputGainNode = null
// The voice store, for the output device of a lazily created playback context.
let outputDeviceStore = null
// The settings watchers of useWebRTC, registered once per voice store.
let watcherScope = null
let watchedStore = null
let inputVolumeTimer = null
// applyAudioSettings runs one at a time; busy counts queued and running ones.
let audioSettingsChain = Promise.resolve()
let audioSettingsBusy = 0
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

// Where stream diagnostics go (the server log, via the WebSocket).
let diagSink = null
let statsTicks = 0
const lastVideoBytes = new Map()

/**
 * What a shared screen looks like on the wire, for the server log: per video
 * stream its codec, size, frame rate, bitrate, losses and, when sending, why
 * the browser holds the quality back (cpu, bandwidth). Never addresses.
 */
export function streamDiag(stats, previousBytes = new Map(), seconds = 10) {
  const codecs = new Map()
  for (const s of stats) if (s.type === 'codec') codecs.set(s.id, s.mimeType)
  const out = []
  for (const s of stats) {
    if (s.kind !== 'video' || (s.type !== 'outbound-rtp' && s.type !== 'inbound-rtp')) continue
    const sending = s.type === 'outbound-rtp'
    const bytes = sending ? s.bytesSent : s.bytesReceived
    if (!bytes) continue
    const before = previousBytes.get(s.id)
    previousBytes.set(s.id, bytes)
    out.push({
      dir: sending ? 'out' : 'in',
      codec: codecs.get(s.codecId) || '',
      size: s.frameWidth ? `${s.frameWidth}x${s.frameHeight}` : '',
      fps: Math.round(s.framesPerSecond || 0),
      kbps: before === undefined ? null : Math.round(((bytes - before) * 8) / 1000 / seconds),
      limit: sending ? (s.qualityLimitationReason || '') : undefined,
      encoder: sending ? (s.encoderImplementation || '') : undefined,
      decoder: sending ? undefined : (s.decoderImplementation || ''),
      // Hardware (power efficient) encoder or decoder; Chrome reports it once
      // the page holds a capture permission, other browsers leave it out.
      hw: sending ? s.powerEfficientEncoder : s.powerEfficientDecoder,
      lost: s.packetsLost ?? undefined,
      dropped: sending ? undefined : (s.framesDropped ?? 0),
      freezes: sending ? undefined : (s.freezeCount ?? 0),
      nacks: s.nackCount ?? undefined,
      plis: s.pliCount ?? undefined
    })
  }
  return out
}

function startStatsPolling(voiceStore) {
  stopStatsPolling(voiceStore)
  statsTicks = 0
  lastVideoBytes.clear()
  statsTimer = setInterval(async () => {
    if (!pc) return
    try {
      const report = await pc.getStats()
      const summary = summarizeStats(report.values(), lastStatsSample)
      lastStatsSample = summary.sample
      voiceStore.rtcStats = summary
      // Every 10 s while a screen is shared or watched: what the stream does.
      if (++statsTicks % 5 === 0 && diagSink && (voiceStore.isScreenSharing || voiceStore.remoteScreenStream)) {
        const video = streamDiag([...report.values()], lastVideoBytes, 10)
        if (video.length) {
          diagSink({ event: 'stream', video, screen_audio: !!voiceStore.hasScreenAudio, own_audio: voiceStore.isScreenSharing ? screenAudioGuard : undefined, sharing: !!voiceStore.isScreenSharing })
        }
      }
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
  if (voiceStore.isDeafened || voiceStore.isMicTesting) return 0
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
      if (outputDeviceStore) applyOutput(outputDeviceStore, playbackContext)
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

// Plays remote audio (an element or an AudioContext) on the chosen output device.
function applyOutput(voiceStore, target) {
  applyOutputDevice(target, voiceStore.selectedOutputDeviceId)
}

function applyOutputDeviceAll(voiceStore) {
  remoteAudioElements.forEach(el => applyOutput(voiceStore, el))
  // Boosted voices (over 100 %) play through the playback context.
  applyOutput(voiceStore, playbackContext)
  // The mic-test monitor.
  applyOutput(voiceStore, loopbackCtx)
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
      el.remove()
    } catch {
      // element already detached / disposed
    }
  })
  remoteAudioElements.clear()
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

function teardownScreenAudioMix(voiceStore) {
  if (!mix) return
  const m = mix
  mix = null
  if (voiceStore) voiceStore.hasScreenAudio = false
  m.screenTrack.onended = null
  for (const node of [m.screenSource, m.screenGain, m.micSource]) {
    try { node?.disconnect() } catch { /* already disconnected */ }
  }
  try { m.destination.disconnect?.() } catch { /* ignore */ }
  m.destination.stream.getTracks().forEach(tr => tr.stop())
  if (m.ownsContext) m.ctx.close().catch(() => {})
}

// Mixes the screen's audio track with the microphone in the call's audio
// context (or a private one when the call has no mic).
function buildScreenAudioMix(screenTrack, voiceStore) {
  teardownScreenAudioMix(voiceStore)
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
    const screenGain = ctx.createGain()
    const targetGain = voiceStore?.isScreenAudioMuted ? 0 : ((voiceStore?.screenAudioVolume ?? 100) / 100)
    if (screenGain.gain?.setValueAtTime) {
      screenGain.gain.setValueAtTime(targetGain, ctx.currentTime)
    } else if (screenGain.gain) {
      screenGain.gain.value = targetGain
    }
    screenSource.connect(screenGain)
    screenGain.connect(destination)

    let micSource = null
    if (localAudioStream.value) {
      micSource = ctx.createMediaStreamSource(localAudioStream.value)
      micSource.connect(destination)
    }
    mix = { ctx, ownsContext, screenTrack, screenSource, screenGain, micSource, destination }
    if (voiceStore) voiceStore.hasScreenAudio = true
    return mix
  } catch (err) {
    console.warn('[WebRTC] Screen audio mixing unavailable:', err)
    if (voiceStore) voiceStore.hasScreenAudio = false
    return null
  }
}


function removeRemoteAudio(trackId) {
  const el = remoteAudioElements.get(trackId)
  if (!el) return
  remoteAudioElements.delete(trackId)
  releaseBoost(el)
  try {
    el.pause?.()
    el.srcObject = null
    el.remove()
  } catch { /* detached */ }
}

// Plays one remote audio track. The stream ID names the speaking user; a
// repeated ontrack for the same track (transceiver reused for someone else)
// only retags its element.
function attachRemoteAudio(voiceStore, track, stream) {
  const userId = stream?.id || ''
  let audioEl = remoteAudioElements.get(track.id)
  if (audioEl) {
    if (userId) audioEl.dataset.userId = userId
    else delete audioEl.dataset.userId
    applyRemoteGain(voiceStore, audioEl)
    if (audioEl.paused) audioEl.play?.().catch(() => {})
  } else {
    audioEl = new Audio()
    audioEl.srcObject = new MediaStream([track])
    audioEl.autoplay = true
    if (userId) audioEl.dataset.userId = userId
    applyOutput(voiceStore, audioEl)
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
    remoteAudioElements.set(track.id, audioEl)
    track.onended = () => removeRemoteAudio(track.id)
  }

  // When the SFU stops forwarding, the browser removes the track from its
  // stream instead of ending it. Only drop the element while it still
  // belongs to this stream: a reused track may already play for someone else.
  stream?.addEventListener?.('removetrack', (e) => {
    if (e.track !== track) return
    const el = remoteAudioElements.get(track.id)
    if (el && (el.dataset.userId || '') === userId) removeRemoteAudio(track.id)
  })
}

function setupPeerConnection(voiceStore, chatStore) {
  cleanupPeerConnection(voiceStore)

  const rtcConfig = { iceServers }
  if (voiceStore?.qosHighPriority) {
    rtcConfig.dscp = true
  }
  pc = new RTCPeerConnection(rtcConfig)
  diagSink = (payload) => chatStore.sendWSEvent('webrtc_diag', payload)
  startStatsPolling(voiceStore)

  // Connection diagnostics for the server log: what this browser gathers and
  // how the connection develops (candidate types only, never addresses).
  const diag = { candidates: {}, errors: [] }
  const reportDiag = (event) => chatStore.sendWSEvent('webrtc_diag', {
    event,
    gathering: pc?.iceGatheringState,
    ice: pc?.iceConnectionState,
    connection: pc?.connectionState,
    signaling: pc?.signalingState,
    candidates: diag.candidates,
    errors: diag.errors.slice(-5),
    ice_servers: (iceServers || []).length,
    ua: typeof navigator !== 'undefined' ? navigator.userAgent : ''
  })

  pc.onicecandidate = (event) => {
    if (event.candidate) {
      const key = `${event.candidate.protocol || '?'}/${event.candidate.type || '?'}`
      diag.candidates[key] = (diag.candidates[key] || 0) + 1
      chatStore.sendWSEvent('webrtc_candidate', event.candidate.toJSON())
    }
  }
  pc.onicecandidateerror = (event) => {
    diag.errors.push(`${event.errorCode} ${event.errorText || ''} ${event.url || ''}`.trim())
  }
  pc.onicegatheringstatechange = () => reportDiag('gathering')
  pc.oniceconnectionstatechange = () => {
    if (['connected', 'failed', 'disconnected'].includes(pc?.iceConnectionState)) reportDiag('ice')
  }

  pc.ontrack = (event) => {
    // The SFU names the publishing user in the stream ID.
    const streamId = event.streams?.[0]?.id
    if (event.track.kind === 'audio') {
      attachRemoteAudio(voiceStore, event.track, event.streams?.[0])
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

  // The outgoing lines (microphone, screen, camera) are the SFU's own receive
  // lines from its offer; see bindPublishSenders.
  return pc
}

/**
 * The SFU creates our three receive lines before anything else, so they are
 * the offer's first audio line (microphone) and first two video lines
 * (screen, camera). Their direction says nothing: the SFU may reuse them to
 * forward other members' media (then they read sendrecv/sendonly).
 */
export function publishMids(sdp) {
  const out = { audio: null, video: [] }
  let kind = null
  let mid = null
  const flush = () => {
    if (!kind || mid === null) return
    if (kind === 'audio' && out.audio === null) out.audio = mid
    else if (kind === 'video' && out.video.length < 2) out.video.push(mid)
  }
  for (const line of String(sdp || '').split(/\r?\n/)) {
    if (line.startsWith('m=')) {
      flush()
      kind = line.slice(2).split(' ')[0]
      mid = null
    } else if (line.startsWith('a=mid:')) {
      mid = line.slice(6).trim()
    }
  }
  flush()
  return out
}

// How a shared screen starts: the browser's bandwidth estimate opens at a few
// hundred kbit/s and only creeps up, which at 4K means a slideshow for the
// first half minute. The estimate starts at SCREEN_START_KBPS instead and
// never drops below SCREEN_MIN_KBPS (congestion control still backs off above
// that). Chrome reads both from the codec parameters of what it sends.
export const SCREEN_START_KBPS = 6000
export const SCREEN_MIN_KBPS = 1000

// Payload types that carry no video of their own.
const AUX_CODECS = new Set(['rtx', 'red', 'ulpfec', 'flexfec-03'])

// H.264 first, preferring constrained baseline (what the hardware encoders on
// Mac and Windows handle everywhere), then baseline, then other profiles.
function h264Rank(name, fmtp) {
  if (name !== 'h264') return 4
  if (!/packetization-mode=1/.test(fmtp)) return 3
  const profile = (/profile-level-id=([0-9a-f]{4})/i.exec(fmtp)?.[1] || '').toLowerCase()
  if (profile === '42e0') return 0
  if (profile === '4200') return 1
  return 2
}

/**
 * Tunes the screen line (mid) of the SFU's offer before it is applied: with
 * h264, H.264 moves to the front so the browser sends it (hardware encoded on
 * Mac and Windows; VP8 at 4K runs on the CPU and tops out near 30 fps), and
 * every video codec on the line gets the start and floor bitrate above. The
 * SFU forwards whatever arrives, so viewers decode the same codec.
 */
export function tuneScreenOffer(sdp, mid, { h264 = false, startKbps = SCREEN_START_KBPS, minKbps = SCREEN_MIN_KBPS } = {}) {
  if (!sdp || mid === null || mid === undefined) return sdp
  const eol = sdp.includes('\r\n') ? '\r\n' : '\n'
  const lines = sdp.split(/\r?\n/)
  // Section boundaries: [start, end) per m= line.
  const starts = []
  lines.forEach((l, i) => { if (l.startsWith('m=')) starts.push(i) })
  for (let n = 0; n < starts.length; n++) {
    const from = starts[n]
    const to = n + 1 < starts.length ? starts[n + 1] : lines.length
    const section = lines.slice(from, to)
    if (!section[0].startsWith('m=video') || !section.includes(`a=mid:${mid}`)) continue

    const names = new Map()
    const fmtps = new Map()
    for (const l of section) {
      let m = /^a=rtpmap:(\d+) ([^/]+)\//.exec(l)
      if (m) names.set(m[1], m[2].toLowerCase())
      m = /^a=fmtp:(\d+) (.*)$/.exec(l)
      if (m) fmtps.set(m[1], m[2])
    }
    const media = (pt) => names.has(pt) && !AUX_CODECS.has(names.get(pt))

    const out = []
    for (const l of section) {
      if (l.startsWith('m=') && h264) {
        const parts = l.split(' ')
        const pts = parts.slice(3)
        const rank = (pt) => (media(pt) ? h264Rank(names.get(pt), fmtps.get(pt) || '') : 5)
        const sorted = pts.map((pt, i) => [pt, i]).sort((a, b) => rank(a[0]) - rank(b[0]) || a[1] - b[1]).map(e => e[0])
        out.push([...parts.slice(0, 3), ...sorted].join(' '))
        continue
      }
      let m = startKbps ? /^a=fmtp:(\d+) (.*)$/.exec(l) : null
      if (m && media(m[1]) && !m[2].includes('x-google-')) {
        out.push(`${l};x-google-start-bitrate=${startKbps};x-google-min-bitrate=${minKbps}`)
        continue
      }
      out.push(l)
      m = startKbps ? /^a=rtpmap:(\d+) /.exec(l) : null
      if (m && media(m[1]) && !fmtps.has(m[1])) {
        out.push(`a=fmtp:${m[1]} x-google-start-bitrate=${startKbps};x-google-min-bitrate=${minKbps}`)
      }
    }
    lines.splice(from, to - from, ...out)
    break
  }
  return lines.join(eol)
}

// Whether this browser can send H.264 at all (not whether in hardware; the
// stream diagnostics report that).
function canSendH264() {
  try {
    const caps = globalThis.RTCRtpSender?.getCapabilities?.('video')
    return !!caps?.codecs?.some(c => /^video\/h264$/i.test(c.mimeType))
  } catch {
    return false
  }
}

/**
 * Sends on the SFU's receive lines. Transceivers created locally with
 * addTransceiver are never matched to a remote offer's m-lines, so media put
 * on them would never reach the server; the offer's own transceivers are
 * switched to send instead, once, right after the offer is applied.
 */
function bindPublishSenders(conn, sdp) {
  const mids = publishMids(sdp)
  const bind = (mid, track) => {
    const tr = mid === null || mid === undefined ? null : conn.getTransceivers().find(t => t.mid === mid)
    if (!tr) return null
    tr.direction = 'sendrecv'
    if (track) tr.sender.replaceTrack(track).catch(() => {})
    return tr.sender
  }
  // fresh: senders bound by this call, which still need their parameters.
  const bound = { screen: false, camera: false, fresh: {} }
  if (!audioSender) {
    audioSender = bind(mids.audio, activeAudioTrack())
    if (audioSender) bound.fresh.audio = audioSender
  }
  if (!screenSender) {
    screenSender = bind(mids.video[0], localScreenStream.value?.getVideoTracks()[0])
    bound.screen = !!screenSender && !!localScreenStream.value
    if (screenSender) bound.fresh.screen = screenSender
  }
  if (!cameraSender) {
    cameraSender = bind(mids.video[1], localCameraStream.value?.getVideoTracks()[0])
    bound.camera = !!cameraSender && !!localCameraStream.value
    if (cameraSender) bound.fresh.camera = cameraSender
  }
  return bound
}

// Bitrate, degradation and priority of a freshly bound sender: the same a
// share or camera started in a running connection gets, so a reconnect or a
// channel switch keeps them.
async function tuneBoundSenders(fresh, voiceStore) {
  if (fresh.audio) await applyQosToSender(fresh.audio, voiceStore)
  if (fresh.screen) await tuneSender(fresh.screen, screenSenderParams(voiceStore))
  if (fresh.camera) await tuneSender(fresh.camera, cameraSenderParams(voiceStore))
}

// Sender limits (bits per second). The browser's own congestion control still
// lowers the bitrate on a weak uplink; these decide what it gives up while
// doing so and cap the ceiling. A shared screen keeps its resolution and drops
// frames instead (text stays readable); a camera may trade either.
export const SCREEN_MAX_BITRATE = 12_000_000
export const CAMERA_MAX_BITRATE = 2_500_000

/**
 * Shared system audio would also carry the voices this page plays: the others
 * hear themselves, and with two people sharing it loops. Browsers that can
 * leave this page's own sound out of the capture (restrictOwnAudio, Chrome and
 * Edge 141+) do that and keep the sound untouched; elsewhere echo
 * cancellation removes what the page plays (at some cost to music).
 */
export function screenAudioConstraints(supported = navigator.mediaDevices?.getSupportedConstraints?.() || {}) {
  const base = { autoGainControl: false, noiseSuppression: false }
  if (supported.restrictOwnAudio) return { ...base, echoCancellation: false, restrictOwnAudio: true }
  return { ...base, echoCancellation: true }
}

// What keeps the page's own sound out of a shared screen's audio, for the
// stream diagnostics: restricted, echo-cancel or none.
function ownAudioGuard(track) {
  const s = track?.getSettings?.() || {}
  if (s.restrictOwnAudio) return 'restricted'
  if (s.echoCancellation) return 'echo-cancel'
  return 'none'
}
let screenAudioGuard = null

async function tuneSender(sender, { maxBitrate, degradationPreference, priority, networkPriority } = {}) {
  try {
    const params = sender.getParameters?.()
    if (!params) return
    if (!params.encodings?.length) params.encodings = [{}]
    if (maxBitrate !== undefined) params.encodings[0].maxBitrate = maxBitrate
    if (degradationPreference) params.degradationPreference = degradationPreference
    if (priority) params.encodings[0].priority = priority
    if (networkPriority) params.encodings[0].networkPriority = networkPriority
    await sender.setParameters(params)
  } catch (err) {
    // Not every browser accepts every field; the defaults still work.
    console.debug('[WebRTC] Could not tune sender:', err)
  }
}

function qosPriority(voiceStore) {
  return voiceStore?.qosHighPriority ? 'high' : 'medium'
}

function screenSenderParams(voiceStore) {
  const prio = qosPriority(voiceStore)
  return { maxBitrate: SCREEN_MAX_BITRATE, degradationPreference: 'maintain-resolution', priority: prio, networkPriority: prio }
}

function cameraSenderParams(voiceStore) {
  const prio = qosPriority(voiceStore)
  return { maxBitrate: CAMERA_MAX_BITRATE, degradationPreference: 'balanced', priority: prio, networkPriority: prio }
}

async function applyQosToSender(sender, voiceStore) {
  if (!sender) return
  const prio = qosPriority(voiceStore)
  await tuneSender(sender, { priority: prio, networkPriority: prio })
}

function isTypingTarget(el) {
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}

function handlePttKeyDown(e) {
  const voiceStore = pttStore
  if (!voiceStore || voiceStore.inputMode !== 'ptt' || e.repeat) return
  // Holding the key while typing in the composer must not open the mic.
  if (isTypingTarget(e.target)) return
  if ((e.code || e.key) === voiceStore.pttKey) {
    if (!voiceStore.isPttPressed) {
      voiceStore.isPttPressed = true
      if (voiceStore.isConnected || voiceStore.isMicTesting) {
        playSound('ptt_start')
      }
    }
  }
}

function handlePttKeyUp(e) {
  const voiceStore = pttStore
  if (!voiceStore || voiceStore.inputMode !== 'ptt') return
  if ((e.code || e.key) === voiceStore.pttKey) {
    if (voiceStore.isPttPressed) {
      voiceStore.isPttPressed = false
      if (voiceStore.isConnected || voiceStore.isMicTesting) {
        playSound('ptt_stop')
      }
    }
  }
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
let testBiquad = null
let testSuppressor = null
// Bumped whenever the standalone test is stopped or restarted (see startMicTest).
let micTestGen = 0
// The test installed the push-to-talk listeners (no call had them).
let testOwnsPtt = false

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

function setNodeGain(gainNode, val, time = 0) {
  if (!gainNode?.gain) return
  if (typeof gainNode.gain.setValueAtTime === 'function') {
    try { gainNode.gain.setValueAtTime(val, time) } catch { /* ignore */ }
  } else {
    gainNode.gain.value = val
  }
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

  // Builds the mic-test monitor (what I hear of myself) once, on the given
  // context after the high-pass filter. Starts silent; the meter interval
  // opens it with the gate.
  function connectLoopback(ctx, source) {
    if (loopbackGain || !ctx || !source) return
    try {
      const gain = ctx.createGain()
      setNodeGain(gain, 0, ctx.currentTime)
      source.connect(gain)
      gain.connect(ctx.destination)
      loopbackGain = gain
      loopbackCtx = ctx
      // The monitor plays on the same device as the voices in a call.
      applyOutput(voiceStore, ctx)
    } catch (err) {
      console.warn('[WebRTC] Mic test loopback setup failed:', err)
    }
  }

  function disconnectLoopback() {
    if (!loopbackGain) return
    try { loopbackGain.disconnect() } catch { /* ignore */ }
    loopbackGain = null
    loopbackCtx = null
  }

  // The standalone test (outside a call). Every await is followed by a check
  // of micTestGen: stopMicTest (or a newer start) bumps it, and a superseded
  // start cleans up what it acquired instead of finishing, so closing the
  // settings while the browser still asks for the mic leaves nothing open.
  async function startMicTest() {
    // If we're already connected to a voice channel, ensure audioContext is active
    if (localAudioStream.value && audioContext) {
      if (audioContext.state === 'suspended') {
        await audioContext.resume().catch(() => {})
      }
      return
    }

    teardownTestPipeline()
    const gen = micTestGen
    const stale = () => gen !== micTestGen

    let stream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints() })
    } catch (err) {
      console.warn('Mic test failed (permission denied or no device):', err)
      if (!stale()) voiceStore.currentInputLevel = 0
      return
    }
    if (stale()) {
      stream.getTracks().forEach(tr => tr.stop())
      return
    }
    testAudioStream = stream

    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext
      const ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
      testAudioContext = ctx
      // Not awaited: without a user gesture resume() may never settle.
      if (ctx.state === 'suspended') ctx.resume().catch(() => {})

      const analyserNode = ctx.createAnalyser()
      analyserNode.fftSize = 256
      analyserNode.smoothingTimeConstant = 0.2

      let source = ctx.createMediaStreamSource(stream)
      // Meter the filtered signal so the gate threshold calibrates like in a call.
      const model = aiModel(voiceStore.noiseMode)
      if (model) {
        const node = await createNoiseSuppressorNode(ctx, model)
        if (stale()) {
          // stopMicTest already closed the context and stopped the stream.
          try { node?.destroy?.() } catch { /* ignore */ }
          return
        }
        if (node) {
          source.connect(node)
          source = node
          testSuppressor = node
        }
      }
      const biquad = ctx.createBiquadFilter()
      biquad.type = 'highpass'
      biquad.frequency.setValueAtTime(85, ctx.currentTime)
      source.connect(biquad)
      biquad.connect(analyserNode)
      testAnalyser = analyserNode
      testBiquad = biquad

      if (voiceStore.isMicTesting) connectLoopback(ctx, biquad)

      // Push-to-talk works in the test too, also outside a call.
      if (!pttStore) {
        setupPttListeners(voiceStore)
        testOwnsPtt = true
      }

      const buffer = new Uint8Array(analyserNode.fftSize)
      const gateOpen = createVoiceGate(voiceStore)
      testSpeakingInterval = setInterval(() => {
        if (!testAnalyser) return
        const level = calculateRMSLevel(testAnalyser, buffer)
        voiceStore.currentInputLevel = level
        const open = gateOpen(level)
        if (loopbackGain && loopbackCtx === ctx && voiceStore.isMicTesting) {
          setNodeGain(loopbackGain, open ? (voiceStore.outputVolume / 100) : 0, ctx.currentTime)
        }
      }, 50)

      // Refresh devices after permission is granted so device labels are available
      await refreshAudioDevices()
    } catch (err) {
      console.warn('Mic test setup failed:', err)
      if (!stale()) {
        teardownTestPipeline()
        voiceStore.currentInputLevel = 0
      }
    }
  }

  async function startMicLoopback() {
    disconnectLoopback()
    voiceStore.isMicTesting = true

    // If in a voice call:
    if (localAudioStream.value && audioContext && callBiquad) {
      connectLoopback(audioContext, callBiquad)
      const track = localAudioStream.value.getAudioTracks()[0]
      if (track) track.enabled = false
      chatStore.sendWSEvent('voice_speaking', { active: false })
      updateRemoteVolume(voiceStore)
      return
    }

    // Not in a voice call: the test builds the loopback itself once it runs.
    if (testAudioContext && testBiquad) {
      connectLoopback(testAudioContext, testBiquad)
    } else {
      await startMicTest()
    }
  }

  function stopMicLoopback() {
    voiceStore.isMicTesting = false
    disconnectLoopback()
    if (localAudioStream.value) {
      const track = localAudioStream.value.getAudioTracks()[0]
      if (track) track.enabled = !voiceStore.isMuted
      updateRemoteVolume(voiceStore)
    }
  }

  function toggleMicTest() {
    if (voiceStore.isMicTesting) stopMicLoopback()
    else startMicLoopback()
  }

  // Ends the standalone test pipeline (and any start still awaiting the mic).
  function teardownTestPipeline() {
    micTestGen++
    if (testSpeakingInterval) {
      clearInterval(testSpeakingInterval)
      testSpeakingInterval = null
    }
    if (loopbackCtx && loopbackCtx === testAudioContext) disconnectLoopback()
    if (testSuppressor) {
      try {
        testSuppressor.disconnect?.()
        testSuppressor.destroy?.()
      } catch { /* ignore */ }
      testSuppressor = null
    }
    testBiquad = null
    testAnalyser = null
    if (testAudioStream) {
      testAudioStream.getTracks().forEach(tr => tr.stop())
      testAudioStream = null
    }
    if (testAudioContext) {
      testAudioContext.close().catch(() => {})
      testAudioContext = null
    }
    if (testOwnsPtt) {
      testOwnsPtt = false
      // A call that started meanwhile keeps its own listeners.
      if (!localAudioStream.value) removePttListeners()
    }
  }

  function stopMicTest() {
    stopMicLoopback()
    teardownTestPipeline()

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

    teardownScreenAudioMix(voiceStore)
    teardownMicPipeline()
    voiceStore.currentInputLevel = 0
  }

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

    try {
      const sdp = tuneScreenOffer(offer.sdp, publishMids(offer.sdp).video[0], { h264: canSendH264() })
      await pc.setRemoteDescription(new RTCSessionDescription({ type: offer.type, sdp }))
      const bound = bindPublishSenders(pc, offer.sdp)
      // A share started before the connection was up goes out now.
      if (bound.screen) {
        chatStore.sendWSEvent('webrtc_screenshare_start', {})
        chatStore.sendWSEvent('webrtc_request_keyframe', {})
      }
      const queued = pendingCandidates
      pendingCandidates = []
      for (const c of queued) await pc.addIceCandidate(new RTCIceCandidate(c)).catch(() => {})
      const conn = pc
      const answer = await conn.createAnswer()
      await conn.setLocalDescription(answer)
      chatStore.sendWSEvent('webrtc_answer', answer)
      // Encoding parameters only stick once the line is negotiated.
      if (pc === conn) await tuneBoundSenders(bound.fresh, voiceStore)
    } catch (err) {
      console.warn('[WebRTC] Offer/Answer negotiation error:', err)
      chatStore.sendWSEvent('webrtc_diag', { event: 'negotiation_error', error: String(err?.message || err) })
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
      onCandidate: handleRemoteCandidate,
      onKicked: handleKicked
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
    if (gen !== joinGeneration) {
      sendStream?.getTracks().forEach(t => t.stop())
      stream?.getTracks().forEach(t => t.stop())
      return
    }

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

  // Everyone sees whether I am muted or deafened (marks on my avatar).
  function sendMuteState() {
    if (!voiceStore.currentChannelId) return
    chatStore.sendWSEvent('voice_mute_state', { muted: !!voiceStore.isMuted, deafened: !!voiceStore.isDeafened })
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
    sendMuteState()
  }

  // Applies changed input settings (device, AGC, noise suppression, echo
  // cancellation). In a call the mic is re-acquired and swapped into the
  // running connection without renegotiation; otherwise the mic test restarts.
  // Runs are serialized: two overlapping swaps would each tear down the
  // other's pipeline and lose the screen-audio mix on the way.
  function applyAudioSettings() {
    if (!localAudioStream.value && !audioSettingsBusy) {
      return applyQos().then(() => startMicTest())
    }
    audioSettingsBusy++
    const run = audioSettingsChain.then(() => swapMicrophone())
    audioSettingsChain = run.catch(() => {}).finally(() => { audioSettingsBusy-- })
    return run
  }

  async function applyQos() {
    if (audioSender) await applyQosToSender(audioSender, voiceStore)
    if (screenSender) await applyQosToSender(screenSender, voiceStore)
    if (cameraSender) await applyQosToSender(cameraSender, voiceStore)
  }

  async function swapMicrophone() {
    await applyQos()
    // Left the call while an earlier run was busy.
    if (!localAudioStream.value) return
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
    const sharedAudio = mix?.screenTrack
    teardownScreenAudioMix(voiceStore)
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
    // A share started meanwhile mixed in the old (stopped) mic: rebuild with
    // whichever screen audio is current.
    const screenAudio = mix?.screenTrack || sharedAudio
    if (screenAudio && screenAudio.readyState !== 'ended') {
      if (buildScreenAudioMix(screenAudio, voiceStore)) screenAudio.onended = detachScreenAudio
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
    // Not awaited: after a reload (voice resume) there is no user gesture yet
    // and resume() may stay pending until there is one; the graph starts then.
    if (ctx.state === 'suspended') ctx.resume().catch(() => {})

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

    // Input volume other than 100 %: microphone (or AI filter) -> gain -> sent stream.
    const inputVolume = Number(voiceStore.inputVolume ?? 100)
    if (inputVolume !== 100) {
      try {
        const gain = ctx.createGain()
        setNodeGain(gain, inputVolume / 100, ctx.currentTime)
        const destination = ctx.createMediaStreamDestination()
        if (suppressorNode) {
          suppressorNode.disconnect()
          suppressorNode.connect(gain)
        } else {
          ctx.createMediaStreamSource(stream).connect(gain)
          rawMicStream = stream
        }
        gain.connect(destination)
        inputGainNode = gain
        sendStream = destination.stream
      } catch (err) {
        console.warn('[WebRTC] Input volume stage unavailable:', err)
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
      callBiquad = biquad

      if (voiceStore.isMicTesting) connectLoopback(ctx, biquad)
    } catch (err) {
      console.warn('AudioContext speaking detector setup error:', err)
    }
    return sendStream
  }

  function teardownMicPipeline() {
    callBiquad = null
    if (inputGainNode) {
      try { inputGainNode.disconnect() } catch { /* ignore */ }
      inputGainNode = null
    }
    if (loopbackCtx && loopbackCtx === audioContext) disconnectLoopback()
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
    const gateOpen = createVoiceGate(voiceStore)

    speakingInterval = setInterval(() => {
      if (!analyser) return

      const level = calculateRMSLevel(analyser, buffer)

      // PERFORMANCE FIX: Only update reactive Pinia store if settings modal is open
      if (voiceStore.showAudioSettings) {
        voiceStore.currentInputLevel = level
      }

      // No Audio Detected Warning check
      if (voiceStore.warnNoAudioDetected && !voiceStore.isMuted && !voiceStore.isMicTesting) {
        if (level < 1) {
          if (!unmutedZeroAudioStartTime) unmutedZeroAudioStartTime = Date.now()
          else if (Date.now() - unmutedZeroAudioStartTime > 15000) {
            if (Date.now() - lastNoAudioWarningToastTime > 120000) {
              useToastStore().info(t('audio.noAudioDetectedToast'))
              lastNoAudioWarningToastTime = Date.now()
            }
          }
        } else {
          unmutedZeroAudioStartTime = 0
        }
      } else {
        unmutedZeroAudioStartTime = 0
      }

      // If mic testing in a call: mute channel transmission and gate loopback
      if (voiceStore.isMicTesting) {
        if (wasSpeaking) {
          wasSpeaking = false
          chatStore.sendWSEvent('voice_speaking', { active: false })
        }
        if (localAudioStream.value) {
          const track = localAudioStream.value.getAudioTracks()[0]
          if (track && track.enabled) track.enabled = false
        }
        const open = gateOpen(level)
        if (loopbackGain && audioContext && loopbackCtx === audioContext) {
          setNodeGain(loopbackGain, open ? (voiceStore.outputVolume / 100) : 0, audioContext.currentTime)
        }
        return
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

      // Noise gate: push-to-talk, or voice activity with hangover.
      const shouldTransmit = gateOpen(level)

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
    teardownScreenAudioMix(voiceStore)
    if (audioSender) await audioSender.replaceTrack(localAudioStream.value?.getAudioTracks()[0] ?? null).catch(() => {})
  }

  async function startScreenShare() {
    if (localScreenStream.value) return
    const gen = joinGeneration
    const channelId = voiceStore.currentChannelId
    let stream
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({
        video: {
          frameRate: { ideal: 60, max: 60 },
          width: { ideal: 3840, max: 3840 },
          height: { ideal: 2160, max: 2160 }
        },
        audio: screenAudioConstraints()
      })
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
      return
    }
    // Left or switched the call (or started twice) while the picker was open.
    if (gen !== joinGeneration || voiceStore.currentChannelId !== channelId || localScreenStream.value) {
      stream.getTracks().forEach(tr => tr.stop())
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
      await tuneSender(screenSender, screenSenderParams(voiceStore))
      chatStore.sendWSEvent('webrtc_screenshare_start', {})
      chatStore.sendWSEvent('webrtc_request_keyframe', {})
    }

    // Share the screen's sound too: one mixed track replaces the mic track.
    const screenAudio = stream.getAudioTracks()[0]
    screenAudioGuard = screenAudio ? ownAudioGuard(screenAudio) : null
    if (screenAudio) {
      if (buildScreenAudioMix(screenAudio, voiceStore)) {
        screenAudio.onended = detachScreenAudio
        const mixed = activeAudioTrack()
        if (audioSender && mixed) await audioSender.replaceTrack(mixed).catch(() => {})
      }
    } else {
      voiceStore.hasScreenAudio = false
      useToastStore().info(t('talk.noAudioInShareTip'))
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
    voiceStore.hasScreenAudio = false
    voiceStore.isScreenAudioMuted = false
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
        await tuneSender(cameraSender, cameraSenderParams(voiceStore))
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

  // Ends the call locally. The stored session is cleared and the channel
  // unset, so neither a reload nor a reconnect rejoins it.
  function endCall({ notifyServer }) {
    joinGeneration++
    voiceSession.forget()
    cleanupPeerConnection(voiceStore)
    cleanupVoiceAudio()
    stopScreenShare()
    stopCamera()
    if (notifyServer) chatStore.sendWSEvent('voice_leave', {})
    voiceStore.disconnect()
    voiceStore.audioBlocked = false
  }

  function leaveVoiceChannel() {
    endCall({ notifyServer: true })
  }

  // An admin removed me from the call (voice_kicked): the server already
  // dropped me, so no voice_leave goes out.
  function handleKicked(payload) {
    if (!voiceStore.currentChannelId) return
    if (payload?.channel_id && payload.channel_id !== voiceStore.currentChannelId) return
    endCall({ notifyServer: false })
    useToastStore().info(t('voice.kicked'))
  }

  // Rejoins the channel of a call interrupted by a reload less than 30 s ago.
  // Must run once the WebSocket is connected (voice_join goes over it).
  async function resumeVoiceSession() {
    const channelId = voiceSession.recent()
    if (!channelId || voiceStore.currentChannelId) return false
    if (!chatStore.isVoiceChannel(channelId)) {
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

  // These watchers exist once per voice store, however many components call
  // useWebRTC(): one per caller would send the mute state and rebuild the
  // microphone several times over. They live in a detached scope, so they
  // keep running after the first caller unmounts; a new store (a new Pinia,
  // as in tests) replaces them.
  if (watchedStore !== voiceStore) {
    watcherScope?.stop()
    watchedStore = voiceStore
    outputDeviceStore = voiceStore
    watcherScope = effectScope(true)
    watcherScope.run(() => {
      watch(() => voiceStore.selectedOutputDeviceId, () => applyOutputDeviceAll(voiceStore))

      // Input volume: adjust the gain live; the first change away from 100 %
      // rebuilds the pipeline once to put the gain stage in.
      watch(() => voiceStore.inputVolume, (v) => {
        if (inputGainNode) {
          setNodeGain(inputGainNode, v / 100, audioContext?.currentTime ?? 0)
          return
        }
        if (v === 100 || !localAudioStream.value) return
        clearTimeout(inputVolumeTimer)
        inputVolumeTimer = setTimeout(() => applyAudioSettings(), 250)
      })

      watch([
        () => voiceStore.outputVolume,
        () => voiceStore.isDeafened,
        () => voiceStore.userVolumes,
        () => voiceStore.localMutedUsers
      ], () => {
        updateRemoteVolume(voiceStore)
      })

      watch([
        () => voiceStore.isScreenAudioMuted,
        () => voiceStore.screenAudioVolume
      ], ([muted, vol]) => {
        if (mix?.screenGain && mix.ctx) {
          const target = muted ? 0 : ((vol ?? 100) / 100)
          if (mix.screenGain.gain?.setValueAtTime) {
            mix.screenGain.gain.setValueAtTime(target, mix.ctx.currentTime)
          } else if (mix.screenGain.gain) {
            mix.screenGain.gain.value = target
          }
        }
      })

      // Everyone sees whether I am muted or deafened.
      watch(() => [voiceStore.isMuted, voiceStore.isDeafened], sendMuteState)
    })
  }

  return {
    localAudioStream,
    localScreenStream,
    localCameraStream,
    refreshAudioDevices,
    startMicTest,
    stopMicTest,
    startMicLoopback,
    stopMicLoopback,
    toggleMicTest,
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
