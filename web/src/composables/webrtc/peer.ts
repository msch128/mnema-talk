import { mediaCall } from '../../lib/media/api'
import { SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO } from '../../lib/media/frame'
import { canSendH264 } from './h264Support'
import { wantsDtx } from './dtx'
import { enableMicDtx, publishMids, restrictVideoCodecs, tuneScreenLines } from './sdp'
import { iceServers, relayOffered } from './iceServers'
import { attachRemoteAudio, releaseRemoteAudio } from './remoteAudio'
import { CAMERA_STREAM_PREFIX, SCREEN_AUDIO_STREAM_PREFIX, publisherId } from './streamIds'
import { announceMediaUnprotected, announceNoMediaConnection } from './mediaRoom'
import type { Cx } from './state'
import { stopCaptureWatch, syncLayers } from './layers'
import { syncCameraLayers } from './cameraLayerSync'
import { bindPublishSenders, tuneBoundSenders } from './senders'

export function cleanupPeerConnection(cx: Cx) {
  const { voice, st } = cx
  stopCaptureWatch(cx)
  st.statsPolling.stop(voice)
  st.signalingChain = Promise.resolve()
  st.pendingCandidates = []
  if (st.pc) {
    try { st.pc.close() } catch (err) { void err }
    st.pc = null
  }
  releaseRemoteAudio()
  st.audioSender = null
  st.screenSender = null
  st.cameraSender = null
  st.screenAudioSender = null
  st.layerSenders = [null, null]
  st.cameraLayerSenders = [null, null]
  st.declaredKey = ''
  st.declaredCameraKey = ''
  voice?.resetRemoteMedia()
}

function candidateKey(candidate: RTCIceCandidate): string {
  const protocol = candidate.protocol || '?'
  const type = candidate.type || '?'
  return `${protocol}/${type}`
}

function formatCandidateError(event: RTCPeerConnectionIceErrorEvent): string {
  const text = event.errorText || ''
  const url = event.url || ''
  return `${event.errorCode} ${text} ${url}`.trim()
}

const REPORTABLE_ICE_STATES = new Set(['connected', 'failed', 'disconnected'])

function createDiagnosticsReporter(cx: Cx, conn: RTCPeerConnection, diag: { candidates: Record<string, number>; errors: string[] }) {
  const { chat, st } = cx
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  return (event: string) => {
    if (st.pc !== conn) return
    chat.sendWSEvent('webrtc_diag', {
      event,
      gathering: st.pc?.iceGatheringState,
      ice: st.pc?.iceConnectionState,
      connection: st.pc?.connectionState,
      signaling: st.pc?.signalingState,
      candidates: diag.candidates,
      errors: diag.errors.slice(-5),
      ice_servers: iceServers().length,
      ua
    })
  }
}

// Connection diagnostics for the server log: what this browser gathers and
// how the connection develops (candidate types only, never addresses).
function wireIceDiagnostics(cx: Cx, conn: RTCPeerConnection) {
  const { chat, st } = cx
  const diag: { candidates: Record<string, number>; errors: string[] } = { candidates: {}, errors: [] }
  const reportDiag = createDiagnosticsReporter(cx, conn, diag)

  conn.onicecandidate = (event) => {
    if (st.pc !== conn || !event.candidate) return
    const key = candidateKey(event.candidate)
    diag.candidates[key] = (diag.candidates[key] || 0) + 1
    chat.sendWSEvent('webrtc_candidate', event.candidate.toJSON())
  }
  conn.onicecandidateerror = (event) => {
    if (st.pc !== conn) return
    diag.errors.push(formatCandidateError(event))
  }
  conn.onicegatheringstatechange = () => reportDiag('gathering')
  conn.oniceconnectionstatechange = () => {
    const state = st.pc?.iceConnectionState
    if (state && REPORTABLE_ICE_STATES.has(state)) reportDiag('ice')
    if (state === 'failed' && st.pc === conn) announceNoMediaConnection(relayOffered())
  }
}

// What the line carries, as the SFU names it. A sealed frame says what its
// sender made it for, and only a frame that agrees is opened.
function trackSource(kind: string, streamId: string | undefined) {
  if (kind === 'video') return streamId?.startsWith(CAMERA_STREAM_PREFIX) ? SOURCE_CAMERA : SOURCE_SCREEN
  return streamId?.startsWith(SCREEN_AUDIO_STREAM_PREFIX) ? SOURCE_SCREEN_AUDIO : SOURCE_MIC
}

