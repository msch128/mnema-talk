import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { mediaUnprotected } from './mediaRoom'
import type { Cx } from './state'
import { tuneSender } from './senderTuning'
import { cameraSenderParams } from './senders'
import { declareCameraLayers, releaseCameraLayers } from './cameraLayers'
import { syncCameraLayers } from './cameraLayerSync'

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

function canStartCamera(cx: Cx): boolean {
  if (mediaUnprotected()) return false
  return Boolean(cx.voice.currentChannelId && !cx.st.localCameraStream.value)
}

function isCameraCaptureOutdated(st: Cx['st'], gen: number, captureGen: number): boolean {
  return gen !== st.joinGeneration || captureGen !== st.cameraCaptureGeneration || Boolean(st.localCameraStream.value)
}

async function attachCameraTrack(cx: Cx, track: MediaStreamTrack) {
  if ('contentHint' in track) track.contentHint = 'motion'
  track.onended = () => stopCamera(cx)
  if (cx.st.cameraSender) {
    // The server hears of the layers before the first video: that is when it decides to take them.
    declareCameraLayers(cx, track, cx.st.cameraLayerSenders.some(Boolean))
    await cx.st.cameraSender.replaceTrack(track).catch(() => {})
    await tuneSender(cx.st.cameraSender, cameraSenderParams(cx))
    await syncCameraLayers(cx)
    // The server asks the sender's own camera; a plain request is for the videos it watches.
    cx.chat.sendWSEvent('webrtc_request_keyframe', { source: 'camera' })
  }
}

export async function startCamera(cx: Cx) {
  if (!canStartCamera(cx)) return
  const { voice, st } = cx
  const gen = st.joinGeneration
  const captureGen = st.cameraCaptureGeneration
  let stream: MediaStream
  try {
    stream = await navigator.mediaDevices.getUserMedia({
      video: {
        // Preferred, not required: an unplugged camera must not keep the camera from starting.
        ...(voice.selectedCameraDeviceId ? { deviceId: { ideal: voice.selectedCameraDeviceId } } : {}),
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
  if (isCameraCaptureOutdated(st, gen, captureGen)) {
    stream.getTracks().forEach(tr => tr.stop())
    return
  }
  st.localCameraStream.value = stream
  voice.localCameraStream = stream
  voice.isCameraOn = true

  const track = stream.getVideoTracks()[0]
  if (track) {
    await attachCameraTrack(cx, track)
  }
}

export function stopCamera(cx: Cx) {
  const { voice, chat, st } = cx
  st.cameraCaptureGeneration++
  const wasOn = !!st.localCameraStream.value
  if (st.cameraSender) st.cameraSender.replaceTrack(null).catch(() => {})
  releaseCameraLayers(cx)
  if (st.localCameraStream.value) {
    st.localCameraStream.value.getTracks().forEach(tr => tr.stop())
    st.localCameraStream.value = null
  }
  voice.localCameraStream = null
  voice.isCameraOn = false
  if (wasOn) chat.sendWSEvent('webrtc_camera_stop', {})
}

/** Applies a changed camera choice to the running camera. */
export async function restartCamera(cx: Cx) {
  if (!cx.voice.isCameraOn) return
  stopCamera(cx)
  await startCamera(cx)
}

export function toggleCamera(cx: Cx) {
  const { voice } = cx
  return voice.isCameraOn ? stopCamera(cx) : startCamera(cx)
}

