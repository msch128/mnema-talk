// A screen share can go out as up to three encodings of the same capture
// ("layers", best first), each on a sending line of its own, so every viewer
// can receive the one that suits their connection (see doc/architecture.md,
// "Layers of a screen share"). Layer 0 is the share as the streamer chose it;
// the others are smaller and capped at 30 frames per second. The SFU forwards
// exactly one layer to each viewer and never decodes anything.
import type { ScreenLayer } from '../types/events'
import { MAX_SCREEN_LAYERS } from '../types/events'
import { bitrateFor, normalizeQuality, sentSize, streamEncoding } from './streamQuality'

/** Heights the lower layers aim for, best first. */
export const SCREEN_LAYER_HEIGHTS = [1080, 720, 480, 360] as const
/** A lower layer must be at least this much smaller than the one above it, or it adds nothing. */
export const MIN_LAYER_STEP = 1.25
/** Frame rate cap of the lower layers. */
export const LOWER_LAYER_FPS = 30

/** A layer as declared to the server, plus what the sender needs to produce it. */
export interface LayerPlan extends ScreenLayer {
  /** scaleResolutionDownBy, relative to the captured track. */
  scale: number
  maxFramerate: number
  /** Bits per second. */
  maxBitrate: number
}

// Encoders round a scaled picture down to an even size.
function evenWidth(width: number): number {
  return Math.max(2, Math.floor(width / 2) * 2)
}

/**
 * The layers of a share captured at `settings` and shared in quality `q`.
 * Empty while the capture's size is unknown; one layer when nothing smaller is
 * worth sending.
 */
export function planScreenLayers(q: unknown, settings: MediaTrackSettings = {}): LayerPlan[] {
  const quality = normalizeQuality(q)
  const captureHeight = Number(settings.height) || 0
  const captureWidth = Number(settings.width) || 0
  const top = sentSize(quality, settings)
  if (!top || !captureHeight) return []
  const encoding = streamEncoding(quality, settings)
  const plans: LayerPlan[] = [{
    width: top.width, height: top.height, fps: quality.fps,
    scale: encoding.scaleResolutionDownBy, maxFramerate: encoding.maxFramerate, maxBitrate: encoding.maxBitrate
  }]
  const fps = Math.min(quality.fps, LOWER_LAYER_FPS)
  for (const height of SCREEN_LAYER_HEIGHTS) {
    if (plans.length >= MAX_SCREEN_LAYERS) break
    const above = plans[plans.length - 1]
    if (!above || height > Math.floor(above.height / MIN_LAYER_STEP)) continue
    const scale = Math.round((captureHeight / height) * 1000) / 1000
    const width = evenWidth(captureWidth / scale)
    plans.push({
      width, height, fps,
      scale,
      maxFramerate: fps,
      maxBitrate: bitrateFor(width, height, fps)
    })
  }
  return plans
}

/** What goes to the server and the room: size and rate only. */
export function declaredLayers(plans: readonly LayerPlan[]): ScreenLayer[] {
  return plans.map(({ width, height, fps }) => ({ width, height, fps }))
}

/** "1080p", or "1440p60" where the frame rate is above 30. */
export function layerName(layer: ScreenLayer): string {
  return `${layer.height}p${layer.fps > LOWER_LAYER_FPS ? layer.fps : ''}`
}

/** "1920×1080 · 30 fps". */
export function layerDetail(layer: ScreenLayer): string {
  return `${layer.width}×${layer.height} · ${layer.fps} fps`
}

/** Equal for equal layer lists; a viewer's choice only lasts as long as this does. */
export function layersKey(layers: readonly ScreenLayer[]): string {
  return layers.map(l => `${l.width}x${l.height}@${l.fps}`).join(',')
}
