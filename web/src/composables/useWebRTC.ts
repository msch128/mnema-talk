import { gamingMicLease } from '../lib/gamingMicLease'
import { ref, watch, effectScope, toRaw } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { summarizeStats } from '../lib/rtcStats'
import * as voiceSession from '../lib/voiceSession'
import { createMeteringTrack } from '../lib/micMetering'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported, preloadNoiseSuppressor } from '../lib/noiseSuppressor'
import { useToastStore } from '../stores/toast'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { browserMediaCall } from '../lib/media/browserCall'
import { installMediaCallFactory, mediaCall } from '../lib/media/api'
import { createVoiceGate } from '../lib/levelMeter'
import { trackConstraints, streamEncoding, streamTuning, STREAM_MAX_BITRATE } from '../lib/streamQuality'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import { platform } from '../lib/platform'
import type { FilterNode } from '../lib/dfnNode'
import type { NoiseModel } from '../lib/dfnTypes'
import type { MediaStat } from '../lib/mediaStats'
import type { RTCStatsSample } from '../lib/rtcStats'
import type { StreamQuality } from '../lib/streamQuality'
import { declaredLayers, layersKey, planScreenLayers } from '../lib/screenLayers'
import { SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, type FrameSource } from '../lib/media/frame'
import type { SubscribeRequest, JsonValue, ServerEventPayloads } from '../types/events'
import { decodeServerICEConfig } from '../types/rest'
import { publishMids, restrictVideoCodecs, tuneScreenLines } from './webrtc/sdp'
import { streamDiag } from './webrtc/streamDiag'
import { createPushToTalk } from './webrtc/pushToTalk'
import { applyOutput, applyOutputToRemote, attachRemoteAudio, followOutputDevice, playPausedRemoteAudio, releaseRemoteAudio, resumeRemoteAudio as resumeRemotePlayback, updateRemoteVolume } from './webrtc/remoteAudio'
import { CAMERA_STREAM_PREFIX, SCREEN_AUDIO_STREAM_PREFIX, publisherId } from './webrtc/streamIds'
import { announceMediaRekeyed, announceMediaUnprotected, closeMediaRoom, mediaUnprotected, onMediaUnprotected, openMediaRoom, queueMediaWire, resetMediaAnnouncements } from './webrtc/mediaRoom'

// Tests install a double first. This only supplies the call when none is installed.
installMediaCallFactory(browserMediaCall)

type VoiceStore = ReturnType<typeof useVoiceStore>
type ChatStore = ReturnType<typeof useChatStore>
type AudioGuard = 'restricted' | 'echo-cancel' | 'none'
interface BoundSenders { audio?: RTCRtpSender; screen?: RTCRtpSender; camera?: RTCRtpSender; screenAudio?: RTCRtpSender; layers?: RTCRtpSender[] }
interface SenderTuning { active?: boolean; maxBitrate?: number; maxFramerate?: number; scaleResolutionDownBy?: number; degradationPreference?: RTCDegradationPreference; priority?: RTCPriorityType; networkPriority?: RTCPriorityType }


// Module-level shared singletons across all components
const localAudioStream = ref<MediaStream | null>(null)
const localScreenStream = ref<MediaStream | null>(null)
const localCameraStream = ref<MediaStream | null>(null)
let audioContext: AudioContext | null = null
let analyser: AnalyserNode | null = null
// Always-enabled clone of the mic track that only feeds the level meter (see lib/micMetering).
let meteringTrack: MediaStreamTrack | null = null
// With AI noise suppression the captured mic stream only feeds the model;
// localAudioStream is then the processed stream that is sent, gated and muted.
let rawMicStream: MediaStream | null = null
let suppressorNode: FilterNode | null = null
let micPipelineController: AbortController | null = null
let micTestController: AbortController | null = null
// A failed model uses browser suppression for this session; the saved choice
// is untouched, and leaving/rejoining permits a fresh attempt.
let failedAIModel: NoiseModel | null = null

// Noise mode -> model in lib/noiseSuppressor (null: no AI filter).
function aiModel(noiseMode: string | null | undefined): NoiseModel | null {
  return noiseMode === 'ai' ? 'dfn3' : noiseMode === 'ai-lite' ? 'gtcrn' : null
}
let speakingInterval: ReturnType<typeof setInterval> | null = null
// The mic-test monitor: a gain stage to the speakers on the context it was
// built on (the call's or the standalone test's).
let loopbackGain: GainNode | null = null
let loopbackCtx: AudioContext | null = null
let callBiquad: BiquadFilterNode | null = null
let unmutedZeroAudioStartTime = 0
let lastNoAudioWarningToastTime = 0

// WebRTC Peer Connection and Remote Audio
let pc: RTCPeerConnection | null = null
// Senders of the four fixed outgoing lines, created with the connection:
// mic, screen share, camera and the screen share's sound. Tracks are swapped
// with replaceTrack, so starting or stopping them never renegotiates. The
// screen's sound goes on its own line (not mixed into the mic), so viewers
// set its volume apart from the voice and only those watching receive it.
let audioSender: RTCRtpSender | null = null
let screenSender: RTCRtpSender | null = null
let cameraSender: RTCRtpSender | null = null
let screenAudioSender: RTCRtpSender | null = null
// Senders of the screen's lower layers (see lib/screenLayers), on the SFU's
// third and fourth video line; null where the server offers none. Each sends
// the same capture as the screen sender, smaller.
let layerSenders: (RTCRtpSender | null)[] = [null, null]
// The layers last declared to the server (lib/screenLayers.layersKey).
let declaredKey = ''
// The size and rate a capture delivers change under a running share (a resized
// window, a new display mode), and a track fires no event for it, so they are
// read regularly. A change that has held for CAPTURE_SETTLE_MS counts, so
// dragging a window edge is one change, not hundreds.
export const CAPTURE_POLL_MS = 500
export const CAPTURE_SETTLE_MS = 1000
let captureWatch: ReturnType<typeof setInterval> | null = null
let captureSettle: ReturnType<typeof setTimeout> | null = null
// A browser picker may stay open for a long time. Repeated clicks must not
// launch several capture pipelines; stopping also invalidates its result.
let screenCapturePending = false
let screenCaptureGeneration = 0
let cameraCaptureGeneration = 0
// Quality changes are applied one after another.
let qualityChain = Promise.resolve()
// Input volume: a gain stage in the mic pipeline, only built when the input
// volume is not 100 % (so the default path stays the plain microphone).
let gamingGainNode: GainNode | null = null
export function setGamingPttLease(pressed: boolean): void {
  if (gamingGainNode && audioContext) gamingMicLease(gamingGainNode, audioContext, useVoiceStore().inputMode === 'ptt', pressed)
}
// Back in view, the browser may have suspended the audio it plays.
const pushToTalk = createPushToTalk({
  lease: setGamingPttLease,
  wake(inCall) {
    if (audioContext && audioContext.state === 'suspended') audioContext.resume().catch(() => {})
    if (inCall) playPausedRemoteAudio()
  }
})
let inputGainNode: GainNode | null = null
// The settings watchers of useWebRTC, registered once per voice store.
let watcherScope: ReturnType<typeof effectScope> | null = null
let watchedStore: VoiceStore | null = null
let inputVolumeTimer: ReturnType<typeof setTimeout> | undefined
// applyAudioSettings runs one at a time; busy counts queued and running ones.
let audioSettingsChain = Promise.resolve()
let audioSettingsBusy = 0
// Offers are applied one after another; candidates that arrive before the
// remote description is set wait in pendingCandidates.
let signalingChain = Promise.resolve()
let pendingCandidates: RTCIceCandidateInit[] = []
// Bumped by every join and leave: an older join that is still awaiting the
// microphone sees the change and backs out instead of finishing.
let joinGeneration = 0

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let iceServers: RTCIceServer[] = []
let iceConfigLoaded: Promise<void> | null = null

