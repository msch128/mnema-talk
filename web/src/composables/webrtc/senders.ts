import { mediaCall } from '../../lib/media/api'
import { trackConstraints, streamEncoding, streamTuning } from '../../lib/streamQuality'
import { SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, type FrameSource } from '../../lib/media/frame'
import { publishMids } from './sdp'
import { callDtx } from './dtx'
import { declareCameraLayers } from './cameraLayers'
import { announceMediaUnprotected } from './mediaRoom'
import type { Cx } from './state'
import type { BoundSenders, SenderTuning } from './senderTuning'
import { CAMERA_MAX_BITRATE, tuneSender } from './senderTuning'

// The track that goes out as "audio": the (processed) microphone.
export function activeAudioTrack(cx: Cx) {
  const { st } = cx
  return st.localAudioStream.value?.getAudioTracks()[0] || null
}

// The sound of my running screen share, while it lasts.
export function liveScreenAudioTrack(cx: Cx) {
  const { st } = cx
  const track = st.localScreenStream.value?.getAudioTracks()[0]
  return track && track.readyState !== 'ended' ? track : null
}

export function qosPriority(cx: Cx): RTCPriorityType {
  const { voice } = cx
  return voice?.qosHighPriority ? 'high' : 'medium'
}

// The screen sender's parameters for the chosen stream quality, at the size
// the capture delivers now.
export function screenSenderParams(cx: Cx, track = cx.st.localScreenStream.value?.getVideoTracks()[0]) {
  const { voice } = cx
  const prio = qosPriority(cx)
  const quality = voice?.screenQuality
  const settings = track?.getSettings?.() || {}
  const { degradationPreference } = streamTuning(quality)
  return { ...streamEncoding(quality, settings), degradationPreference, priority: prio, networkPriority: prio }
}

export function cameraSenderParams(cx: Cx): SenderTuning {
  const prio = qosPriority(cx)
  return { maxBitrate: CAMERA_MAX_BITRATE, degradationPreference: 'balanced', priority: prio, networkPriority: prio }
}

async function applyQosToSender(cx: Cx, sender: RTCRtpSender | null) {
  if (!sender) return
  const prio = qosPriority(cx)
  await tuneSender(sender, { priority: prio, networkPriority: prio })
}

export async function applyQos(cx: Cx) {
  const { st } = cx
  if (st.audioSender) await applyQosToSender(cx, st.audioSender)
  if (st.screenSender) await applyQosToSender(cx, st.screenSender)
  if (st.cameraSender) await applyQosToSender(cx, st.cameraSender)
  if (st.screenAudioSender) await applyQosToSender(cx, st.screenAudioSender)
}

// Applies the chosen stream quality to the running share: the capture is
// constrained (frame rate, height) and the sender re-tuned, without
// restarting the share. Where the browser cannot scale the capture, the
// encoder scales it down (see streamEncoding).
export function applyScreenQuality(cx: Cx) {
  const { voice, st } = cx
  // A preset selected before the picker opens belongs to the next capture.
  // Do not let its queued watcher find that future track and configure it a
  // second time immediately after attachScreen has prepared the encoder.
  const track = st.localScreenStream.value?.getVideoTracks()[0]
  const quality = { ...voice.screenQuality }
  st.qualityChain = st.qualityChain.then(async () => {
    if (!track || track !== st.localScreenStream.value?.getVideoTracks()[0]) return
    // A capture that cannot be constrained live (some engines limit it) is scaled by the encoder instead; the menu then offers a restart.
    try {
      if (typeof track.applyConstraints !== 'function') throw new Error('applyConstraints unsupported')
      await track.applyConstraints(trackConstraints(quality))
      voice.screenQualityNeedsRestart = false
    } catch (err) {
      console.debug('[WebRTC] Could not constrain the shared screen:', err)
      voice.screenQualityNeedsRestart = true
    }
    if ('contentHint' in track) track.contentHint = streamTuning(quality).contentHint
    if (st.screenSender && track === st.localScreenStream.value?.getVideoTracks()[0]) {
      await tuneSender(st.screenSender, screenSenderParams(cx))
      // Viewers get a fresh picture at the new size instead of waiting for the next key frame. The server
      // asks for the sharer's own screen on screenshare_start; webrtc_request_keyframe is for videos the sender watches.
      cx.chat.sendWSEvent('webrtc_screenshare_start', {})
    }
  }).catch(() => {})
  return st.qualityChain
}

