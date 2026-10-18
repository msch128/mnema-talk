import { mediaCall } from '../../lib/media/api'
import { trackConstraints, streamEncoding, streamTuning } from '../../lib/streamQuality'
import { SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, type FrameSource } from '../../lib/media/frame'
import { publishMids } from './sdp'
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
    try {
      await track.applyConstraints?.(trackConstraints(quality))
    } catch (err) {
      console.debug('[WebRTC] Could not constrain the shared screen:', err)
    }
    if ('contentHint' in track) track.contentHint = streamTuning(quality).contentHint
    if (st.screenSender && track === st.localScreenStream.value?.getVideoTracks()[0]) {
      await tuneSender(st.screenSender, screenSenderParams(cx))
    }
  }).catch(() => {})
  return st.qualityChain
}

/**
 * Sends on the SFU's receive lines. Transceivers created locally with
 * addTransceiver are never matched to a remote offer's m-lines, so media put
 * on them would never reach the server; the offer's own transceivers are
 * switched to send instead, once, right after the offer is applied.
 */
export function bindPublishSenders(cx: Cx, conn: RTCPeerConnection, sdp: string, layerLines = 0) {
  const { st } = cx
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
  if (!st.audioSender) {
    st.audioSender = bind(mids.audio, activeAudioTrack(cx), SOURCE_MIC)
    if (st.audioSender) bound.fresh.audio = st.audioSender
  }
  if (!st.screenSender) {
    st.screenSender = bind(mids.video[0], null, SOURCE_SCREEN, 0)
    bound.screen = !!st.screenSender && !!st.localScreenStream.value
    if (st.screenSender) bound.fresh.screen = st.screenSender
  }
  if (!st.cameraSender) {
    st.cameraSender = bind(mids.video[1], st.localCameraStream.value?.getVideoTracks()[0], SOURCE_CAMERA)
    bound.camera = !!st.cameraSender && !!st.localCameraStream.value
    if (st.cameraSender) bound.fresh.camera = st.cameraSender
  }
  if (!st.screenAudioSender) {
    st.screenAudioSender = bind(mids.screenAudio, liveScreenAudioTrack(cx), SOURCE_SCREEN_AUDIO)
    if (st.screenAudioSender) bound.fresh.screenAudio = st.screenAudioSender
  }
  // The lower layers of the screen share: lines an older server does not offer.
  for (let i = 0; i < Math.min(st.layerSenders.length, layerLines); i++) {
    if (st.layerSenders[i]) continue
    const sender = bind(mids.video[2 + i], null, SOURCE_SCREEN, i + 1)
    st.layerSenders[i] = sender
    if (sender) (bound.fresh.layers ??= []).push(sender)
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