// Loaded once, on the first voice join rather than at import time.
function loadIceServers() {
  if (!iceConfigLoaded) {
    iceConfigLoaded = fetch('/api/webrtc/config', { credentials: 'same-origin' })
      .then(async (res): Promise<unknown> => res.ok ? res.json() : null)
      .then(data => { if (data !== null) iceServers = decodeServerICEConfig(data).ice_servers })
      .catch(() => { iceConfigLoaded = null })
  }
  return iceConfigLoaded
}

// Connection statistics measured by the browser (RTCPeerConnection.getStats).
let statsTimer: ReturnType<typeof setInterval> | null = null
let lastStatsSample: RTCStatsSample | null = null
let statsPollingGeneration = 0

// Where stream diagnostics go (the server log, via the WebSocket).
let diagSink: ((payload: Record<string, JsonValue | undefined>) => void) | null = null
let statsTicks = 0
const lastVideoBytes = new Map<string, number>()

function startStatsPolling(voiceStore: VoiceStore) {
  stopStatsPolling(voiceStore)
  statsTicks = 0
  lastVideoBytes.clear()
  const generation = statsPollingGeneration
  statsTimer = setInterval(async () => {
    const conn = pc
    if (!conn) return
    try {
      const report = await conn.getStats()
      // A stopped poll must not restore statistics from a retired connection,
      // including a restarted poll that happens to use the same connection.
      if (pc !== conn || generation !== statsPollingGeneration) return
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

function stopStatsPolling(voiceStore: VoiceStore) {
  statsPollingGeneration++
  if (statsTimer) {
    clearInterval(statsTimer)
    statsTimer = null
  }
  lastStatsSample = null
  if (voiceStore) voiceStore.rtcStats = null
}

function captureKey(track: MediaStreamTrack): string {
  const settings = track.getSettings?.()
  return `${settings?.width}x${settings?.height}@${Math.round(settings?.frameRate ?? 0)}`
}

/** Calls onChange once the capture's width, height or frame rate has changed and stayed so. */
function watchCapture(track: MediaStreamTrack, onChange: () => void) {
  stopCaptureWatch()
  let seen = captureKey(track)
  captureWatch = setInterval(() => {
    const key = captureKey(track)
    if (key === seen) return
    seen = key
    if (captureSettle) clearTimeout(captureSettle)
    captureSettle = setTimeout(() => {
      captureSettle = null
      onChange()
    }, CAPTURE_SETTLE_MS)
  }, CAPTURE_POLL_MS)
}

function stopCaptureWatch() {
  if (captureWatch) clearInterval(captureWatch)
  if (captureSettle) clearTimeout(captureSettle)
  captureWatch = null
  captureSettle = null
}

function cleanupPeerConnection(voiceStore: VoiceStore) {
  stopCaptureWatch()
  stopStatsPolling(voiceStore)
  signalingChain = Promise.resolve()
  pendingCandidates = []
  if (pc) {
    try { pc.close() } catch { /* connection already closed */ }
    pc = null
  }
  releaseRemoteAudio()
  audioSender = null
  screenSender = null
  cameraSender = null
  screenAudioSender = null
  layerSenders = [null, null]
  declaredKey = ''
  voiceStore?.resetRemoteMedia()
}

// The track that goes out as "audio": the (processed) microphone.
function activeAudioTrack() {
  return localAudioStream.value?.getAudioTracks()[0] || null
}

// The sound of my running screen share, while it lasts.
function liveScreenAudioTrack() {
  const track = localScreenStream.value?.getAudioTracks()[0]
  return track && track.readyState !== 'ended' ? track : null
}
function setupPeerConnection(voiceStore: VoiceStore, chatStore: ChatStore) {
  cleanupPeerConnection(voiceStore)

  const rtcConfig: RTCConfiguration = { iceServers }
  if (voiceStore?.qosHighPriority) {
    rtcConfig.dscp = true
  }
  pc = new RTCPeerConnection(rtcConfig)
  const conn = pc
  diagSink = (payload) => chatStore.sendWSEvent('webrtc_diag', payload)
  startStatsPolling(voiceStore)

  // Connection diagnostics for the server log: what this browser gathers and
  // how the connection develops (candidate types only, never addresses).
  const diag: { candidates: Record<string, number>; errors: string[] } = { candidates: {}, errors: [] }
  const reportDiag = (event: string) => pc === conn && chatStore.sendWSEvent('webrtc_diag', {
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
    if (pc !== conn) return
    if (event.candidate) {
      const key = `${event.candidate.protocol || '?'}/${event.candidate.type || '?'}`
      diag.candidates[key] = (diag.candidates[key] || 0) + 1
      chatStore.sendWSEvent('webrtc_candidate', event.candidate.toJSON())
    }
  }
  pc.onicecandidateerror = (event) => {
    if (pc !== conn) return
    diag.errors.push(`${event.errorCode} ${event.errorText || ''} ${event.url || ''}`.trim())
  }
  pc.onicegatheringstatechange = () => reportDiag('gathering')
  pc.oniceconnectionstatechange = () => {
    if (['connected', 'failed', 'disconnected'].includes(pc?.iceConnectionState || '')) reportDiag('ice')
  }

  pc.ontrack = (event) => {
    if (pc !== conn) return
    // The SFU names the publishing user in the stream ID. No name, no playback.
    const streamId = event.streams?.[0]?.id
    const senderId = publisherId(streamId)
    if (!senderId) return
    // What the line carries, as the SFU names it. A sealed frame says what its
    // sender made it for, and only a frame that agrees is opened.
    const isVideo = event.track.kind === 'video'
    const source = isVideo
      ? (streamId?.startsWith(CAMERA_STREAM_PREFIX) ? SOURCE_CAMERA : SOURCE_SCREEN)
      : (streamId?.startsWith(SCREEN_AUDIO_STREAM_PREFIX) ? SOURCE_SCREEN_AUDIO : SOURCE_MIC)
    if (!mediaCall().protectIncoming(event.receiver, senderId, source)) {
      announceMediaUnprotected()
      return
    }
    if (event.track.kind === 'audio') {
      attachRemoteAudio(voiceStore, event.track, event.streams?.[0])
    } else if (event.track.kind === 'video') {
      const stream = new MediaStream([event.track])
      const isCamera = !!streamId?.startsWith(CAMERA_STREAM_PREFIX)
      const userId = isCamera ? streamId?.slice(CAMERA_STREAM_PREFIX.length) : streamId
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
function bindPublishSenders(conn: RTCPeerConnection, sdp: string, layerLines = 0) {
  const mids = publishMids(sdp, layerLines)
  const bind = (mid: string | null | undefined, track: MediaStreamTrack | null | undefined, source: FrameSource, layer?: number) => {
    const tr = mid === null || mid === undefined ? null : conn.getTransceivers().find(t => t.mid === mid)
    if (!tr) return null
    if (!mediaCall().protectSender(tr.sender, source, layer)) {
      announceMediaUnprotected()
      return null
    }
    tr.direction = 'sendrecv'
    if (track) tr.sender.replaceTrack(track).catch(() => {})
    return tr.sender
  }
  // fresh: senders bound by this call, which still need their parameters.
  const bound: { screen: boolean; camera: boolean; fresh: BoundSenders } = { screen: false, camera: false, fresh: {} }
  if (!audioSender) {
    audioSender = bind(mids.audio, activeAudioTrack(), SOURCE_MIC)
    if (audioSender) bound.fresh.audio = audioSender
  }
  if (!screenSender) {
    screenSender = bind(mids.video[0], null, SOURCE_SCREEN, 0)
    bound.screen = !!screenSender && !!localScreenStream.value
    if (screenSender) bound.fresh.screen = screenSender
  }
  if (!cameraSender) {
    cameraSender = bind(mids.video[1], localCameraStream.value?.getVideoTracks()[0], SOURCE_CAMERA)
    bound.camera = !!cameraSender && !!localCameraStream.value
    if (cameraSender) bound.fresh.camera = cameraSender
  }
  if (!screenAudioSender) {
    screenAudioSender = bind(mids.screenAudio, liveScreenAudioTrack(), SOURCE_SCREEN_AUDIO)
    if (screenAudioSender) bound.fresh.screenAudio = screenAudioSender
  }
  // The lower layers of the screen share: lines an older server does not offer.
  for (let i = 0; i < Math.min(layerSenders.length, layerLines); i++) {
    if (layerSenders[i]) continue
    const sender = bind(mids.video[2 + i], null, SOURCE_SCREEN, i + 1)
    layerSenders[i] = sender
    if (sender) (bound.fresh.layers ??= []).push(sender)
  }
  return bound
}

// Bitrate, degradation, quality and priority of a freshly bound sender: the
// same a share or camera started in a running connection gets, so a
// reconnect or a channel switch keeps them (the chosen stream quality too).
async function tuneBoundSenders(fresh: BoundSenders, voiceStore: VoiceStore) {
  if (fresh.audio) await applyQosToSender(fresh.audio, voiceStore)
  if (fresh.screen) await tuneSender(fresh.screen, screenSenderParams(voiceStore))
  if (fresh.camera) await tuneSender(fresh.camera, cameraSenderParams(voiceStore))
  if (fresh.screenAudio) await applyQosToSender(fresh.screenAudio, voiceStore)
  // The lower layers are tuned by syncLayers once the share's size is known.
}

// Sender limits (bits per second). The browser's own congestion control still
// lowers the bitrate on a weak uplink; these decide what it gives up while
// doing so and cap the ceiling. A shared screen keeps its resolution and drops
// frames instead (text stays readable; at 60 fps it may trade either, see
// lib/streamQuality), and its cap follows the chosen quality up to
// SCREEN_MAX_BITRATE; a camera may trade either.
export const SCREEN_MAX_BITRATE = STREAM_MAX_BITRATE
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
function ownAudioGuard(track: MediaStreamTrack | null | undefined): AudioGuard {
  const s = track?.getSettings?.() || {}
  if (s.restrictOwnAudio) return 'restricted'
  if (s.echoCancellation) return 'echo-cancel'
  return 'none'
}
let screenAudioGuard: AudioGuard | null = null
const warnedScreenAudioTracks = new WeakSet<MediaStreamTrack>()

// Recognizing a constraint does not prove it was applied to the selected
// monitor's audio. Inspect the actual track before turning off the fallback.
async function ensureScreenAudioGuard(track: MediaStreamTrack | null | undefined) {
  if (ownAudioGuard(track) === 'none' && typeof track?.applyConstraints === 'function') {
    try {
      await track.applyConstraints({ ...(track.getConstraints?.() || {}), echoCancellation: true })
    } catch { /* The selected capture may not support echo cancellation. */ }
  }
  return ownAudioGuard(track)
}

async function tuneSender(sender: RTCRtpSender, { active, maxBitrate, maxFramerate, scaleResolutionDownBy, degradationPreference, priority, networkPriority }: SenderTuning = {}) {
  try {
    const params = sender.getParameters?.()
    if (!params) return
    if (!params.encodings?.length) params.encodings = [{}]
    const encoding = params.encodings[0]
    if (!encoding) return
    if (active !== undefined) encoding.active = active
    if (maxBitrate !== undefined) encoding.maxBitrate = maxBitrate
    if (maxFramerate !== undefined) encoding.maxFramerate = maxFramerate
    if (scaleResolutionDownBy !== undefined) encoding.scaleResolutionDownBy = scaleResolutionDownBy
    if (degradationPreference) params.degradationPreference = degradationPreference
    if (priority) encoding.priority = priority
    if (networkPriority) encoding.networkPriority = networkPriority
    await sender.setParameters(params)
  } catch (err) {
    // Not every browser accepts every field; the defaults still work.
    console.debug('[WebRTC] Could not tune sender:', err)
  }
}

function qosPriority(voiceStore: VoiceStore): RTCPriorityType {
  return voiceStore?.qosHighPriority ? 'high' : 'medium'
}

// The screen sender's parameters for the chosen stream quality, at the size
// the capture delivers now.
function screenSenderParams(voiceStore: VoiceStore, track = localScreenStream.value?.getVideoTracks()[0]) {
  const prio = qosPriority(voiceStore)
  const quality = voiceStore?.screenQuality
  const settings = track?.getSettings?.() || {}
  const { degradationPreference } = streamTuning(quality)
  return { ...streamEncoding(quality, settings), degradationPreference, priority: prio, networkPriority: prio }
}

// Applies the chosen stream quality to the running share: the capture is
// constrained (frame rate, height) and the sender re-tuned, without
// restarting the share. Where the browser cannot scale the capture, the
// encoder scales it down (see streamEncoding).
function applyScreenQuality(voiceStore: VoiceStore) {
  // A preset selected before the picker opens belongs to the next capture.
  // Do not let its queued watcher find that future track and configure it a
  // second time immediately after attachScreen has prepared the encoder.
  const track = localScreenStream.value?.getVideoTracks()[0]
  const quality = { ...voiceStore.screenQuality }
  qualityChain = qualityChain.then(async () => {
    if (!track || track !== localScreenStream.value?.getVideoTracks()[0]) return
    try {
      await track.applyConstraints?.(trackConstraints(quality))
    } catch (err) {
      console.debug('[WebRTC] Could not constrain the shared screen:', err)
    }
    if ('contentHint' in track) track.contentHint = streamTuning(quality).contentHint
    if (screenSender && track === localScreenStream.value?.getVideoTracks()[0]) {
      await tuneSender(screenSender, screenSenderParams(voiceStore))
    }
  }).catch(() => {})
  return qualityChain
}

function cameraSenderParams(voiceStore: VoiceStore): SenderTuning {
  const prio = qosPriority(voiceStore)
  return { maxBitrate: CAMERA_MAX_BITRATE, degradationPreference: 'balanced', priority: prio, networkPriority: prio }
}

async function applyQosToSender(sender: RTCRtpSender | null, voiceStore: VoiceStore) {
  if (!sender) return
  const prio = qosPriority(voiceStore)
  await tuneSender(sender, { priority: prio, networkPriority: prio })
}

// Test stream for Settings Modal when not connected to a voice channel
let testAudioStream: MediaStream | null = null
let testAudioContext: AudioContext | null = null
let testAnalyser: AnalyserNode | null = null
let testSpeakingInterval: ReturnType<typeof setInterval> | null = null
let testBiquad: BiquadFilterNode | null = null
let testSuppressor: FilterNode | null = null
// Bumped whenever the standalone test is stopped or restarted (see startMicTest).
let micTestGen = 0
// The test installed the push-to-talk listeners (no call had them).
let testOwnsPtt = false

// Accurate RMS volume calculator (time domain PCM audio)
function calculateRMSLevel(analyserNode: AnalyserNode | null, buffer: Uint8Array<ArrayBuffer>) {
  if (!analyserNode) return 0
  analyserNode.getByteTimeDomainData(buffer)
  let sum = 0
  for (let i = 0; i < buffer.length; i++) {
    const sample = ((buffer[i] ?? 128) - 128) / 128
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

function setNodeGain(gainNode: GainNode | null, val: number, time = 0) {
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
  onMediaUnprotected(() => {
    stopScreenShare()
    stopCamera()
    cleanupVoiceAudio()
  })

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
    return mode === 'browser' || (aiModel(mode) !== null && (!isNoiseSuppressionSupported() || aiModel(mode) === failedAIModel))
  }

  function recoverFilter(ctx: AudioContext, model: NoiseModel, test = false) {
    if ((test ? testAudioContext : audioContext) !== ctx || failedAIModel === model) return
    failedAIModel = model
    useToastStore().info(t('audio.filterDegraded'))
    // Allow the current setup to finish before acquiring the native fallback.
    // The old downstream mute/gate remains active throughout this transition.
    setTimeout(() => {
      if ((test ? testAudioContext : audioContext) !== ctx) return
      if (test) {
        if (voiceStore.isMicTesting) startMicTest().catch(() => {})
      } else if (localAudioStream.value && voiceStore.currentChannelId) {
        applyAudioSettings().catch(() => {})
      }
    }, 0)
  }

  function getAudioConstraints() {
    const audioConstraints: MediaTrackConstraints & { googEchoCancellation: boolean; googAutoGainControl: boolean; googNoiseSuppression: boolean; googHighpassFilter: boolean } = {
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
  function connectLoopback(ctx: AudioContext, source: AudioNode) {
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
        audioContext.resume().catch(() => {})
      }
      return true
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
      if (!stale()) voiceStore.isMicTesting = false
      return false
    }
    if (stale()) {
      stream.getTracks().forEach(tr => tr.stop())
      return false
    }
    testAudioStream = stream

    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext
      const ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
      testAudioContext = ctx
      const controller = new AbortController()
      micTestController = controller
      // Not awaited: without a user gesture resume() may never settle.
      if (ctx.state === 'suspended') ctx.resume().catch(() => {})

      const analyserNode = ctx.createAnalyser()
      analyserNode.fftSize = 256
      analyserNode.smoothingTimeConstant = 0.2

      let source: AudioNode = ctx.createMediaStreamSource(stream)
      // Meter the filtered signal so the gate threshold calibrates like in a call.
      const model = aiModel(voiceStore.noiseMode)
      if (model && model !== failedAIModel && isNoiseSuppressionSupported()) {
        const node = await createNoiseSuppressorNode(ctx, model, { signal: controller.signal, onFailure: () => recoverFilter(ctx, model, true) })
        if (stale()) {
          // stopMicTest already closed the context and stopped the stream.
          try { node?.destroy?.() } catch { /* ignore */ }
          return false
        }
        if (node) {
          source.connect(node)
          source = node
          testSuppressor = node
        } else recoverFilter(ctx, model, true)
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
      if (!pushToTalk.attached()) {
        pushToTalk.attach(voiceStore)
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
      return !stale()
    } catch (err) {
      console.warn('Mic test setup failed:', err)
      if (!stale()) {
        teardownTestPipeline()
        voiceStore.currentInputLevel = 0
        voiceStore.isMicTesting = false
      }
      return false
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
      return !!loopbackGain
    }

    // Not in a voice call: the test builds the loopback itself once it runs.
    if (testAudioContext && testBiquad) {
      connectLoopback(testAudioContext, testBiquad)
      return !!loopbackGain
    } else {
      const starting = startMicTest()
      const gen = micTestGen
      const started = await starting
      if (gen !== micTestGen) return false
      if (!started || !loopbackGain) voiceStore.isMicTesting = false
      return started && !!loopbackGain
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
    if (voiceStore.isMicTesting) { stopMicTest(); return Promise.resolve(false) }
    return startMicLoopback()
  }

  // Ends the standalone test pipeline (and any start still awaiting the mic).
  function teardownTestPipeline() {
    micTestGen++
    micTestController?.abort()
    micTestController = null
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
      if (!localAudioStream.value) pushToTalk.detach()
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
    failedAIModel = null
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }

    pushToTalk.detach()

    if (localAudioStream.value) {
      localAudioStream.value.getTracks().forEach(t => t.stop())
      localAudioStream.value = null
    }
    voiceStore.localAudioStream = null

    teardownMicPipeline()
    voiceStore.currentInputLevel = 0
  }

  // Queue only within the connection that received the offer. An old task
  // must never bind tracks or send an answer into a replacement connection.
  function handleRemoteOffer(offer: RTCSessionDescriptionInit) {
    const generation = joinGeneration
    const expectedConnection = pc
    signalingChain = signalingChain.then(() => applyRemoteOffer(offer, generation, expectedConnection))
    return signalingChain
  }

  async function applyRemoteOffer(offer: RTCSessionDescriptionInit, generation: number, expectedConnection: RTCPeerConnection | null) {
    if (!voiceStore.currentChannelId || generation !== joinGeneration || pc !== expectedConnection) return
    if (!pc) setupPeerConnection(voiceStore, chatStore)
    const conn = pc || setupPeerConnection(voiceStore, chatStore)
    const channelId = voiceStore.currentChannelId
    const isCurrent = () => pc === conn && generation === joinGeneration && voiceStore.currentChannelId === channelId

    try {
      // The screen's lower layers get the screen line's tuning, so every layer sends the same codec.
      const layerLines = Math.max(0, voiceStore.serverScreenLayers - 1)
      const sdp = tuneScreenLines(restrictVideoCodecs(offer.sdp || ''), publishMids(offer.sdp, layerLines), canSendH264())
      await conn.setRemoteDescription(new RTCSessionDescription({ type: offer.type, sdp }))
      if (!isCurrent()) return
      const bound = bindPublishSenders(conn, offer.sdp || '', layerLines)
      // Before the answer: a browser ignores a receiver transform that is set
      // after its line was set up, so the track event comes too late.
      for (const transceiver of conn.getTransceivers()) mediaCall().prepareIncoming(transceiver.receiver)
      const queued = pendingCandidates
      pendingCandidates = []
      for (const candidate of queued) {
        await conn.addIceCandidate(new RTCIceCandidate(candidate)).catch(() => {})
        if (!isCurrent()) return
      }
      const answer = await conn.createAnswer()
      if (!isCurrent()) return
      await conn.setLocalDescription(answer)
      if (!isCurrent()) return
      chatStore.sendWSEvent('webrtc_answer', answer)
      await tuneBoundSenders(bound.fresh, voiceStore)
      if (!isCurrent()) return
      // A screen waiting for negotiation starts with its chosen limits already
      // applied, avoiding a second encoder configuration immediately on start.
      const track = localScreenStream.value?.getVideoTracks()[0]
      if (bound.screen && track && bound.fresh.screen) {
        await bound.fresh.screen.replaceTrack(track)
        if (!isCurrent() || localScreenStream.value?.getVideoTracks()[0] !== track) return
        chatStore.sendWSEvent('webrtc_screenshare_start', {})
        chatStore.sendWSEvent('webrtc_request_keyframe', {})
      }
      if (bound.fresh.layers?.length) await syncLayers()
    } catch (err) {
      if (!isCurrent()) return
      console.warn('[WebRTC] Offer/Answer negotiation error:', err)
      chatStore.sendWSEvent('webrtc_diag', { event: 'negotiation_error', error: String(err instanceof Error ? err.message : err) })
    }
  }

  async function handleRemoteCandidate(candidate: RTCIceCandidateInit) {
    if (!candidate || !voiceStore.currentChannelId) return
    const conn = pc
    if (!conn || !conn.remoteDescription) {
      pendingCandidates.push(candidate)
      return
    }
    try {
      await conn.addIceCandidate(new RTCIceCandidate(candidate))
    } catch (err) {
      if (pc === conn) console.warn('[WebRTC] ICE candidate error:', err)
    }
  }

  async function joinVoiceChannel(channelId: string) {
    // The server refuses it too; saying so up front avoids a half-started call.
    if (!usePermissionsStore().canIn(channelId, 'connect')) {
      useToastStore().error(t('talk.noConnect'))
      return
    }
    // The native transport has no qualified protected-media owner yet.
    // Reject before changing connection state or requesting physical capture.
    if (platform.isDesktop()) {
      useToastStore().error(t('nativeDesktop.mediaUnavailable'))
      return
    }
    // If already in this channel, don't re-create audio graphs
    if (channelId === voiceStore.currentChannelId && localAudioStream.value) {
      return
    }
    resetMediaAnnouncements()
    closeMediaRoom()

    // Stop standalone mic test before joining voice
    stopMicTest()

    // Clean up any previous voice session to prevent audio thread leak
    cleanupVoiceAudio()

    chatStore.setWebRTCHandlers({
      onOffer: handleRemoteOffer,
      onCandidate: handleRemoteCandidate,
      onKicked: handleKicked,
      onMedia(payload) {
        if (payload.channel_id !== voiceStore.currentChannelId) return
        mediaCall().receive(payload.user_id, payload.wire)
      }
    })

    const gen = ++joinGeneration
    voiceStore.setChannel(channelId)
    const mediaUser = useAuthStore().user?.id
    if (mediaUser) {
      mediaCall().start(channelId, mediaUser)
      // Whoever the Talk already lists (also members whose reload is still in
      // its grace period): the group must not take the room for empty and
      // found its own before the roster changes for the first time.
      mediaCall().roster(Object.keys(voiceStore.channelUsers[channelId] || {}))
    }
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
    openMediaRoom()
    attachSubscriptions()

    if (sendStream) {
      startSpeakingDetection()
      pushToTalk.attach(voiceStore)
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
    if (platform.isDesktop()) return
    const channelId = voiceStore.currentChannelId
    if (!channelId) return
    // The server keeps the roster across a short disconnect, so nobody else
    // joins or leaves and a handshake that was dropped is not sent again.
    setupPeerConnection(voiceStore, chatStore)
    chatStore.sendWSEvent('voice_join', { channel_id: channelId })
    // After the join, so a package it produces is not dropped by the hub.
    mediaCall().resume()
    attachSubscriptions()
  }

  // Which video I receive is decided on the server: tell it my camera opt-outs
  // and screen opt-ins after every join, and forward later changes.
  function attachSubscriptions() {
    voiceStore.setSubscriptionSink((msg: SubscribeRequest) => chatStore.sendWSEvent('webrtc_subscribe', msg))
    voiceStore.resendSubscriptions()
    sendMuteState()
  }

  // Applies changed input settings (device, AGC, noise suppression, echo
  // cancellation). In a call the mic is re-acquired and swapped into the
  // running connection without renegotiation; otherwise the mic test restarts.
  // Runs are serialized: two overlapping swaps would each tear down the
  // other's pipeline.
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
    if (screenAudioSender) await applyQosToSender(screenAudioSender, voiceStore)
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
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }
    teardownMicPipeline()
    old.getTracks().forEach(t => t.stop())
    const sendStream = await setupMicPipeline(stream)
    if (gen !== joinGeneration || !sendStream) {
      // The new capture is not installed yet, so leaving can only stop the
      // previous localAudioStream. Release this run's own streams as well.
      sendStream?.getTracks().forEach(track => track.stop())
      stream.getTracks().forEach(track => track.stop())
      return
    }
    const track = sendStream.getAudioTracks()[0]
    if (track) track.enabled = !voiceStore.isMuted
    localAudioStream.value = sendStream
    voiceStore.localAudioStream = sendStream
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
  async function setupMicPipeline(stream: MediaStream): Promise<MediaStream | null> {
    const AudioCtx = window.AudioContext || window.webkitAudioContext
    let ctx
    try {
      ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
    } catch (err) {
      console.warn('AudioContext setup error:', err)
      if (platform.gamingBridgeToken() !== null) { stream.getTracks().forEach(track => track.stop()); return null }
      return stream
    }
    audioContext = ctx
    const controller = new AbortController()
    micPipelineController = controller
    // Not awaited: after a reload (voice resume) there is no user gesture yet
    // and resume() may stay pending until there is one; the graph starts then.
    if (ctx.state === 'suspended') ctx.resume().catch(() => {})

    let sendStream = stream
    let meterSource = null
    const model = aiModel(voiceStore.noiseMode)
    if (model && model !== failedAIModel && isNoiseSuppressionSupported()) {
      const node = await createNoiseSuppressorNode(ctx, model, { signal: controller.signal, onFailure: () => recoverFilter(ctx, model) })
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
      } else recoverFilter(ctx, model)
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

    // Only the desktop gaming bridge needs a native-key lease. The sent
    // microphone passes this gain even at 100% volume and without an AI filter.
    if (platform.gamingBridgeToken() !== null && window.location.protocol === 'https:') {
      try {
        const gain = ctx.createGain()
        const destination = ctx.createMediaStreamDestination()
        ctx.createMediaStreamSource(sendStream).connect(gain)
        gain.connect(destination)
        gamingMicLease(gain, ctx, voiceStore.inputMode === 'ptt', false)
        gamingGainNode = gain
        rawMicStream ??= stream
        sendStream = destination.stream
      } catch {
        stream.getTracks().forEach(track => track.stop())
        sendStream.getTracks().forEach(track => track.stop())
        teardownMicPipeline()
        return null // Never send an ungated desktop microphone after setup failure.
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
    if (gamingGainNode) {
      try { gamingGainNode.disconnect() } catch { /* already disconnected */ }
      gamingGainNode = null
    }
    micPipelineController?.abort()
    micPipelineController = null
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
    let lastGamingTick = performance.now()

    speakingInterval = setInterval(() => {
      const now = performance.now()
      if (gamingGainNode && voiceStore.inputMode === 'ptt' && now - lastGamingTick > 250) {
        // An overdue callback cannot renew the pre-stall pressed state. A
        // fresh native pulse or local key press must establish it again.
        voiceStore.isPttPressed = false
        setGamingPttLease(false)
      }
      lastGamingTick = now
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
      setGamingPttLease(voiceStore.isPttPressed)
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

  // The screen's sound ended (or sharing stopped): its line goes silent.
  async function detachScreenAudio() {
    voiceStore.hasScreenAudio = false
    if (screenAudioSender) await screenAudioSender.replaceTrack(null).catch(() => {})
  }

  // Asks the browser for a screen, captured in the quality chosen for the share.
  function pickScreen() {
    return navigator.mediaDevices.getDisplayMedia({
      video: trackConstraints(voiceStore.screenQuality),
      audio: screenAudioConstraints()
    })
  }

  // Sends a freshly picked screen (and its sound, on its own line).
  async function attachScreen(stream: MediaStream) {
    const gen = joinGeneration
    const captureGen = screenCaptureGeneration
    const currentCapture = () => gen === joinGeneration && captureGen === screenCaptureGeneration && toRaw(localScreenStream.value) === stream
    const videoTrack = stream.getVideoTracks()[0]
    if (videoTrack) {
      if ('contentHint' in videoTrack) videoTrack.contentHint = streamTuning(voiceStore.screenQuality).contentHint
      videoTrack.onended = () => { if (toRaw(localScreenStream.value) === stream) stopScreenShare() }
    }
    const screenAudio = stream.getAudioTracks()[0] || null
    const guard = screenAudio ? await ensureScreenAudioGuard(screenAudio) : null
    if (!currentCapture()) return
    screenAudioGuard = guard
    if (screenAudio) {
      // The streamer's mute: a disabled track sends silence.
      screenAudio.enabled = !voiceStore.isScreenAudioMuted
      screenAudio.onended = () => { if (toRaw(localScreenStream.value) === stream) detachScreenAudio() }
      if (guard === 'none' && !warnedScreenAudioTracks.has(screenAudio)) {
        warnedScreenAudioTracks.add(screenAudio)
        useToastStore().info(t('talk.screenAudioFeedbackWarning'), {
          action: {
            label: t('talk.muteStreamAudio'),
            onClick: () => {
              if (!currentCapture()) return
              voiceStore.isScreenAudioMuted = true
              screenAudio.enabled = false
            }
          }
        })
      }
    }
    voiceStore.hasScreenAudio = !!screenAudio

    if (screenSender && videoTrack) {
      const sender = screenSender
      const gen = joinGeneration
      const current = () => gen === joinGeneration && screenSender === sender && toRaw(localScreenStream.value) === stream
      // Set the chosen limits while the negotiated line is dormant. Attaching
      // first starts an encoder at the previous/default size, then immediately
      // asks it to reconfigure (particularly expensive for a 4K capture).
      const dormant = !sender.track
      if (dormant) await tuneSender(sender, screenSenderParams(voiceStore, videoTrack))
      if (!current()) return
      await sender.replaceTrack(videoTrack).catch(() => {})
      if (!current()) return
      if (!dormant) await tuneSender(sender, screenSenderParams(voiceStore, videoTrack))
      if (!current()) return
      chatStore.sendWSEvent('webrtc_screenshare_start', {})
      chatStore.sendWSEvent('webrtc_request_keyframe', {})
      await syncLayers()
      watchCapture(videoTrack, () => void retuneForCapture(videoTrack))
    }
    if (screenAudioSender) await screenAudioSender.replaceTrack(screenAudio).catch(() => {})
    if (!screenAudio) useToastStore().info(t('talk.noAudioInShareTip'))
  }

  // One screen share per person: starting another while sharing asks first;
  // the new screen then replaces the running one (see replaceScreenShare).
  // The lower layers of my screen share: sending the captured track smaller,
  // only the layers viewers need (until the server says, only the best), and
  // declaring them to the room. Runs one after another with quality changes.
  function syncLayers() {
    qualityChain = qualityChain.then(runSyncLayers).catch(() => {})
    return qualityChain
  }

  async function runSyncLayers() {
    if (!layerSenders.some(Boolean)) return
    const track = localScreenStream.value?.getVideoTracks()[0]
    const gen = joinGeneration
    const senders = [...layerSenders]
    if (!track || !screenSender) {
      for (const sender of senders) if (sender?.track) await sender.replaceTrack(null).catch(() => {})
      // The next share starts on its best layer alone, whatever this one was asked for.
      if (screenSender) await tuneSender(screenSender, { active: true })
      declaredKey = ''
      return
    }
    const plans = planScreenLayers(voiceStore.screenQuality, track.getSettings?.() || {})
    const wanted = voiceStore.layersWanted
    const active = (layer: number) => (wanted === null ? layer === 0 : wanted.includes(layer))
    const prio = qosPriority(voiceStore)
    const { degradationPreference } = streamTuning(voiceStore.screenQuality)
    await tuneSender(screenSender, { active: active(0) })
    for (let i = 0; i < senders.length; i++) {
      const sender = senders[i]
      const plan = plans[i + 1]
      if (!sender) continue
      if (!plan) {
        if (sender.track) await sender.replaceTrack(null).catch(() => {})
        continue
      }
      await tuneSender(sender, {
        active: active(i + 1), maxBitrate: plan.maxBitrate, maxFramerate: plan.maxFramerate, scaleResolutionDownBy: plan.scale,
        degradationPreference, priority: prio, networkPriority: prio
      })
      if (gen !== joinGeneration || toRaw(localScreenStream.value)?.getVideoTracks()[0] !== track) return
      if (sender.track !== track) await sender.replaceTrack(track).catch(() => {})
    }
    if (gen !== joinGeneration || !plans.length) return
    const declared = declaredLayers(plans)
    const key = layersKey(declared)
    if (key === declaredKey) return
    declaredKey = key
    chatStore.sendWSEvent('webrtc_layers', { layers: declared })
  }

  async function startScreenShare(quality?: StreamQuality): Promise<void> {
    if (platform.isDesktop()) {
      useToastStore().error(t('nativeDesktop.mediaUnavailable'))
      return
    }
    if (mediaUnprotected()) return
    if (screenCapturePending) return
    if (localScreenStream.value) {
      const ok = await confirm({
        title: t('talk.replaceShareTitle'),
        body: t('talk.replaceShareBody'),
        confirmLabel: t('talk.replaceShareConfirm'),
        cancelLabel: t('common.cancel'),
        danger: false
      })
      if (!ok || !localScreenStream.value) return
      return replaceScreenShare()
    }
    const gen = joinGeneration
    const captureGen = ++screenCaptureGeneration
    const channelId = voiceStore.currentChannelId
    if (quality) voiceStore.setScreenQuality(quality)
    else voiceStore.resetScreenQuality()
    let stream
    screenCapturePending = true
    try {
      stream = await pickScreen()
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
      return
    } finally {
      screenCapturePending = false
    }
    // Left or switched the call (or started twice) while the picker was open.
    if (gen !== joinGeneration || captureGen !== screenCaptureGeneration || voiceStore.currentChannelId !== channelId || localScreenStream.value) {
      stream.getTracks().forEach(tr => tr.stop())
      return
    }
    localScreenStream.value = stream
    voiceStore.localScreenStream = stream
    voiceStore.isScreenSharing = true
    await attachScreen(stream)
  }

  // Swaps the running share for a newly picked screen on the same lines: no
  // renegotiation, the viewers keep watching, the quality stays as chosen.
  async function replaceScreenShare(): Promise<void> {
    if (screenCapturePending) return
    const old = localScreenStream.value
    if (!old) return startScreenShare()
    const gen = joinGeneration
    const captureGen = ++screenCaptureGeneration
    let stream
    screenCapturePending = true
    try {
      stream = await pickScreen()
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
      return
    } finally {
      screenCapturePending = false
    }
    // Left, stopped or replaced meanwhile.
    if (gen !== joinGeneration || captureGen !== screenCaptureGeneration || localScreenStream.value !== old) {
      stream.getTracks().forEach(tr => tr.stop())
      return
    }
    localScreenStream.value = stream
    voiceStore.localScreenStream = stream
    await attachScreen(stream)
    old.getTracks().forEach(tr => {
      tr.onended = null
      tr.stop()
    })
  }

  // The capture changed size or rate: the best layer is sized for the new
  // picture, the lower ones are re-planned and the layers declared again (the
  // server and the viewers' pickers only hear of it if the sizes differ).
  async function retuneForCapture(track: MediaStreamTrack) {
    qualityChain = qualityChain.then(async () => {
      if (screenSender) await tuneSender(screenSender, screenSenderParams(voiceStore, track))
    }).catch(() => {})
    await syncLayers()
  }

  function stopScreenShare() {
    stopCaptureWatch()
    screenCaptureGeneration++
    const wasSharing = !!localScreenStream.value
    if (screenSender) screenSender.replaceTrack(null).catch(() => {})
    detachScreenAudio()
    // The next share starts again with its best layer only.
    voiceStore.layersWanted = null
    declaredKey = ''

    if (localScreenStream.value) {
      localScreenStream.value.getTracks().forEach(tr => tr.stop())
      localScreenStream.value = null
    }
    voiceStore.localScreenStream = null
    voiceStore.isScreenSharing = false
    voiceStore.hasScreenAudio = false
    voiceStore.isScreenAudioMuted = false
    voiceStore.resetScreenQuality()
    if (wasSharing) chatStore.sendWSEvent('webrtc_screenshare_stop', {})
    void syncLayers()
  }

  // What the screen sender sends right now (RTCRtpSender.getStats), for the
  // streamer's "Advanced" info; null without a share.
  async function getScreenSendStats(): Promise<MediaStat[] | null> {
    if (!screenSender?.getStats || !localScreenStream.value) return null
    try {
      return [...(await screenSender.getStats()).values()]
    } catch {
      return null
    }
  }

  // A track setting is the capture's configured rate, not measured frame
  // delivery. The advanced menu displays media-source FPS separately.
  function getScreenCaptureSettings(): MediaTrackSettings | null {
    const settings = localScreenStream.value?.getVideoTracks()[0]?.getSettings?.()
    return settings ? {
      ...(settings.width === undefined ? {} : { width: settings.width }),
      ...(settings.height === undefined ? {} : { height: settings.height }),
      ...(settings.frameRate === undefined ? {} : { frameRate: settings.frameRate })
    } : null
  }

  function toggleScreenShare() {
    return voiceStore.isScreenSharing ? stopScreenShare() : startScreenShare()
  }

  // Tells the user why the camera failed instead of always blaming permissions.
  function cameraErrorKey(err: unknown) {
    switch (typeof err === 'object' && err !== null && 'name' in err ? err.name : undefined) {
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
    if (platform.isDesktop()) {
      useToastStore().error(t('nativeDesktop.mediaUnavailable'))
      return
    }
    if (mediaUnprotected()) return
    if (!voiceStore.currentChannelId || localCameraStream.value) return
    const gen = joinGeneration
    const captureGen = cameraCaptureGeneration
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
    // Left the call, or the call became unprotected, while the browser was asking.
    if (gen !== joinGeneration || captureGen !== cameraCaptureGeneration || localCameraStream.value) {
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
    cameraCaptureGeneration++
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
  function endCall({ notifyServer }: { notifyServer: boolean }) {
    joinGeneration++
    resetMediaAnnouncements()
    closeMediaRoom()
    mediaCall().stop()
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

  // An admin removed me, or the server refused a full room. The server has
  // already ended or rejected membership, so no voice_leave goes out.
  function handleKicked(payload: ServerEventPayloads['voice_kicked']) {
    if (!voiceStore.currentChannelId) return
    if (payload.channel_id !== voiceStore.currentChannelId) return
    endCall({ notifyServer: false })
    useToastStore().info(t(payload.reason === 'room_full' ? 'voice.roomFull' : payload.reason === 'missing_permission' ? 'voice.noPermission' : 'voice.kicked'))
  }

  // Rejoins the channel of a call interrupted by a reload less than 30 s ago.
  // Must run once the WebSocket is connected (voice_join goes over it).
  async function resumeVoiceSession() {
    if (platform.isDesktop()) return false
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
    resumeRemotePlayback(voiceStore)
  }

  // These watchers exist once per voice store, however many components call
  // useWebRTC(): one per caller would send the mute state and rebuild the
  // microphone several times over. They live in a detached scope, so they
  // keep running after the first caller unmounts; a new store (a new Pinia,
  // as in tests) replaces them.
  if (watchedStore !== voiceStore) {
    watcherScope?.stop()
    watchedStore = voiceStore
    followOutputDevice(voiceStore)
    watcherScope = effectScope(true)
    watcherScope.run(() => {
      watch(() => voiceStore.selectedOutputDeviceId, () => {
        applyOutputToRemote(voiceStore)
        // The mic-test monitor.
        applyOutput(voiceStore, loopbackCtx)
      })

      // Input volume: adjust the gain live; the first change away from 100 %
      // rebuilds the pipeline once to put the gain stage in.
      watch(() => voiceStore.inputMode, () => { voiceStore.isPttPressed = false; setGamingPttLease(false) }, { flush: 'sync' })
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
        () => voiceStore.localMutedUsers,
        () => voiceStore.streamVolumes,
        () => voiceStore.mutedStreams
      ], () => {
        updateRemoteVolume(voiceStore)
      })

      // The streamer's mute of their own share's sound: silence on its line.
      watch(() => voiceStore.isScreenAudioMuted, (muted) => {
        const track = liveScreenAudioTrack()
        if (track) track.enabled = !muted
      })

      // The chosen stream quality, applied to the running share.
      watch(() => voiceStore.screenQuality, () => { void applyScreenQuality(voiceStore).then(syncLayers) })
      // The layers viewers need: the others stop being encoded and sent.
      watch(() => voiceStore.layersWanted, () => { void syncLayers() })

      // Everyone sees whether I am muted or deafened.
      watch(() => [voiceStore.isMuted, voiceStore.isDeafened], sendMuteState)

      const call = mediaCall()
      call.onKey(() => {
        if (!voiceStore.currentChannelId) return
        chatStore.sendWSEvent('webrtc_request_keyframe', {})
      })
      call.onWire((channelId, wire) => {
        queueMediaWire(channelId, wire)
      })
      call.onNotice(announceMediaRekeyed)
      call.onVerification((verification) => { voiceStore.mediaVerification = verification })
      watch(() => {
        const channelId = voiceStore.currentChannelId
        if (!channelId) return ''
        return Object.keys(voiceStore.channelUsers[channelId] || {}).sort().join('\0')
      }, (packed) => {
        mediaCall().roster(packed ? packed.split('\0') : [])
      })
    })
  }

  return {
    localAudioStream,
    localScreenStream,
    localCameraStream,
    refreshAudioDevices,
    startMicTest,
    stopMicTest,
    toggleMicTest,
    joinVoiceChannel,
    leaveVoiceChannel,
    rejoinAfterReconnect,
    applyAudioSettings,
    startScreenShare,
    replaceScreenShare,
    stopScreenShare,
    toggleScreenShare,
    getScreenSendStats,
    getScreenCaptureSettings,
    startCamera,
    stopCamera,
    toggleCamera,
    resumeVoiceSession,
    resumeRemoteAudio
  }
}
