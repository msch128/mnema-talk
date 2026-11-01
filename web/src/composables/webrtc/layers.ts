import { toRaw } from 'vue'
import { streamTuning } from '../../lib/streamQuality'
import { declaredLayers, layersKey, planScreenLayers } from '../../lib/screenLayers'
import type { Cx } from './state'
import { tuneSender } from './senderTuning'
import { qosPriority, screenSenderParams } from './senders'

// The size and rate a capture delivers change under a running share (a resized
// window, a new display mode), and a track fires no event for it, so they are
// read regularly. A change that has held for CAPTURE_SETTLE_MS counts, so
// dragging a window edge is one change, not hundreds.
export const CAPTURE_POLL_MS = 500
export const CAPTURE_SETTLE_MS = 1000

function captureKey(track: MediaStreamTrack): string {
  const settings = track.getSettings?.()
  return `${settings?.width}x${settings?.height}@${Math.round(settings?.frameRate ?? 0)}`
}

/** Calls onChange once the capture's width, height or frame rate has changed and stayed so. */
export function watchCapture(cx: Cx, track: MediaStreamTrack, onChange: () => void) {
  const { st } = cx
  stopCaptureWatch(cx)
  let seen = captureKey(track)
  st.captureWatch = setInterval(() => {
    const key = captureKey(track)
    if (key === seen) return
    seen = key
    if (st.captureSettle) clearTimeout(st.captureSettle)
    st.captureSettle = setTimeout(() => {
      st.captureSettle = null
      onChange()
    }, CAPTURE_SETTLE_MS)
  }, CAPTURE_POLL_MS)
}

export function stopCaptureWatch(cx: Cx) {
  const { st } = cx
  if (st.captureWatch) clearInterval(st.captureWatch)
  if (st.captureSettle) clearTimeout(st.captureSettle)
  st.captureWatch = null
  st.captureSettle = null
}

// The lower layers of my screen share: sending the captured track smaller,
// only the layers viewers need (until the server says, only the best), and
// declaring them to the room. Runs one after another with quality changes.
export function syncLayers(cx: Cx) {
  const { st } = cx
  st.qualityChain = st.qualityChain.then(() => runSyncLayers(cx)).catch(() => {})
  return st.qualityChain
}

const dropTrack = async (sender: RTCRtpSender) => {
  if (sender.track) await sender.replaceTrack(null).catch(() => {})
}

/** No share (or no best-layer sender): the lower layers go quiet and the next share starts on its best layer alone. */
async function releaseLayers(cx: Cx, senders: (RTCRtpSender | null | undefined)[]) {
  const { st } = cx
  for (const sender of senders) if (sender) await dropTrack(sender)
  if (st.screenSender) await tuneSender(st.screenSender, { active: true })
  st.declaredKey = ''
}

type LayerPlan = ReturnType<typeof planScreenLayers>[number]

/** Tunes one lower layer's sender and feeds it the capture. Resolves to false when the share changed meanwhile. */
async function feedLayer(cx: Cx, sender: RTCRtpSender, plan: LayerPlan, active: boolean, track: MediaStreamTrack, gen: number) {
  const { voice, st } = cx
  const prio = qosPriority(cx)
  const { degradationPreference } = streamTuning(voice.screenQuality)
  await tuneSender(sender, {
    active, maxBitrate: plan.maxBitrate, maxFramerate: plan.maxFramerate, scaleResolutionDownBy: plan.scale,
    degradationPreference, priority: prio, networkPriority: prio
  })
  if (gen !== st.joinGeneration || toRaw(st.localScreenStream.value)?.getVideoTracks()[0] !== track) return false
  if (sender.track !== track) await sender.replaceTrack(track).catch(() => {})
  return true
}

/** Tells the room which layers exist, when they differ from what it was told last. */
function declareLayers(cx: Cx, plans: LayerPlan[]) {
  const { chat, st } = cx
  const declared = declaredLayers(plans)
  const key = layersKey(declared)
  if (key === st.declaredKey) return
  st.declaredKey = key
  chat.sendWSEvent('webrtc_layers', { layers: declared })
}

export function isLayerActive(wanted: number[] | null, layer: number): boolean {
  return wanted === null ? layer === 0 : wanted.includes(layer)
}

async function syncSenderLayers(
  cx: Cx,
  senders: (RTCRtpSender | null | undefined)[],
  plans: LayerPlan[],
  track: MediaStreamTrack,
  gen: number,
  wanted: number[] | null
): Promise<boolean> {
  for (let i = 0; i < senders.length; i++) {
    const sender = senders[i]
    if (!sender) continue
    const plan = plans[i + 1]
    if (!plan) {
      await dropTrack(sender)
      continue
    }
    if (!(await feedLayer(cx, sender, plan, isLayerActive(wanted, i + 1), track, gen))) {
      return false
    }
  }
  return true
}

async function runSyncLayers(cx: Cx) {
  const { voice, st } = cx
  if (!st.layerSenders.some(Boolean)) return
  const track = st.localScreenStream.value?.getVideoTracks()[0]
  const gen = st.joinGeneration
  const senders = [...st.layerSenders]
  if (!track || !st.screenSender) {
    await releaseLayers(cx, senders)
    return
  }
  const plans = planScreenLayers(voice.screenQuality, track.getSettings?.() ?? {})
  const wanted = voice.layersWanted
  await tuneSender(st.screenSender, { active: isLayerActive(wanted, 0) })
  const fed = await syncSenderLayers(cx, senders, plans, track, gen, wanted)
  if (!fed || gen !== st.joinGeneration || !plans.length) return
  declareLayers(cx, plans)
}

// The capture changed size or rate: the best layer is sized for the new
// picture, the lower ones are re-planned and the layers declared again (the
// server and the viewers' pickers only hear of it if the sizes differ).
export async function retuneForCapture(cx: Cx, track: MediaStreamTrack) {
  const { st } = cx
  st.qualityChain = st.qualityChain.then(async () => {
    if (st.screenSender) await tuneSender(st.screenSender, screenSenderParams(cx, track))
  }).catch(() => {})
  await syncLayers(cx)
}

