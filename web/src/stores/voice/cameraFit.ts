// How big my picture of each person's camera is, in device pixels, so the
// server sends the smallest layer of their camera that has enough (a small
// tile gets the thumbnail, the stage the best). A camera is often shown more
// than once (a tile and the stage): the largest picture counts. The report
// settles for a moment before it goes out, so resizing a window is one message.
import type { VoiceState } from './state'
import { emitSubscription } from './cameras'

/** A size change must hold this long before it is sent. */
export const CAMERA_FIT_SETTLE_MS = 400

interface FitMemory {
  timers: Map<string, ReturnType<typeof setTimeout>>
  /** What the server was last told per person. */
  sent: Map<string, number>
}

const memories = new WeakMap<VoiceState, FitMemory>()

function memoryOf(cs: VoiceState): FitMemory {
  let memory = memories.get(cs)
  if (!memory) {
    memory = { timers: new Map(), sent: new Map() }
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
  const px = wantedPixels(cs, userId)
  if (px === (memory.sent.get(userId) ?? 0)) return
  if (px > 0) memory.sent.set(userId, px)
  else memory.sent.delete(userId)
  emitSubscription(cs, { kind: 'camera', user_id: userId, on: true, fit_px: px })
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
  memory.timers.set(userId, setTimeout(() => send(cs, userId), first ? 0 : CAMERA_FIT_SETTLE_MS))
}

/** After joining and after a reconnect the server knows nothing of what was said: say it all again. */
export function resendCameraFit(cs: VoiceState) {
  const memory = memoryOf(cs)
  memory.sent.clear()
  for (const timer of memory.timers.values()) clearTimeout(timer)
  memory.timers.clear()
  for (const id of Object.keys(cs.cameraSizes.value)) send(cs, id)
}

/** Leaving the call: nothing is shown, nothing was said. */
export function resetCameraFit(cs: VoiceState) {
  const memory = memoryOf(cs)
  for (const timer of memory.timers.values()) clearTimeout(timer)
  memory.timers.clear()
  memory.sent.clear()
  cs.cameraSizes.value = {}
}