// A remote camera or screen: shown in the room until its track ends or the SFU stops forwarding it.
function showRemoteVideo(cx: Cx, event: RTCTrackEvent, streamId: string | undefined) {
  const { voice, chat } = cx
  const stream = new MediaStream([event.track])
  const isCamera = !!streamId?.startsWith(CAMERA_STREAM_PREFIX)
  const userId = isCamera ? streamId?.slice(CAMERA_STREAM_PREFIX.length) : streamId
  let drop
  if (isCamera) {
    if (userId) voice.setUserVideoStream(userId, stream)
    drop = () => { if (userId) voice.removeUserVideoStream(userId, stream) }
  } else {
    if (userId) voice.setRemoteScreen(userId, stream)
    drop = () => { if (userId) voice.removeRemoteScreen(userId, stream) }
  }
  chat.sendWSEvent('webrtc_request_keyframe', {})

  event.track.onended = drop
  // When the SFU stops forwarding, the browser removes the track from its
  // stream instead of ending it.
  event.streams?.[0]?.addEventListener?.('removetrack', (e) => {
    if (e.track === event.track) drop()
  })
}

function handleRemoteTrack(cx: Cx, event: RTCTrackEvent) {
  // The SFU names the publishing user in the stream ID. No name, no playback.
  const streamId = event.streams?.[0]?.id
  const senderId = publisherId(streamId)
  if (!senderId) return
  if (!mediaCall().protectIncoming(event.receiver, senderId, trackSource(event.track.kind, streamId))) {
    announceMediaUnprotected()
    return
  }
  if (event.track.kind === 'audio') {
    attachRemoteAudio(cx.voice, event.track, event.streams?.[0])
  } else if (event.track.kind === 'video') {
    showRemoteVideo(cx, event, streamId)
  }
}

export function setupPeerConnection(cx: Cx) {
  const { voice, chat, st } = cx
  cleanupPeerConnection(cx)

  const rtcConfig: RTCConfiguration = { iceServers: iceServers() }
  if (voice?.qosHighPriority) {
    rtcConfig.dscp = true
  }
  st.pc = new RTCPeerConnection(rtcConfig)
  const conn = st.pc
  st.statsPolling.start(voice, payload => chat.sendWSEvent('webrtc_diag', payload))
  wireIceDiagnostics(cx, conn)
  conn.ontrack = (event) => {
    if (st.pc !== conn) return
    handleRemoteTrack(cx, event)
  }

  // The outgoing lines (microphone, screen, camera) are the SFU's own receive
  // lines from its offer; see bindPublishSenders.
  return st.pc
}

// Queue only within the connection that received the offer. An old task
// must never bind tracks or send an answer into a replacement connection.
export function handleRemoteOffer(cx: Cx, offer: RTCSessionDescriptionInit) {
  const { st } = cx
  const generation = st.joinGeneration
  const expectedConnection = st.pc
  st.signalingChain = st.signalingChain.then(() => applyRemoteOffer(cx, offer, generation, expectedConnection))
  return st.signalingChain
}

/** Adds the candidates that arrived before the offer. Resolves to false once the connection is no longer current. */
async function flushQueuedCandidates(cx: Cx, conn: RTCPeerConnection, isCurrent: () => boolean) {
  const { st } = cx
  const queued = st.pendingCandidates
  st.pendingCandidates = []
  for (const candidate of queued) {
    await conn.addIceCandidate(new RTCIceCandidate(candidate)).catch(err => { void err })
    if (!isCurrent()) return false
  }
  return true
}

// A screen waiting for negotiation starts with its chosen limits already
// applied, avoiding a second encoder configuration immediately on start.
async function startWaitingScreen(cx: Cx, bound: ReturnType<typeof bindPublishSenders>, isCurrent: () => boolean) {
  const { chat, st } = cx
  const track = st.localScreenStream.value?.getVideoTracks()[0]
  if (!bound.screen || !track || !bound.fresh.screen) return true
  await bound.fresh.screen.replaceTrack(track)
  if (!isCurrent() || st.localScreenStream.value?.getVideoTracks()[0] !== track) return false
  chat.sendWSEvent('webrtc_screenshare_start', {})
  chat.sendWSEvent('webrtc_request_keyframe', {})
  return true
}

function shouldApplyOffer(cx: Cx, generation: number, expected: RTCPeerConnection | null): boolean {
  const { voice, st } = cx
  if (!voice.currentChannelId) return false
  if (generation !== st.joinGeneration) return false
  return st.pc === expected
}

