// How big my picture of each person's camera is, in device pixels, so the
// server sends the smallest layer of their camera that has enough (a small
// tile gets the thumbnail, the stage the best). A camera is often shown more
// than once (a tile and the stage): the largest picture counts. The report
// settles for a moment before it goes out, so resizing a window is one message.
import type { VoiceState } from './state'
import { emitSubscription } from './cameras'

/** A size change must hold this long before it is sent. */
export const CAMERA_FIT_SETTLE_MS = 400
/**
 * A picture that is gone is only reported after this long: a tile that moves
 * (the grid to the strip under a stage) is removed and shown again within a
 * moment, and telling the server "no picture" in between would wake the best
 * layer for nothing.
 */
export const CAMERA_FIT_GONE_MS = 2000
/** What is shown is said again this often, so a report that was lost on the way heals. */
export const CAMERA_FIT_REASSERT_MS = 10_000

interface FitMemory {
  timers: Map<string, ReturnType<typeof setTimeout>>
  /** What the server was last told per person. */
  sent: Map<string, number>
  beat: ReturnType<typeof setInterval> | null
}

const memories = new WeakMap<VoiceState, FitMemory>()

function memoryOf(cs: VoiceState): FitMemory {
  let memory = memories.get(cs)
  if (!memory) {
    memory = { timers: new Map(), sent: new Map(), beat: null }
    memories.set(cs, memory)
  }
  return memory
}

/** The largest picture of userId's camera on screen now, 0 when there is none. */
function wantedPixels(cs: VoiceState, userId: string): number {
  return Math.max(0, ...Object.values(cs.cameraSizes.value[userId] ?? {}))
}

function send(cs: VoiceState, userId: string) {
  const memory = memoryOf(cs)
  memory.timers.delete(userId)
  // A server that does not take camera layers has nothing to do with a size, and would read it as an ordinary subscription.
  if (cs.serverCameraLayers.value <= 0) return
  // Nothing to send it with (not in a call yet): it is not said, so it is said once there is.
  if (!cs.subscriptionSink) return
  const px = wantedPixels(cs, userId)
  if (px === (memory.sent.get(userId) ?? 0)) return
  if (px > 0) memory.sent.set(userId, px)
  else memory.sent.delete(userId)
  emitSubscription(cs, { kind: 'camera', user_id: userId, on: true, fit_px: px })
}

/** Says again what is shown of userId's camera, whether or not it changed. */
function say(cs: VoiceState, userId: string) {
  const px = wantedPixels(cs, userId)
  if (px <= 0 || cs.serverCameraLayers.value <= 0 || !cs.subscriptionSink) return
  memoryOf(cs).sent.set(userId, px)
  emitSubscription(cs, { kind: 'camera', user_id: userId, on: true, fit_px: px })
}

/** The heart beat runs while any camera is shown and stops with the last one. */
function keepBeating(cs: VoiceState) {
  const memory = memoryOf(cs)
  const shown = Object.keys(cs.cameraSizes.value).length > 0
  if (shown && !memory.beat) {
    memory.beat = setInterval(() => { for (const id of Object.keys(cs.cameraSizes.value)) say(cs, id) }, CAMERA_FIT_REASSERT_MS)
  } else if (!shown && memory.beat) {
    clearInterval(memory.beat)
    memory.beat = null
  }
}

/**
 * userId's camera just arrived (the server made its track for me, so the
 * size I told it before the track existed may have been lost, or not heard):
 * say it again now that it can act on it.
 */
export function replayCameraFit(cs: VoiceState, userId: string) {
  say(cs, userId)
}

/**
 * One picture of userId's camera (`key` names where it is shown) is `pixels`
 * tall, 0 when it is gone. The first report of a person goes out at once.
 */
export function setCameraSize(cs: VoiceState, userId: string | null, key: string, pixels: number) {
  if (!userId) return
  const sizes = { ...cs.cameraSizes.value[userId] }
  if (pixels > 0) sizes[key] = Math.round(pixels)
  else delete sizes[key]
  const all = { ...cs.cameraSizes.value }
  if (Object.keys(sizes).length) all[userId] = sizes
  else delete all[userId]
  cs.cameraSizes.value = all

  const memory = memoryOf(cs)
  const pending = memory.timers.get(userId)
  if (pending) clearTimeout(pending)
  const first = !memory.sent.has(userId)
  const gone = wantedPixels(cs, userId) === 0
  memory.timers.set(userId, setTimeout(() => send(cs, userId), first ? 0 : gone ? CAMERA_FIT_GONE_MS : CAMERA_FIT_SETTLE_MS))
  keepBeating(cs)
}

/** After joining and after a reconnect the server knows nothing of what was said: say it all again. */
export function resendCameraFit(cs: VoiceState) {
  const memory = memoryOf(cs)
  memory.sent.clear()
  for (const timer of memory.timers.values()) clearTimeout(timer)
  memory.timers.clear()
  for (const id of Object.keys(cs.cameraSizes.value)) send(cs, id)
  keepBeating(cs)
}

/** Leaving the call: nothing is shown, nothing was said. */
export function resetCameraFit(cs: VoiceState) {
  const memory = memoryOf(cs)
  for (const timer of memory.timers.values()) clearTimeout(timer)
  memory.timers.clear()
  memory.sent.clear()
  cs.cameraSizes.value = {}
  keepBeating(cs)
}
