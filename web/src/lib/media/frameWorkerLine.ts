import { FrameStream, LAYER_STREAM_MASK, SOURCE_SCREEN, isFrameSource, layerStreamId, type FrameSource } from './frame'

export interface TransformOptions {
  port?: MessagePort
  direction?: unknown
  channelId?: unknown
  senderId?: unknown
  /** The layer of a screen share this line sends (0..3); absent for every other line. */
  layer?: unknown
  /** What the line carries (frame.ts SOURCE_*). */
  source?: unknown
  /** The microphone's Opus was negotiated with DTX (usedtx=1): its empty frames are not sent. */
  dtx?: unknown
}

export interface Line {
  direction: 'send' | 'recv'
  channelId: string
  senderId: string | null
  stream: FrameStream | null
  /** What the line carries; a receiver learns it with the publisher. */
  source: FrameSource | null
  layer: number
  /** A sending line whose encoder uses DTX; its empty frames are dropped (see mediaFrameWorker). */
  dtx: boolean
}

function directionOf(value: unknown): 'send' | 'recv' | null {
  return value === 'send' || value === 'recv' ? value : null
}

function publisherId(value: unknown): string | null {
  if (typeof value !== 'string') return null
  return value ? value : null
}

function channelOf(value: unknown): string | null {
  if (typeof value !== 'string') return null
  return value ? value : null
}

function isLayer(layer: unknown): layer is number {
  if (typeof layer !== 'number') return false
  if (!Number.isInteger(layer)) return false
  return layer >= 0 && layer <= LAYER_STREAM_MASK
}

/** A screen share with no layer number sends layer 0. Every other line leaves it unset. */
function screenLayer(layer: unknown, source: FrameSource | null): number | undefined {
  if (isLayer(layer)) return layer
  if (source === SOURCE_SCREEN) return 0
  return undefined
}

function carriedSource(direction: 'send' | 'recv', value: unknown): FrameSource | null | undefined {
  const source = isFrameSource(value) ? value : null
  // Every sending line says what it carries; without that nothing is sealed.
  if (direction === 'send' && source === null) return undefined
  return source
}

function sendingStream(direction: 'send' | 'recv', layerNo: number | undefined): FrameStream | null {
  if (direction !== 'send') return null
  // Every sending transform starts a fresh stream; a screen share's layers
  // carry their layer in the stream ID, so they cannot share one.
  const layerId = layerNo === undefined ? undefined : layerStreamId(layerNo)
  return new FrameStream(layerId)
}

/** The line a transform serves, or null when its options are not ones this worker will seal or open for. */
export function lineFrom(options: TransformOptions | undefined): Line | null {
  const direction = directionOf(options?.direction)
  const channelId = channelOf(options?.channelId)
  // A receiver may be armed before the SFU says who publishes on its line.
  const senderId = publisherId(options?.senderId)
  if (direction === null || channelId === null) return null
  if (direction === 'send' && senderId === null) return null
  const source = carriedSource(direction, options?.source)
  if (source === undefined) return null
  const layerNo = screenLayer(options?.layer, source)
  return {
    direction,
    channelId,
    senderId,
    stream: sendingStream(direction, layerNo),
    source,
    layer: layerNo ?? 0,
    dtx: direction === 'send' && options?.dtx === true,
  }
}
