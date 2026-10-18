import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { platform } from '../../lib/platform'
import { mediaUnprotected } from './mediaRoom'
import type { Cx } from './state'
import { tuneSender } from './senderTuning'
import { cameraSenderParams } from './senders'

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

export async function startCamera(cx: Cx) {
  const { voice, chat, st } = cx
  if (platform.isDesktop()) {
    useToastStore().error(t('nativeDesktop.mediaUnavailable'))
    return
  }
  if (mediaUnprotected()) return
  if (!voice.currentChannelId || st.localCameraStream.value) return
  const gen = st.joinGeneration
  const captureGen = st.cameraCaptureGeneration
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
  if (gen !== st.joinGeneration || captureGen !== st.cameraCaptureGeneration || st.localCameraStream.value) {
    stream.getTracks().forEach(tr => tr.stop())
    return
  }
  st.localCameraStream.value = stream
  voice.localCameraStream = stream
  voice.isCameraOn = true

  const track = stream.getVideoTracks()[0]
  if (track) {
    if ('contentHint' in track) track.contentHint = 'motion'
    track.onended = () => stopCamera(cx)
    if (st.cameraSender) {
      await st.cameraSender.replaceTrack(track).catch(() => {})
      await tuneSender(st.cameraSender, cameraSenderParams(cx))
      chat.sendWSEvent('webrtc_request_keyframe', {})
    }
  }
}

export function stopCamera(cx: Cx) {
  const { voice, chat, st } = cx
  st.cameraCaptureGeneration++
  const wasOn = !!st.localCameraStream.value
  if (st.cameraSender) st.cameraSender.replaceTrack(null).catch(() => {})
  if (st.localCameraStream.value) {
    st.localCameraStream.value.getTracks().forEach(tr => tr.stop())
    st.localCameraStream.value = null
  }
  voice.localCameraStream = null
  voice.isCameraOn = false
  if (wasOn) chat.sendWSEvent('webrtc_camera_stop', {})
}

export function toggleCamera(cx: Cx) {
  const { voice } = cx
  return voice.isCameraOn ? stopCamera(cx) : startCamera(cx)
}

