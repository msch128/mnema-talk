import { sealMedia, openMedia, videoCodecOf, type FrameKind } from './codec'
import { FrameStream, openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'

interface EncodedFrame {
  data: ArrayBuffer
  /** Only video frames have a type. */
  type?: string
  getMetadata?: () => { mimeType?: unknown }
}

interface TransformOptions {
  port?: MessagePort
  direction?: unknown
  channelId?: unknown
  senderId?: unknown
}

interface RtpTransformer {
  readable: ReadableStream<EncodedFrame>
  writable: WritableStream<EncodedFrame>
  options: TransformOptions
}

const keys = new Map<number, EpochKeys>()
// Per publisher, shared by every receiving line of this worker (one worker
// serves the whole call): a unit opens once, whichever line or recreated
// receiver it is played into.
const replays = new Map<string, ReplayWindow>()

function replayOf(senderId: string): ReplayWindow {
  let window = replays.get(senderId)
  if (!window) {
    window = new ReplayWindow()
    replays.set(senderId, window)
  }
  return window
}
let sendEpoch: number | null = null

function remember(epoch: number, key: EpochKeys, replace: boolean) {
  if (replace || (sendEpoch !== null && epoch < sendEpoch)) keys.clear()
  keys.set(epoch, key)
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
  // A key was just stored, so the newest epoch is present.
  sendEpoch = ordered[ordered.length - 1] as number
  for (const window of replays.values()) window.retain(keys.keys())
}

function forget() {
  keys.clear()
  sendEpoch = null
  replays.clear()
}

function isEpochKeys(value: unknown): value is EpochKeys {
  return !!value && typeof value === 'object' && 'key' in value && !!value.key && 'signer' in value && !!value.signer
    && 'verifiers' in value && value.verifiers instanceof Map
}

function isEpoch(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 0xffffffff
}

function packet(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

interface Line {
  direction: 'send' | 'recv'
  channelId: string
  senderId: string | null
  stream: FrameStream | null
}

async function protect(frame: EncodedFrame, line: Line): Promise<ArrayBuffer | null> {
  const { direction, channelId, senderId } = line
  // A receiver whose publisher is not known yet opens nothing.
  if (senderId === null) return null
  const kind: FrameKind = frame.type === 'key' || frame.type === 'delta' ? frame.type : undefined
  // A video frame of another type is not one this worker can seal.
  if (frame.type !== undefined && kind === undefined) return null
  const codec = kind === undefined ? null : videoCodecOf(frame.getMetadata?.().mimeType)
  const data = new Uint8Array(frame.data)
  if (direction === 'send') {
    if (sendEpoch === null) return null
    const epochKeys = keys.get(sendEpoch)
    const stream = line.stream
    if (!epochKeys || !stream) return null
    const epoch = sendEpoch
    const sealed = await sealMedia(data, kind, codec, (plaintext, clear) => {
      const counter = stream.next()
      return counter === null
        ? Promise.resolve(null)
        : sealFrame({ keys: epochKeys, epoch, stream: stream.id, counter, channelId, senderId, plaintext, clear })
    })
    return sealed ? packet(sealed) : null
  }
  const replay = replayOf(senderId)
  let unknown = -1
  const opened = await openMedia(data, kind, codec, (sealed, clear) => openFrame({
    keys, channelId, senderId, packet: sealed, clear, replay, onUnknownEpoch: (epoch) => { unknown = Math.max(unknown, epoch) },
  }))
  count(senderId, opened !== null, unknown)
  return opened ? packet(opened) : null
}

// What the call needs to judge whether it fell behind: per publisher, how many
// frames opened and how many came from an epoch newer than any key held. A
// frame names its own epoch before it can be checked, so one frame proves
// nothing; the call weighs this against the roster.
interface SenderStats { opened: number, newer: number, epoch: number }
const STATS_MS = 1000
const stats = new Map<string, SenderStats>()
let lastStats = -Infinity

function count(senderId: string, opened: boolean, unknownEpoch: number) {
  const newest = Math.max(-1, ...keys.keys())
  const entry = stats.get(senderId) ?? { opened: 0, newer: 0, epoch: -1 }
  if (opened) entry.opened++
  else if (unknownEpoch > newest) {
    entry.newer++
    entry.epoch = Math.max(entry.epoch, unknownEpoch)
  } else return
  stats.set(senderId, entry)
  const now = Date.now()
  if (now - lastStats < STATS_MS) return
  lastStats = now
  self.postMessage({ type: 'stats', senders: Object.fromEntries(stats) })
  stats.clear()
}

function directionOf(value: unknown): 'send' | 'recv' | null {
  return value === 'send' || value === 'recv' ? value : null
}

const scope = self as typeof self & {
  onrtctransform: ((event: Event & { transformer?: RtpTransformer }) => void) | null
}

scope.onrtctransform = (event) => {
  const transformer = event.transformer
  const options = transformer?.options
  const port = options?.port
  const direction = directionOf(options?.direction)
  const channelId = options?.channelId
  // A receiver may be armed before the SFU says who publishes on its line.
  const senderId = typeof options?.senderId === 'string' && options.senderId ? options.senderId : null
  if (!transformer || !port || !direction || typeof channelId !== 'string' || !channelId || (direction === 'send' && senderId === null)) return
  // Every sending transform starts a fresh stream.
  const line: Line = { direction, channelId, senderId, stream: direction === 'send' ? new FrameStream() : null }
  port.onmessage = (message: MessageEvent<unknown>) => {
    const data = message.data
    if (!data || typeof data !== 'object') return
    if ('type' in data && data.type === 'drop') {
      forget()
      return
    }
    if (direction === 'recv' && 'type' in data && data.type === 'sender' && 'senderId' in data && typeof data.senderId === 'string' && data.senderId) {
      line.senderId = data.senderId
      return
    }
    if ('type' in data && data.type === 'key' && 'epoch' in data && 'key' in data && isEpoch(data.epoch) && isEpochKeys(data.key)) {
      remember(data.epoch, data.key, 'replace' in data && data.replace === true)
    }
  }
  transformer.readable.pipeThrough(new TransformStream<EncodedFrame, EncodedFrame>({
    async transform(frame, controller) {
      try {
        const next = await protect(frame, line)
        if (!next) return
        frame.data = next
        controller.enqueue(frame)
      } catch {
        // A failed seal or open never becomes a plaintext frame.
      }
    },
  })).pipeTo(transformer.writable).catch(() => {})
}
