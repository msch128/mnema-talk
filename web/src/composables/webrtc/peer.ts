import { mediaCall } from '../../lib/media/api'
import { SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO } from '../../lib/media/frame'
import { publishMids, restrictVideoCodecs, tuneScreenLines } from './sdp'
import { iceServers } from './iceServers'
import { attachRemoteAudio, releaseRemoteAudio } from './remoteAudio'
import { CAMERA_STREAM_PREFIX, SCREEN_AUDIO_STREAM_PREFIX, publisherId } from './streamIds'
import { announceMediaUnprotected } from './mediaRoom'
import type { Cx } from './state'
import { stopCaptureWatch, syncLayers } from './layers'
import { bindPublishSenders, tuneBoundSenders } from './senders'

export function cleanupPeerConnection(cx: Cx) {
  const { voice, st } = cx
  stopCaptureWatch(cx)
  st.statsPolling.stop(voice)
  st.signalingChain = Promise.resolve()
  st.pendingCandidates = []
  if (st.pc) {
    try { st.pc.close() } catch { /* connection already closed */ }
    st.pc = null
  }
  releaseRemoteAudio()
  st.audioSender = null
  st.screenSender = null
  st.cameraSender = null
  st.screenAudioSender = null
  st.layerSenders = [null, null]
  st.declaredKey = ''
  voice?.resetRemoteMedia()
}

// Connection diagnostics for the server log: what this browser gathers and
// how the connection develops (candidate types only, never addresses).
function wireIceDiagnostics(cx: Cx, conn: RTCPeerConnection) {
  const { chat, st } = cx
  const diag: { candidates: Record<string, number>; errors: string[] } = { candidates: {}, errors: [] }
  const reportDiag = (event: string) => st.pc === conn && chat.sendWSEvent('webrtc_diag', {
    event,
    gathering: st.pc?.iceGatheringState,
    ice: st.pc?.iceConnectionState,
    connection: st.pc?.connectionState,
    signaling: st.pc?.signalingState,
    candidates: diag.candidates,
    errors: diag.errors.slice(-5),
    ice_servers: iceServers().length,
    ua: typeof navigator !== 'undefined' ? navigator.userAgent : ''
  })

  conn.onicecandidate = (event) => {
    if (st.pc !== conn) return
    if (event.candidate) {
      const key = `${event.candidate.protocol || '?'}/${event.candidate.type || '?'}`
      diag.candidates[key] = (diag.candidates[key] || 0) + 1
      chat.sendWSEvent('webrtc_candidate', event.candidate.toJSON())
    }
  }
  conn.onicecandidateerror = (event) => {
    if (st.pc !== conn) return
    diag.errors.push(`${event.errorCode} ${event.errorText || ''} ${event.url || ''}`.trim())
  }
  conn.onicegatheringstatechange = () => reportDiag('gathering')
  conn.oniceconnectionstatechange = () => {
    if (['connected', 'failed', 'disconnected'].includes(st.pc?.iceConnectionState || '')) reportDiag('ice')
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
    await conn.addIceCandidate(new RTCIceCandidate(candidate)).catch(() => {})
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

async function applyRemoteOffer(cx: Cx, offer: RTCSessionDescriptionInit, generation: number, expectedConnection: RTCPeerConnection | null) {
  const { voice, chat, st } = cx
  if (!voice.currentChannelId || generation !== st.joinGeneration || st.pc !== expectedConnection) return
  if (!st.pc) setupPeerConnection(cx)
  const conn = st.pc || setupPeerConnection(cx)
  const channelId = voice.currentChannelId
  const isCurrent = () => st.pc === conn && generation === st.joinGeneration && voice.currentChannelId === channelId

  try {
    // The screen's lower layers get the screen line's tuning, so every layer sends the same codec.
    const layerLines = Math.max(0, voice.serverScreenLayers - 1)
    const sdp = tuneScreenLines(restrictVideoCodecs(offer.sdp || ''), publishMids(offer.sdp, layerLines), canSendH264())
    await conn.setRemoteDescription(new RTCSessionDescription({ type: offer.type, sdp }))
    if (!isCurrent()) return
    const bound = bindPublishSenders(cx, conn, offer.sdp || '', layerLines)
    // Before the answer: a browser ignores a receiver transform that is set
    // after its line was set up, so the track event comes too late.
    for (const transceiver of conn.getTransceivers()) mediaCall().prepareIncoming(transceiver.receiver)
    if (!(await flushQueuedCandidates(cx, conn, isCurrent))) return
    const answer = await conn.createAnswer()
    if (!isCurrent()) return
    await conn.setLocalDescription(answer)
    if (!isCurrent()) return
    chat.sendWSEvent('webrtc_answer', answer)
    await tuneBoundSenders(cx, bound.fresh)
    if (!isCurrent()) return
    if (!(await startWaitingScreen(cx, bound, isCurrent))) return
    if (bound.fresh.layers?.length) await syncLayers(cx)
  } catch (err) {
    if (!isCurrent()) return
    console.warn('[WebRTC] Offer/Answer negotiation error:', err)
    chat.sendWSEvent('webrtc_diag', { event: 'negotiation_error', error: String(err instanceof Error ? err.message : err) })
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