function ensurePeerConnection(cx: Cx): RTCPeerConnection {
  if (!cx.st.pc) setupPeerConnection(cx)
  return cx.st.pc || setupPeerConnection(cx)
}

function prepareRemoteSdp(offerSdp: string | undefined, layerLines: number, cameraLayerLines: number, dtx: boolean): string {
  const raw = offerSdp || ''
  const mids = publishMids(raw, layerLines, cameraLayerLines)
  const tuned = tuneScreenLines(restrictVideoCodecs(raw), mids, canSendH264())
  return dtx ? enableMicDtx(tuned, mids.audio) : tuned
}

function prepareTransceiverTransforms(conn: RTCPeerConnection) {
  for (const transceiver of conn.getTransceivers()) {
    mediaCall().prepareIncoming(transceiver.receiver)
  }
}

async function publishAnswer(conn: RTCPeerConnection, chat: Cx['chat'], isCurrent: () => boolean): Promise<boolean> {
  const answer = await conn.createAnswer()
  if (!isCurrent()) return false
  await conn.setLocalDescription(answer)
  if (!isCurrent()) return false
  chat.sendWSEvent('webrtc_answer', answer)
  return true
}

async function finishOfferNegotiation(
  cx: Cx,
  bound: ReturnType<typeof bindPublishSenders>,
  isCurrent: () => boolean
): Promise<void> {
  await tuneBoundSenders(cx, bound.fresh)
  if (!isCurrent()) return
  if (!(await startWaitingScreen(cx, bound, isCurrent))) return
  if (bound.fresh.layers?.length) {
    await syncLayers(cx)
  }
  if (bound.fresh.cameraLayers?.length) {
    await syncCameraLayers(cx)
  }
}

function reportNegotiationError(chat: Cx['chat'], err: unknown) {
  console.warn('[WebRTC] Offer/Answer negotiation error:', err)
  const message = err instanceof Error ? err.message : String(err)
  chat.sendWSEvent('webrtc_diag', { event: 'negotiation_error', error: message })
}

function createCurrentChecker(cx: Cx, conn: RTCPeerConnection, generation: number, channelId: string): () => boolean {
  return () => {
    if (cx.st.pc !== conn) return false
    if (cx.st.joinGeneration !== generation) return false
    return cx.voice.currentChannelId === channelId
  }
}

async function executeOfferNegotiation(
  cx: Cx,
  conn: RTCPeerConnection,
  offer: RTCSessionDescriptionInit,
  isCurrent: () => boolean
): Promise<void> {
  const layerLines = Math.max(0, cx.voice.serverScreenLayers - 1)
  const cameraLayerLines = Math.max(0, cx.voice.serverCameraLayers - 1)
  const sdp = prepareRemoteSdp(offer.sdp, layerLines, cameraLayerLines, wantsDtx(cx.voice))
  await conn.setRemoteDescription(new RTCSessionDescription({ type: offer.type, sdp }))
  if (!isCurrent()) return
  const bound = bindPublishSenders(cx, conn, offer.sdp || '', layerLines, cameraLayerLines)
  prepareTransceiverTransforms(conn)
  if (!(await flushQueuedCandidates(cx, conn, isCurrent))) return
  if (!(await publishAnswer(conn, cx.chat, isCurrent))) return
  await finishOfferNegotiation(cx, bound, isCurrent)
}

async function applyRemoteOffer(
  cx: Cx,
  offer: RTCSessionDescriptionInit,
  generation: number,
  expectedConnection: RTCPeerConnection | null
) {
  if (!shouldApplyOffer(cx, generation, expectedConnection)) return
  const conn = ensurePeerConnection(cx)
  const isCurrent = createCurrentChecker(cx, conn, generation, cx.voice.currentChannelId || '')

  try {
    await executeOfferNegotiation(cx, conn, offer, isCurrent)
  } catch (err) {
    if (isCurrent()) reportNegotiationError(cx.chat, err)
  }
}

export async function handleRemoteCandidate(cx: Cx, candidate: RTCIceCandidateInit) {
  const { voice, st } = cx
  if (!candidate || !voice.currentChannelId) return
  const conn = st.pc
  if (!conn || !conn.remoteDescription) {
    st.pendingCandidates.push(candidate)
    return
  }
  try {
    await conn.addIceCandidate(new RTCIceCandidate(candidate))
  } catch (err) {
    if (st.pc === conn) console.warn('[WebRTC] ICE candidate error:', err)
  }
}

