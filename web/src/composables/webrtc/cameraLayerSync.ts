import { toRaw } from 'vue'
import type { planCameraLayers } from '../../lib/cameraLayers'
import type { Cx } from './state'
import { isLayerActive } from './layers'
import { tuneSender } from './senderTuning'
import { cameraSenderParams, qosPriority } from './senders'
import { cameraPlansOf, replaceQuietly } from './cameraLayers'

// Feeding and switching the camera's lower layers (see cameraLayers).

async function feedLowerLayer(cx: Cx, sender: RTCRtpSender, plan: ReturnType<typeof planCameraLayers>[number], active: boolean, track: MediaStreamTrack, gen: number) {
  const { st } = cx
  const prio = qosPriority(cx)
  await tuneSender(sender, {
    active, maxBitrate: plan.maxBitrate, maxFramerate: plan.maxFramerate, scaleResolutionDownBy: plan.scale,
    degradationPreference: 'balanced', priority: prio, networkPriority: prio
  })
  if (gen !== st.joinGeneration || toRaw(st.localCameraStream.value)?.getVideoTracks()[0] !== track) return
  if (sender.track !== track) await replaceQuietly(sender, track)
}

async function runSyncCameraLayers(cx: Cx) {
  const { voice, st } = cx
  const track = st.localCameraStream.value?.getVideoTracks()[0]
  const gen = st.joinGeneration
  const senders = [...st.cameraLayerSenders]
  if (!senders.some(Boolean)) return
  const plans = track ? cameraPlansOf(track, true) : []
  const wanted = voice.cameraLayersWanted
  if (track && st.cameraSender) {
    await tuneSender(st.cameraSender, { ...cameraSenderParams(cx), ...(plans.length ? { active: isLayerActive(wanted, 0) } : { active: true }) })
  }
  for (let i = 0; i < senders.length; i++) {
    const sender = senders[i]
    if (!sender) continue
    const plan = plans[i + 1]
    if (!track || !plan) {
      if (sender.track) await replaceQuietly(sender, null)
      continue
    }
    await feedLowerLayer(cx, sender, plan, isLayerActive(wanted, i + 1), track, gen)
  }
}

/** A failure here must not stop the quality changes queued behind it. */
function reportSyncFailure(err: unknown) {
  console.debug('[WebRTC] Could not apply the camera layers:', err)
}

/**
 * Applies the layers the server says viewers need to the camera's lines, and
 * feeds the lower ones the capture. Runs one after another with quality changes.
 */
export function syncCameraLayers(cx: Cx) {
  const { st } = cx
  st.qualityChain = st.qualityChain.then(() => runSyncCameraLayers(cx)).catch(reportSyncFailure)
  return st.qualityChain
}

