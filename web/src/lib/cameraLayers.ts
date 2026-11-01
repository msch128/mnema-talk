// A camera can go out as up to three encodings of the same capture ("layers",
// best first), each on a sending line of its own, like a screen share (see
// lib/screenLayers). The SFU forwards exactly one layer to each viewer, by
// the size of that viewer's picture of the camera: a small tile gets the
// thumbnail, the stage the best. A capture too small for a second layer, a
// browser without the lines and an older server send the camera as one layer,
// as before.
import type { ScreenLayer } from '../types/events'
import { MAX_SCREEN_LAYERS } from '../types/events'
import type { LayerPlan } from './screenLayers'
import { MIN_LAYER_STEP } from './screenLayers'

/** Heights the lower layers aim for, best first. */
export const CAMERA_LAYER_HEIGHTS = [360, 180] as const
/** The thumbnail is not worth less than this. */
export const MIN_CAMERA_LAYER_HEIGHT = 180
/** Frame rate cap of the lower layers; the thumbnail moves less. */
const CAMERA_LAYER_FPS = [30, 15] as const
/** Bits per second a camera layer of this height is capped at. */
const CAMERA_LAYER_BITRATE: Record<number, number> = { 360: 500_000, 180: 150_000 }
/** The best layer's cap (as the camera had it before layers). */
export const CAMERA_TOP_BITRATE = 2_500_000

function evenWidth(width: number): number {
  return Math.max(2, Math.floor(width / 2) * 2)
}

/** The bitrate cap of a lower layer: the table's, or one in proportion to its pixels. */
function lowerBitrate(height: number, width: number): number {
  return CAMERA_LAYER_BITRATE[height] ?? Math.round(width * height * 0.15 / 1000) * 1000
}

/**
 * The layers of a camera captured at `settings`: the capture as it is, and
 * smaller ones when they are worth sending. Empty while the capture's size is
 * unknown; one layer when nothing smaller is worth it.
 */
export function planCameraLayers(settings?: MediaTrackSettings, topBitrate = CAMERA_TOP_BITRATE, topFps = 30): LayerPlan[] {
  const height = Number(settings?.height) || 0
  const width = Number(settings?.width) || 0
  if (!height || !width) return []
  const fps = Math.round(Number(settings?.frameRate) || topFps)
  const plans: LayerPlan[] = [{ width, height, fps, scale: 1, maxFramerate: Math.max(fps, 1), maxBitrate: topBitrate }]
  CAMERA_LAYER_HEIGHTS.forEach((target, i) => {
    if (plans.length >= MAX_SCREEN_LAYERS) return
    const above = plans[plans.length - 1]
    if (!above || target < MIN_CAMERA_LAYER_HEIGHT || target > Math.floor(above.height / MIN_LAYER_STEP)) return
    const scale = Math.round((height / target) * 1000) / 1000
    const w = evenWidth(width / scale)
    const rate = Math.min(fps, CAMERA_LAYER_FPS[i] ?? 15)
    plans.push({ width: w, height: target, fps: rate, scale, maxFramerate: rate, maxBitrate: lowerBitrate(target, w) })
  })
  return plans
}

/** What goes to the server: size and rate only. */
export function declaredCameraLayers(plans: readonly LayerPlan[]): ScreenLayer[] {
  return plans.map(({ width, height, fps }) => ({ width, height, fps }))
}

/** Equal for equal layer lists. */
export function cameraLayersKey(layers: readonly ScreenLayer[]): string {
  return layers.map(l => `${l.width}x${l.height}@${l.fps}`).join(',')
}

/** Device pixels of a picture's height (CSS pixels times the pixel ratio), rounded; 0 while unknown. */
export function pictureHeightPx(cssHeight: number, pixelRatio = 1): number {
  if (!(cssHeight > 0)) return 0
  return Math.round(cssHeight * (pixelRatio > 0 ? pixelRatio : 1))
}
