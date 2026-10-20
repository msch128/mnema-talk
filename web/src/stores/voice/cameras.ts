import { writeJson } from '../../lib/voicePlayback'
import { watch, computed } from 'vue'
import type { SubscribeRequest } from '../../types/events'
import { HIDDEN_CAMERAS_KEY, ALL_CAMERAS_OFF_KEY, HIDE_NO_VIDEO_KEY, myUserId } from './state'
import type { VoiceState } from './state'

// A camera on the stage: mine, or one I receive and have not hidden.
export function cameraStreamOf(cs: VoiceState, userId: string | null) {
  if (!userId) return null
  if (userId === myUserId()) return cs.localCameraStream.value || null
  return isCameraHidden(cs, userId) ? null : cs.userVideoStreams.value[userId] || null
}

export function canFocusCamera(cs: VoiceState, userId: string | null) {
  return !!cameraStreamOf(cs, userId)
}

export function focusCamera(cs: VoiceState, userId: string | null) {
  if (canFocusCamera(cs, userId)) cs.focusedCamera.value = userId
}

// Back to the screen share it replaced, or to everyone (the grid).
export function unfocusCamera(cs: VoiceState) {
  cs.focusedCamera.value = null
}

export function toggleCameraFocus(cs: VoiceState, userId: string | null) {
  if (cs.focusedCamera.value === userId) unfocusCamera(cs)
  else focusCamera(cs, userId)
}

export function setUserVideoStream(cs: VoiceState, userId: string, stream: MediaStream) {
  if (isCameraHidden(cs, userId)) return
  cs.userVideoStreams.value = { ...cs.userVideoStreams.value, [userId]: stream }
}

export function removeUserVideoStream(cs: VoiceState, userId: string, stream?: MediaStream) {
  const current = cs.userVideoStreams.value[userId]
  // A stale track ending must not drop a newer stream of the same user.
  if (!current || (stream && current !== stream)) return
  const next = { ...cs.userVideoStreams.value }
  delete next[userId]
  cs.userVideoStreams.value = next
}

export function setSubscriptionSink(cs: VoiceState, fn: ((request: SubscribeRequest) => void) | null) {
  cs.subscriptionSink = typeof fn === 'function' ? fn : null
}

export function emitSubscription(cs: VoiceState, msg: SubscribeRequest) {
  cs.subscriptionSink?.(msg)
}

export function isCameraHidden(cs: VoiceState, userId: string | null) {
  return cs.allCamerasOff.value || (!!userId && !!cs.hiddenCameras.value[userId])
}

export function setCameraHidden(cs: VoiceState, userId: string | null, hidden: boolean) {
  if (!userId) return
  const next = { ...cs.hiddenCameras.value }
  if (hidden) next[userId] = true
  else delete next[userId]
  cs.hiddenCameras.value = next
  writeJson(HIDDEN_CAMERAS_KEY, next)
  if (hidden) removeUserVideoStream(cs, userId)
  emitSubscription(cs, { kind: 'camera', user_id: userId, on: !hidden })
}

export function toggleCameraHidden(cs: VoiceState, userId: string | null) {
  if (userId) setCameraHidden(cs, userId, !cs.hiddenCameras.value[userId])
}

export function setAllCamerasOff(cs: VoiceState, off: boolean) {
  cs.allCamerasOff.value = !!off
  writeJson(ALL_CAMERAS_OFF_KEY, { off: !!off })
  if (off) cs.userVideoStreams.value = {}
  emitSubscription(cs, { kind: 'camera', all: true, on: !off })
}

export function setHideNoVideo(cs: VoiceState, on: boolean) {
  cs.hideNoVideo.value = !!on
  writeJson(HIDE_NO_VIDEO_KEY, { on: !!on })
}

export function toggleHideNoVideo(cs: VoiceState) {
  setHideNoVideo(cs, !cs.hideNoVideo.value)
}

// Sends every stored choice; right after joining and after a reconnect.
export function resendSubscriptions(cs: VoiceState) {
  if (cs.allCamerasOff.value) emitSubscription(cs, { kind: 'camera', all: true, on: false })
  for (const id of Object.keys(cs.hiddenCameras.value)) {
    emitSubscription(cs, { kind: 'camera', user_id: id, on: false })
  }
  for (const id of Object.keys(cs.watchedScreens.value)) {
    emitSubscription(cs, screenRequest(cs, id, true))
  }
}

// A subscription to a screen share carries the layer I chose, if I did.
export function screenRequest(cs: VoiceState, userId: string, on: boolean): SubscribeRequest {
  const choice = cs.screenChoice.value[userId]
  return choice === undefined ? { kind: 'screen', user_id: userId, on } : { kind: 'screen', user_id: userId, on, quality: choice }
}

function resolveScreenStage(cs: VoiceState) {
  const own = cs.localScreenStream.value
  const remoteId = cs.remoteScreenUserId.value
  const remote = remoteId ? cs.remoteScreenStream.value : null
  const prefersOwn = own && (cs.ownScreenFocused.value || !remote)
  if (prefersOwn) {
    return { kind: 'screen' as const, own: true, userId: myUserId(), stream: own }
  }
  if (remote) {
    return { kind: 'screen' as const, own: false, userId: remoteId, stream: remote }
  }
  return null
}

function resolveStageState(cs: VoiceState) {
  const cameraId = cs.focusedCamera.value
  const camera = cameraStreamOf(cs, cameraId)
  if (camera) {
    return { kind: 'camera' as const, own: cameraId === myUserId(), userId: cameraId, stream: camera }
  }
  return resolveScreenStage(cs)
}

export function installStage(cs: VoiceState) {
  // --- The stage: one screen share (mine or one I watch) or one camera ---
  // Starting to share puts my own share on the stage unless I already watch
  // someone; then theirs stays and mine is a card.
  watch(cs.localScreenStream, (stream, prev) => {
    if (!stream) {
      cs.ownScreenFocused.value = false
    } else if (!prev) {
      cs.ownScreenFocused.value = !cs.remoteScreenUserId.value
    }
  }, { flush: 'sync' })

  // The camera on the stage was turned off, hidden or left: it leaves the stage.
  watch(() => Boolean(cs.focusedCamera.value && !canFocusCamera(cs, cs.focusedCamera.value)), gone => {
    if (gone) cs.focusedCamera.value = null
  }, { flush: 'sync' })

  return computed(() => resolveStageState(cs))
}

