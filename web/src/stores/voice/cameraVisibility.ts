import { watch } from 'vue'
import type { SubscribeRequest, CamerasCapped } from '../../types/events'
import type { VoiceState } from './state'
import { myUserId } from './state'
import { emitSubscription, removeUserVideoStream, isCameraHidden, isCameraOffscreen } from './cameras'

// Only the cameras that can be seen are received. The Talk view reports which
// tiles are on screen (see useVisibleCameras); the cameras of everyone else in
// the call are switched off at the server and asked for again when their tile
// comes back. This is on top of what the member chose: a camera they hid stays
// hidden whatever is on screen (see setCameraHidden).

/** Cameras named in one webrtc_subscribe; the server takes at most this many. */
export const CAMERA_BATCH = 16
/** A tile that came into view waits this long (changes in a burst go out as one). */
const SHOW_DELAY_MS = 120
/** A tile that left view waits longer, so a scroll back and forth costs nothing. */
const HIDE_DELAY_MS = 800

// The pending flush of each state and when it is due.
const timers = new WeakMap<VoiceState, { timer: ReturnType<typeof setTimeout>; due: number }>()

// Who should not be sent a camera: everyone with a camera on whose tile is not
// on screen and who is not on the stage. Nobody while the view does not report.
function offscreenNow(cs: VoiceState): Record<string, true> {
  const visible = cs.visibleCameras.value
  const out: Record<string, true> = {}
  if (!visible) return out
  const me = myUserId()
  for (const [id, media] of Object.entries(cs.mediaState.value)) {
    if (id === me || !media.camera || visible[id] || cs.focusedCamera.value === id) continue
    out[id] = true
  }
  return out
}

function requests(ids: string[], on: boolean): SubscribeRequest[] {
  const out: SubscribeRequest[] = []
  for (let i = 0; i < ids.length; i += CAMERA_BATCH) {
    const chunk = ids.slice(i, i + CAMERA_BATCH)
    const [only] = chunk
    out.push(chunk.length === 1 && only ? { kind: 'camera', user_id: only, on } : { kind: 'camera', user_ids: chunk, on })
  }
  return out
}

/** Tells the server about the cameras that went off or came back since the last time; one event per 16 cameras. */
export function flushCameraVisibility(cs: VoiceState) {
  const want = offscreenNow(cs)
  const sent = cs.offscreenCameras.value
  const off = Object.keys(want).filter(id => !sent[id])
  const on = Object.keys(sent).filter(id => !want[id])
  if (!off.length && !on.length) return
  cs.offscreenCameras.value = want
  // A camera the member hid is hidden at the server already, and stays so.
  for (const request of requests(off.filter(id => !cs.hiddenCameras.value[id]), false)) emitSubscription(cs, request)
  for (const request of requests(on.filter(id => !cs.hiddenCameras.value[id]), true)) emitSubscription(cs, request)
  for (const id of off) removeUserVideoStream(cs, id)
}

// A flush is due after the wait that fits what it does: soon when a camera is to
// come back (or right after a join), later when cameras only go off. A change
// never pushes a flush that is pending already further out, so a view that keeps
// changing cannot hold the cameras off its screen back for ever.
function schedule(cs: VoiceState, soon = false) {
  const want = offscreenNow(cs)
  const bringsBack = Object.keys(cs.offscreenCameras.value).some(id => !want[id])
  const due = Date.now() + (soon || bringsBack ? SHOW_DELAY_MS : HIDE_DELAY_MS)
  const pending = timers.get(cs)
  if (pending && pending.due <= due) return
  if (pending) clearTimeout(pending.timer)
  const timer = setTimeout(() => {
    timers.delete(cs)
    flushCameraVisibility(cs)
  }, due - Date.now())
  timers.set(cs, { timer, due })
}

/**
 * The cameras whose tiles are on screen, or null while the Talk view does not
 * report (every camera is received then). Call again whenever the set changes;
 * changes are collected and sent together.
 */
export function setVisibleCameras(cs: VoiceState, ids: string[] | null) {
  cs.visibleCameras.value = ids === null ? null : Object.fromEntries(ids.map(id => [id, true as const]))
  schedule(cs)
}

/** A camera appearing or disappearing, or the stage changing hands, moves who is on screen. */
export function installCameraVisibility(cs: VoiceState) {
  watch([cs.mediaState, cs.focusedCamera], () => { if (cs.visibleCameras.value) schedule(cs) }, { deep: true })
}

/** The server was just joined or rejoined and knows no choices: everything is sent again. */
export function resetCameraVisibility(cs: VoiceState) {
  cs.offscreenCameras.value = {}
  cs.cappedCameras.value = {}
  // Soon: until it is sent, the server sends everything, the very spike this avoids.
  if (cs.visibleCameras.value) schedule(cs, true)
}

/** cameras_capped: whose cameras the server does not forward because I asked for too many. */
export function handleCamerasCapped(cs: VoiceState, event: CamerasCapped) {
  // Late events of a room I left are not mine.
  if (!event || !Array.isArray(event.user_ids) || !cs.currentChannelId.value || (event.channel_id && event.channel_id !== cs.currentChannelId.value)) return
  cs.cappedCameras.value = Object.fromEntries(event.user_ids.map(id => [id, true as const]))
  for (const id of event.user_ids) removeUserVideoStream(cs, id)
}

/** The camera's tile shows no picture because of the server's limit, not because I hid it. */
export function isCameraCapped(cs: VoiceState, userId: string | null) {
  return !!userId && !!cs.cappedCameras.value[userId] && !isCameraHidden(cs, userId) && !isCameraOffscreen(cs, userId)
}
