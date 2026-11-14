import type { LayerClone } from './state'

// One clone of the capture per lower layer line (camera and screen share).

/**
 * The track lower layer `index` of a camera or a screen share encodes: a clone
 * of the capture, its own per layer. The browser adapts a track to the most constrained sender it feeds,
 * so with one track on every line a best layer cut down for bandwidth shrank
 * every layer with it (a 180-line thumbnail went out at 90 lines, and only
 * slowly grew back). A clone is made once per capture and line, and stopped
 * when the capture or the line no longer needs it: a clone keeps the camera
 * (or the browser's sharing indicator) on.
 */
export function layerClone(slots: (LayerClone | null)[], index: number, source: MediaStreamTrack): MediaStreamTrack {
  const held = slots[index]
  if (held && held.source === source && held.clone.readyState === 'live') return held.clone
  dropLayerClone(slots, index)
  const clone = source.clone()
  clone.contentHint = source.contentHint
  slots[index] = { source, clone }
  return clone
}

/** Stops lower layer `index`'s clone, if it has one. */
export function dropLayerClone(slots: (LayerClone | null)[], index: number) {
  slots[index]?.clone.stop()
  slots[index] = null
}

/** Stops clones for lines that have no sender (after a reconnect or renegotiation). */
export function dropMissingSenderClones(senders: (RTCRtpSender | null | undefined)[], slots: (LayerClone | null)[]) {
  for (let i = 0; i < slots.length; i++) {
    if (!senders[i]) dropLayerClone(slots, i)
  }
}

/** Stops every clone in `slots`: the capture stopped or was replaced. */
export function dropAllClones(slots: (LayerClone | null)[]) {
  slots.forEach((_, i) => dropLayerClone(slots, i))
}
