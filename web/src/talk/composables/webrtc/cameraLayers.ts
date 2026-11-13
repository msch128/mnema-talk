import { cameraLayersKey, declaredCameraLayers, planCameraLayers } from '../../lib/cameraLayers'
import type { Cx } from './state'

// The camera's lower layers (see talk/lib/cameraLayers): sending the captured track
// smaller on lines of its own, only the layers viewers need (until the server
// says, only the best), and declaring them to the server before the first
// video. A camera whose browser has no such lines, or whose server does not
// take layers, or that is too small for a second one, is sent as one layer
// exactly as before, and declares nothing.

/** The layers this camera track gets; fewer than two means it is sent as before. */
export function cameraPlansOf(track: MediaStreamTrack, available: boolean) {
  if (!available) return []
  const plans = planCameraLayers(track.getSettings?.())
  return plans.length > 1 ? plans : []
}

/**
 * Tells the server the layers the camera will send. It must come before the
 * camera's first video, which is when the server decides to take layers.
 * Returns how many layers there are (0: a single plain camera).
 */
export function declareCameraLayers(cx: Cx, track: MediaStreamTrack, available: boolean): number {
  const { chat, st } = cx
  const plans = cameraPlansOf(track, available)
  if (!plans.length) return 0
  const declared = declaredCameraLayers(plans)
  const key = cameraLayersKey(declared)
  if (key !== st.declaredCameraKey) {
    st.declaredCameraKey = key
    chat.sendWSEvent('webrtc_layers', { layers: declared, source: 'camera' })
  }
  return plans.length
}

/** Puts a track on a line, or takes it off; a line that cannot is left as it is (the camera is then sent without that layer). */
export async function replaceQuietly(sender: RTCRtpSender, track: MediaStreamTrack | null) {
  try {
    await sender.replaceTrack(track)
  } catch (err) {
    console.debug('[WebRTC] Could not change a camera layer line:', err)
  }
}

/**
 * The track lower layer `index` encodes: a clone of the capture, its own per
 * layer. The browser adapts a track to the most constrained sender it feeds,
 * so with one track on every line a best layer cut down for bandwidth shrank
 * every layer with it (a 180-line thumbnail went out at 90 lines, and only
 * slowly grew back). A clone is made once per capture and line, and stopped
 * when the capture or the line no longer needs it: a clone keeps the camera on.
 */
export function layerClone(st: Cx['st'], index: number, source: MediaStreamTrack): MediaStreamTrack {
  const held = st.cameraLayerClones[index]
  if (held && held.source === source && held.clone.readyState === 'live') return held.clone
  dropLayerClone(st, index)
  const clone = source.clone()
  clone.contentHint = source.contentHint
  st.cameraLayerClones[index] = { source, clone }
  return clone
}

/** Stops lower layer `index`'s clone of the capture, if it has one. */
export function dropLayerClone(st: Cx['st'], index: number) {
  st.cameraLayerClones[index]?.clone.stop()
  st.cameraLayerClones[index] = null
}

/** The camera stopped: the lower layers go quiet, their clones stop, and the next camera declares afresh. */
export function releaseCameraLayers(cx: Cx) {
  const { voice, st } = cx
  st.declaredCameraKey = ''
  voice.cameraLayersWanted = null
  for (const sender of st.cameraLayerSenders) {
    if (sender?.track) void replaceQuietly(sender, null)
  }
  st.cameraLayerClones.forEach((_, i) => dropLayerClone(st, i))
}
