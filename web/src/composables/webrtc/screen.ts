import { toRaw } from 'vue'
import { useToastStore } from '../../stores/toast'
import { trackConstraints, streamTuning } from '../../lib/streamQuality'
import { confirm } from '../../lib/confirm'
import { t } from '../../i18n'
import { platform } from '../../lib/platform'
import type { MediaStat } from '../../lib/mediaStats'
import type { StreamQuality } from '../../lib/streamQuality'
import { mediaUnprotected } from './mediaRoom'
import type { Cx } from './state'
import { retuneForCapture, stopCaptureWatch, syncLayers, watchCapture } from './layers'
import { ensureScreenAudioGuard, screenAudioConstraints, tuneSender } from './senderTuning'
import { screenSenderParams } from './senders'

// The screen's sound ended (or sharing stopped): its line goes silent.
async function detachScreenAudio(cx: Cx) {
  const { voice, st } = cx
  voice.hasScreenAudio = false
  if (st.screenAudioSender) await st.screenAudioSender.replaceTrack(null).catch(() => {})
}

// Asks the browser for a screen, captured in the quality chosen for the share.
function pickScreen(cx: Cx) {
  const { voice } = cx
  return navigator.mediaDevices.getDisplayMedia({
    video: trackConstraints(voice.screenQuality),
    audio: screenAudioConstraints()
  })
}

type CurrentCapture = () => boolean

function prepareScreenVideo(cx: Cx, stream: MediaStream, videoTrack: MediaStreamTrack) {
  const { voice, st } = cx
  if ('contentHint' in videoTrack) videoTrack.contentHint = streamTuning(voice.screenQuality).contentHint
  videoTrack.onended = () => { if (toRaw(st.localScreenStream.value) === stream) stopScreenShare(cx) }
}

function prepareScreenAudio(cx: Cx, stream: MediaStream, screenAudio: MediaStreamTrack, guard: unknown, currentCapture: CurrentCapture) {
  const { voice, st } = cx
  // The streamer's mute: a disabled track sends silence.
  screenAudio.enabled = !voice.isScreenAudioMuted
  screenAudio.onended = () => { if (toRaw(st.localScreenStream.value) === stream) detachScreenAudio(cx) }
  if (guard !== 'none' || st.warnedScreenAudioTracks.has(screenAudio)) return
  st.warnedScreenAudioTracks.add(screenAudio)
  useToastStore().info(t('talk.screenAudioFeedbackWarning'), {
    action: {
      label: t('talk.muteStreamAudio'),
      onClick: () => {
        if (!currentCapture()) return
        voice.isScreenAudioMuted = true
        screenAudio.enabled = false
      }
    }
  })
}

// Puts the picked screen on its negotiated line. Resolves to false when the
// share was left, stopped or replaced meanwhile.
async function sendScreenVideo(cx: Cx, stream: MediaStream, sender: RTCRtpSender, videoTrack: MediaStreamTrack) {
  const { chat, st } = cx
  const gen = st.joinGeneration
  const current = () => gen === st.joinGeneration && st.screenSender === sender && toRaw(st.localScreenStream.value) === stream
  // Set the chosen limits while the negotiated line is dormant. Attaching
  // first starts an encoder at the previous/default size, then immediately
  // asks it to reconfigure (particularly expensive for a 4K capture).
  const dormant = !sender.track
  if (dormant) await tuneSender(sender, screenSenderParams(cx, videoTrack))
  if (!current()) return false
  await sender.replaceTrack(videoTrack).catch(() => {})
  if (!current()) return false
  if (!dormant) await tuneSender(sender, screenSenderParams(cx, videoTrack))
  if (!current()) return false
  chat.sendWSEvent('webrtc_screenshare_start', {})
  chat.sendWSEvent('webrtc_request_keyframe', {})
  await syncLayers(cx)
  watchCapture(cx, videoTrack, () => void retuneForCapture(cx, videoTrack))
  return true
}

async function prepareAttachedTracks(
  cx: Cx,
  stream: MediaStream,
  currentCapture: CurrentCapture
): Promise<{ videoTrack?: MediaStreamTrack; screenAudio: MediaStreamTrack | null } | null> {
  const { voice, st } = cx
  const videoTrack = stream.getVideoTracks()[0]
  if (videoTrack) prepareScreenVideo(cx, stream, videoTrack)
  const screenAudio = stream.getAudioTracks()[0] || null
  const guard = screenAudio ? await ensureScreenAudioGuard(screenAudio) : null
  if (!currentCapture()) return null
  st.screenAudioGuard = guard
  if (screenAudio) prepareScreenAudio(cx, stream, screenAudio, guard, currentCapture)
  voice.hasScreenAudio = !!screenAudio
  return { videoTrack, screenAudio }
}

async function dispatchAttachedSenders(
  cx: Cx,
  stream: MediaStream,
  videoTrack?: MediaStreamTrack,
  screenAudio: MediaStreamTrack | null = null
) {
  const { st } = cx
  if (st.screenSender && videoTrack && !(await sendScreenVideo(cx, stream, st.screenSender, videoTrack))) return
  if (st.screenAudioSender) await st.screenAudioSender.replaceTrack(screenAudio).catch(() => {})
  if (!screenAudio) useToastStore().info(t('talk.noAudioInShareTip'))
}

// Sends a freshly picked screen (and its sound, on its own line).
export async function attachScreen(cx: Cx, stream: MediaStream) {
  const { st } = cx
  const gen = st.joinGeneration
  const captureGen = st.screenCaptureGeneration
  const currentCapture = () => gen === st.joinGeneration && captureGen === st.screenCaptureGeneration && toRaw(st.localScreenStream.value) === stream
  const tracks = await prepareAttachedTracks(cx, stream, currentCapture)
  if (!tracks) return
  await dispatchAttachedSenders(cx, stream, tracks.videoTrack, tracks.screenAudio)
}

