import { sealMedia, openMedia, type FrameKind } from './codec'
import { openFrame, sealFrame } from './frame'

interface EncodedFrame {
  data: ArrayBuffer
  /** Only video frames have a type. */
  type?: string
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

const keys = new Map<number, CryptoKey>()
let sendEpoch: number | null = null

function remember(epoch: number, key: CryptoKey, replace: boolean) {
  if (replace || (sendEpoch !== null && epoch < sendEpoch)) keys.clear()
  keys.set(epoch, key)
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
  // A key was just stored, so the newest epoch is present.
  sendEpoch = ordered[ordered.length - 1] as number
}

function forget() {
  keys.clear()
  sendEpoch = null
}

function isEpoch(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 0xffffffff
}

function packet(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

async function protect(frame: EncodedFrame, direction: 'send' | 'recv', channelId: string, senderId: string | null): Promise<ArrayBuffer | null> {
  // A receiver whose publisher is not known yet opens nothing.
  if (senderId === null) return null
  const kind: FrameKind = frame.type === 'key' || frame.type === 'delta' ? frame.type : undefined
  const data = new Uint8Array(frame.data)
  if (direction === 'send') {
    if (sendEpoch === null) return null
    const key = keys.get(sendEpoch)
    if (!key) return null
    const epoch = sendEpoch
    const sealed = await sealMedia(data, kind, (plaintext, clear) => sealFrame({ key, epoch, channelId, senderId, plaintext, clear }))
    return sealed ? packet(sealed) : null
  }
  const opened = await openMedia(data, kind, (sealed, clear) => openFrame({ keys, channelId, senderId, packet: sealed, clear }))
  return opened ? packet(opened) : null
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
  let senderId = typeof options?.senderId === 'string' && options.senderId ? options.senderId : null
  if (!transformer || !port || !direction || typeof channelId !== 'string' || !channelId || (direction === 'send' && senderId === null)) return
  port.onmessage = (message: MessageEvent<unknown>) => {
    const data = message.data
    if (!data || typeof data !== 'object') return
    if ('type' in data && data.type === 'drop') {
      forget()
      return
    }
    if (direction === 'recv' && 'type' in data && data.type === 'sender' && 'senderId' in data && typeof data.senderId === 'string' && data.senderId) {
      senderId = data.senderId
      return
    }
    if ('type' in data && data.type === 'key' && 'epoch' in data && 'key' in data && isEpoch(data.epoch) && data.key) {
      remember(data.epoch, data.key as CryptoKey, 'replace' in data && data.replace === true)
    }
  }
  transformer.readable.pipeThrough(new TransformStream<EncodedFrame, EncodedFrame>({
    async transform(frame, controller) {
      try {
        const next = await protect(frame, direction, channelId, senderId)
        if (!next) return
        frame.data = next
        controller.enqueue(frame)
      } catch {
        // A failed seal or open never becomes a plaintext frame.
      }
    },
  })).pipeTo(transformer.writable).catch(() => {})
}