/** Switches the offer's own transceiver with this mid to send, protected for its source. */
function bindTransceiver(conn: RTCPeerConnection, mid: string | null | undefined, track: MediaStreamTrack | null | undefined, source: FrameSource, layer?: number, dtx?: boolean) {
  const tr = mid === null || mid === undefined ? null : conn.getTransceivers().find(t => t.mid === mid)
  if (!tr) return null
  if (!mediaCall().protectSender(tr.sender, source, layer, dtx)) {
    announceMediaUnprotected()
    return null
  }
  tr.direction = 'sendrecv'
  if (track) tr.sender.replaceTrack(track).catch(() => {})
  return tr.sender
}

type Bound = { screen: boolean; camera: boolean; fresh: BoundSenders }

/** Binds the sender slot if it is still empty. Resolves to whether it is bound now. */
function bindSlot(
  cx: Cx,
  conn: RTCPeerConnection,
  slot: 'audioSender' | 'screenSender' | 'cameraSender' | 'screenAudioSender',
  freshKey: 'audio' | 'screen' | 'camera' | 'screenAudio',
  line: { mid: string | null | undefined; track: MediaStreamTrack | null | undefined; source: FrameSource; layer?: number; dtx?: boolean },
  bound: Bound
) {
  const { st } = cx
  if (st[slot]) return false
  const sender = bindTransceiver(conn, line.mid, line.track, line.source, line.layer, line.dtx)
  st[slot] = sender
  if (sender) bound.fresh[freshKey] = sender
  return !!sender
}

/**
 * Sends on the SFU's receive lines. Transceivers created locally with
 * addTransceiver are never matched to a remote offer's m-lines, so media put
 * on them would never reach the server; the offer's own transceivers are
 * switched to send instead, once, right after the offer is applied.
 */
export function bindPublishSenders(cx: Cx, conn: RTCPeerConnection, sdp: string, layerLines = 0, cameraLayerLines = 0) {
  const { st } = cx
  const mids = publishMids(sdp, layerLines, cameraLayerLines)
  // fresh: senders bound by this call, which still need their parameters.
  const bound: Bound = { screen: false, camera: false, fresh: {} }
  bindSlot(cx, conn, 'audioSender', 'audio', { mid: mids.audio, track: activeAudioTrack(cx), source: SOURCE_MIC, dtx: callDtx(cx) }, bound)
  if (bindSlot(cx, conn, 'screenSender', 'screen', { mid: mids.video[0], track: null, source: SOURCE_SCREEN, layer: 0 }, bound)) {
    bound.screen = !!st.localScreenStream.value
  }
  const cameraTrack = st.localCameraStream.value?.getVideoTracks()[0]
  // A running camera is sent as layers from the start: the server hears of them before its video.
  if (cameraTrack && cameraLayerLines > 0) declareCameraLayers(cx, cameraTrack, true)
  if (bindSlot(cx, conn, 'cameraSender', 'camera', { mid: mids.video[1], track: cameraTrack, source: SOURCE_CAMERA, ...(cameraLayerLines > 0 ? { layer: 0 } : {}) }, bound)) {
    bound.camera = !!st.localCameraStream.value
  }
  bindSlot(cx, conn, 'screenAudioSender', 'screenAudio', { mid: mids.screenAudio, track: liveScreenAudioTrack(cx), source: SOURCE_SCREEN_AUDIO }, bound)
  // The lower layers of the screen share: lines an older server does not offer.
  for (let i = 0; i < Math.min(st.layerSenders.length, layerLines); i++) {
    if (st.layerSenders[i]) continue
    const sender = bindTransceiver(conn, mids.video[2 + i], null, SOURCE_SCREEN, i + 1)
    st.layerSenders[i] = sender
    if (sender) (bound.fresh.layers ??= []).push(sender)
  }
  // The same for the camera's, on the lines after those.
  for (let i = 0; i < Math.min(st.cameraLayerSenders.length, cameraLayerLines); i++) {
    if (st.cameraLayerSenders[i]) continue
    const sender = bindTransceiver(conn, mids.cameraLayers[i], null, SOURCE_CAMERA, i + 1)
    st.cameraLayerSenders[i] = sender
    if (sender) (bound.fresh.cameraLayers ??= []).push(sender)
  }
  return bound
}

// Bitrate, degradation, quality and priority of a freshly bound sender: the
// same a share or camera started in a running connection gets, so a
// reconnect or a channel switch keeps them (the chosen stream quality too).
export async function tuneBoundSenders(cx: Cx, fresh: BoundSenders) {
  if (fresh.audio) await applyQosToSender(cx, fresh.audio)
  if (fresh.screen) await tuneSender(fresh.screen, screenSenderParams(cx))
  if (fresh.camera) await tuneSender(fresh.camera, cameraSenderParams(cx))
  if (fresh.screenAudio) await applyQosToSender(cx, fresh.screenAudio)
  // The lower layers are tuned by syncLayers once the share's size is known.
}