function cannotStartShare(cx: Cx): boolean {
  const { st } = cx
  if (platform.isDesktop()) {
    useToastStore().error(t('nativeDesktop.mediaUnavailable'))
    return true
  }
  return mediaUnprotected() || st.screenCapturePending
}

async function confirmReplaceShare(cx: Cx): Promise<boolean> {
  const { st } = cx
  if (!st.localScreenStream.value) return false
  const ok = await confirm({
    title: t('talk.replaceShareTitle'),
    body: t('talk.replaceShareBody'),
    confirmLabel: t('talk.replaceShareConfirm'),
    cancelLabel: t('common.cancel'),
    danger: false
  })
  if (!ok || !st.localScreenStream.value) return false
  await replaceScreenShare(cx)
  return true
}

function isScreenCaptureStale(
  cx: Cx,
  gen: number,
  captureGen: number,
  channelId: string | null
): boolean {
  const { voice, st } = cx
  if (gen !== st.joinGeneration || captureGen !== st.screenCaptureGeneration) return true
  return voice.currentChannelId !== channelId || !!st.localScreenStream.value
}

export async function startScreenShare(cx: Cx, quality?: StreamQuality): Promise<void> {
  if (cannotStartShare(cx)) return
  if (cx.st.localScreenStream.value) {
    await confirmReplaceShare(cx)
    return
  }

  const { voice, st } = cx
  const gen = st.joinGeneration
  const captureGen = ++st.screenCaptureGeneration
  const channelId = voice.currentChannelId
  if (quality) voice.setScreenQuality(quality)
  else voice.resetScreenQuality()

  st.screenCapturePending = true
  let stream: MediaStream
  try {
    stream = await pickScreen(cx)
  } catch (err) {
    console.warn('Screen share canceled or failed:', err)
    return
  } finally {
    st.screenCapturePending = false
  }

  if (isScreenCaptureStale(cx, gen, captureGen, channelId)) {
    stream.getTracks().forEach(tr => tr.stop())
    return
  }
  st.localScreenStream.value = stream
  voice.localScreenStream = stream
  voice.isScreenSharing = true
  await attachScreen(cx, stream)
}

function isReplacementStale(cx: Cx, gen: number, captureGen: number, old: MediaStream | null): boolean {
  const { st } = cx
  if (gen !== st.joinGeneration || captureGen !== st.screenCaptureGeneration) return true
  return st.localScreenStream.value !== old
}

// Swaps the running share for a newly picked screen on the same lines: no
// renegotiation, the viewers keep watching, the quality stays as chosen.
export async function replaceScreenShare(cx: Cx): Promise<void> {
  const { voice, st } = cx
  if (st.screenCapturePending) return
  const old = st.localScreenStream.value
  if (!old) return startScreenShare(cx)
  const gen = st.joinGeneration
  const captureGen = ++st.screenCaptureGeneration
  st.screenCapturePending = true
  let stream: MediaStream
  try {
    stream = await pickScreen(cx)
  } catch (err) {
    console.warn('Screen share canceled or failed:', err)
    return
  } finally {
    st.screenCapturePending = false
  }
  // Left, stopped or replaced meanwhile.
  if (isReplacementStale(cx, gen, captureGen, old)) {
    stream.getTracks().forEach(tr => tr.stop())
    return
  }
  st.localScreenStream.value = stream
  voice.localScreenStream = stream
  await attachScreen(cx, stream)
  old.getTracks().forEach(tr => {
    tr.onended = null
    tr.stop()
  })
}

export function stopScreenShare(cx: Cx) {
  const { voice, chat, st } = cx
  stopCaptureWatch(cx)
  st.screenCaptureGeneration++
  const wasSharing = !!st.localScreenStream.value
  if (st.screenSender) st.screenSender.replaceTrack(null).catch(() => {})
  detachScreenAudio(cx)
  // The next share starts again with its best layer only.
  voice.layersWanted = null
  st.declaredKey = ''

  if (st.localScreenStream.value) {
    st.localScreenStream.value.getTracks().forEach(tr => tr.stop())
    st.localScreenStream.value = null
  }
  voice.localScreenStream = null
  voice.isScreenSharing = false
  voice.hasScreenAudio = false
  voice.isScreenAudioMuted = false
  voice.resetScreenQuality()
  if (wasSharing) chat.sendWSEvent('webrtc_screenshare_stop', {})
  void syncLayers(cx)
}

// What the screen sender sends right now (RTCRtpSender.getStats), for the
// streamer's "Advanced" info; null without a share.
export async function getScreenSendStats(cx: Cx): Promise<MediaStat[] | null> {
  const { st } = cx
  if (!st.screenSender?.getStats || !st.localScreenStream.value) return null
  try {
    return [...(await st.screenSender.getStats()).values()]
  } catch {
    return null
  }
}

// A track setting is the capture's configured rate, not measured frame
// delivery. The advanced menu displays media-source FPS separately.
export function getScreenCaptureSettings(cx: Cx): MediaTrackSettings | null {
  const { st } = cx
  const settings = st.localScreenStream.value?.getVideoTracks()[0]?.getSettings?.()
  return settings ? {
    ...(settings.width === undefined ? {} : { width: settings.width }),
    ...(settings.height === undefined ? {} : { height: settings.height }),
    ...(settings.frameRate === undefined ? {} : { frameRate: settings.frameRate })
  } : null
}

export function toggleScreenShare(cx: Cx) {
  const { voice } = cx
  return voice.isScreenSharing ? stopScreenShare(cx) : startScreenShare(cx)
}

