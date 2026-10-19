import { sealMedia, openMedia, videoCodecOf, type FrameKind } from './codec'
import { FrameStream, LAYER_STREAM_MASK, SOURCE_SCREEN, isFrameSource, layerStreamId, openFrame, ReplayWindow, sealFrame, type EpochKeys, type FrameSource } from './frame'
import { PREVIOUS_EPOCH_MS, PREVIOUS_GROUP_MS, SEND_GRACE_MS, STALE_SEAL_MS, V2_GRACE_MS } from './bounds'
import { clock } from './clock'

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
  /** The layer of a screen share this line sends (0..3); absent for every other line. */
  layer?: unknown
  /** What the line carries (frame.ts SOURCE_*). */
  source?: unknown
}

interface RtpTransformer {
  readable: ReadableStream<EncodedFrame>
  writable: WritableStream<EncodedFrame>
  options: TransformOptions
}

// Keys of the group in use, and of the group before it. When the call
// regroups (a lower id arrives, the committer reloads), members join the new
// group one after another; until they all have, frames under the old keys keep
// arriving, and this member keeps sealing with them until its new key comes.
// The previous group's keys only open frames, and only for a while.
let keys = new Map<number, EpochKeys>()
let previous = new Map<number, EpochKeys>()
let regroupedAt = -Infinity
// Every port of the call delivers the same key message: apply each once.
let lastSeq = -1
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
// Epochs in which every member opens version 3, with when this worker heard
// so: sealed as version 3 from then on, never as version 2 again in that
// epoch, and version 2 units of that epoch stop opening shortly after.
const v3Epochs = new Map<number, number>()
let sendEpoch: number | null = null
// When each epoch that is no longer the newest of its group was left, so its
// key stops opening frames PREVIOUS_EPOCH_MS later.
let retiredAt = new Map<number, number>()
// After a commit that removed nobody, sealing goes on under the epoch left
// until this time, so receivers have the commit when the first frame arrives.
let sendHold: { epoch: number, until: number } | null = null
// Since when the call has no group (the keys are the last ones it had).
let staleSince: number | null = null

function newest(map: ReadonlyMap<number, EpochKeys>): number | null {
  return map.size ? Math.max(...map.keys()) : null
}

function remember(epoch: number, key: EpochKeys, replace: boolean, seq: number | null, soft = false, caps: Caps | null = null) {
  if (seq !== null) {
    if (seq <= lastSeq) return
    lastSeq = seq
  }
  if (replace || (sendEpoch !== null && epoch < sendEpoch)) {
    // A new group: what this member held so far is now the previous group.
    if (keys.size) {
      previous = keys
      regroupedAt = clock.now()
    }
    keys = new Map()
    retiredAt = new Map()
    sendHold = null
    v3Epochs.clear()
  }
  const left = sendEpoch
  const now = clock.now()
  keys.set(epoch, key)
  retiredAt.delete(epoch)
  for (const held of keys.keys()) if (held < epoch && !retiredAt.has(held)) retiredAt.set(held, now)
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
  sendEpoch = newest(keys)
  sendHold = soft && !replace && left !== null && sendEpoch !== null && left < sendEpoch && keys.has(left)
    ? { epoch: left, until: now + Math.min(SEND_GRACE_MS, PREVIOUS_EPOCH_MS) }
    : null
  dropExpired()
  staleSince = null
  retainReplays()
  if (caps) applyCaps(epoch, caps)
}

interface Caps { members: string[], complete: boolean }

function isCaps(value: unknown): value is Caps {
  return !!value && typeof value === 'object' && 'members' in value && Array.isArray(value.members) && value.members.every(id => typeof id === 'string')
    && 'complete' in value && typeof value.complete === 'boolean'
}

/** Whether all of this epoch's members open version 3. Once they do, that stays so for the epoch. */
function applyCaps(epoch: number, caps: Caps) {
  if (caps.complete && keys.has(epoch) && !v3Epochs.has(epoch)) v3Epochs.set(epoch, clock.now())
}

/**
 * Version 2 units open while some member of their epoch cannot open version
 * 3 (their senders seal version 2 for it). Once every member can, senders
 * switch within a moment (V2_GRACE_MS, while they hear the others' messages)
 * and version 2 of that epoch opens no more: nobody honest makes it, so it is
 * a downgrade or a replay.
 */
function acceptsV2(epoch: number): boolean {
  const since = v3Epochs.get(epoch)
  return since === undefined || clock.now() - since < V2_GRACE_MS
}

/** The epoch before the newest opens frames only briefly (PREVIOUS_EPOCH_MS); then its key and verifiers are gone. */
function dropExpired() {
  const now = clock.now()
  let dropped = false
  for (const [epoch, since] of retiredAt) {
    if (now - since < PREVIOUS_EPOCH_MS && keys.has(epoch)) continue
    retiredAt.delete(epoch)
    if (keys.delete(epoch)) dropped = true
  }
  for (const epoch of [...v3Epochs.keys()]) if (!keys.has(epoch)) v3Epochs.delete(epoch)
  if (dropped) retainReplays()
}

function retainReplays() {
  const held = new Set([...keys.keys(), ...previous.keys()])
  for (const window of replays.values()) window.retain(held)
}

/** The previous group's keys, while they may still open frames. */
function previousKeys(): ReadonlyMap<number, EpochKeys> {
  if (previous.size && clock.now() - regroupedAt > PREVIOUS_GROUP_MS) {
    previous = new Map()
    retainReplays()
  }
  return previous
}

function forget() {
  lastSeq = -1
  staleSince = null
  keys = new Map()
  previous = new Map()
  retiredAt = new Map()
  sendHold = null
  regroupedAt = -Infinity
  sendEpoch = null
  v3Epochs.clear()
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
  /** What the line carries; a receiver learns it with the publisher. */
  source: FrameSource | null
  layer: number
}

/** Seals one frame of a sending line under the epoch it is to go out in. */
async function sealOutgoing(data: Uint8Array, kind: FrameKind, codec: ReturnType<typeof videoCodecOf> | null, line: Line, senderId: string): Promise<ArrayBuffer | null> {
  const { channelId } = line
  if (sendEpoch === null) return null
  const now = clock.now()
  if (staleSince !== null && now - staleSince > STALE_SEAL_MS) return null
  dropExpired()
  const hold = sendHold
  if (hold && now >= hold.until) sendHold = null
  const epoch = hold && now < hold.until && keys.has(hold.epoch) ? hold.epoch : sendEpoch
  const epochKeys = keys.get(epoch)
  const stream = line.stream
  if (!epochKeys || !stream) return null
  const sealed = await sealMedia(data, kind, codec, (plaintext, clear) => {
    const counter = stream.next()
    return counter === null
      ? Promise.resolve(null)
      : sealFrame({ keys: epochKeys, epoch, stream: stream.id, counter, channelId, senderId, plaintext, clear, version: v3Epochs.has(epoch) ? 3 : 2, source: line.source as FrameSource, layer: line.layer })
  })
  return sealed ? packet(sealed) : null
}

/** Opens one frame of a receiving line, and counts what it learns about the publisher's epoch. */
async function openIncoming(data: Uint8Array, kind: FrameKind, codec: ReturnType<typeof videoCodecOf> | null, line: Line, senderId: string): Promise<ArrayBuffer | null> {
  const { channelId } = line
  const replay = replayOf(senderId)
  dropExpired()
  let unknown = -1
  const before = previousKeys()
  const opened = await openMedia(data, kind, codec, (sealed, clear) => openFrame({
    keys, previous: before, channelId, senderId, packet: sealed, clear, replay, source: line.source, acceptV2: acceptsV2, onUnknownEpoch: (epoch) => { unknown = Math.max(unknown, epoch) },
  }))
  count(senderId, opened !== null, unknown)
  return opened ? packet(opened) : null
}

async function protect(frame: EncodedFrame, line: Line): Promise<ArrayBuffer | null> {
  const { direction, senderId } = line
  // A receiver whose publisher is not known yet opens nothing.
  if (senderId === null) return null
  const kind: FrameKind = frame.type === 'key' || frame.type === 'delta' ? frame.type : undefined
  // A video frame of another type is not one this worker can seal.
  if (frame.type !== undefined && kind === undefined) return null
  const codec = kind === undefined ? null : videoCodecOf(frame.getMetadata?.().mimeType)
  const data = new Uint8Array(frame.data)
  return direction === 'send' ? sealOutgoing(data, kind, codec, line, senderId) : openIncoming(data, kind, codec, line, senderId)
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
  const latest = newest(keys) ?? -1
  const entry = stats.get(senderId) ?? { opened: 0, newer: 0, epoch: -1 }
  if (opened) entry.opened++
  else if (unknownEpoch > latest) {
    entry.newer++
    entry.epoch = Math.max(entry.epoch, unknownEpoch)
  } else return
  stats.set(senderId, entry)
  const now = clock.now()
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

/** The line a transform serves, or null when its options are not ones this worker will seal or open for. */
function lineFrom(options: TransformOptions | undefined): Line | null {
  const direction = directionOf(options?.direction)
  const channelId = options?.channelId
  // A receiver may be armed before the SFU says who publishes on its line.
  const senderId = typeof options?.senderId === 'string' && options.senderId ? options.senderId : null
  if (!direction || typeof channelId !== 'string' || !channelId || (direction === 'send' && senderId === null)) return null
  // Every sending transform starts a fresh stream; a screen share's layers
  // carry their layer in the stream ID, so they cannot share one.
  const layer = options?.layer
  const source = isFrameSource(options?.source) ? options.source : null
  // Every sending line says what it carries; without that nothing is sealed.
  if (direction === 'send' && source === null) return null
  const layerNo = typeof layer === 'number' && Number.isInteger(layer) && layer >= 0 && layer <= LAYER_STREAM_MASK ? layer : source === SOURCE_SCREEN ? 0 : undefined
  const layerId = layerNo === undefined ? undefined : layerStreamId(layerNo)
  return { direction, channelId, senderId, stream: direction === 'send' ? new FrameStream(layerId) : null, source, layer: layerNo ?? 0 }
}

/** The key message of the call: applied once however many lines deliver it. */
function onKeyMessage(data: object & { epoch?: unknown, key?: unknown }): boolean {
  if (!('type' in data) || data.type !== 'key' || !('epoch' in data) || !('key' in data) || !isEpoch(data.epoch) || !isEpochKeys(data.key)) return false
  const seq = 'seq' in data && typeof data.seq === 'number' ? data.seq : null
  remember(data.epoch, data.key, 'replace' in data && data.replace === true, seq, 'soft' in data && data.soft === true, 'caps' in data && isCaps(data.caps) ? data.caps : null)
  return true
}

function onCapsMessage(data: object) {
  if (!('type' in data) || data.type !== 'caps' || !('epoch' in data) || !('caps' in data) || !isEpoch(data.epoch) || !isCaps(data.caps)) return
  const seq = 'seq' in data && typeof data.seq === 'number' ? data.seq : null
  if (seq !== null) {
    if (seq <= lastSeq) return
    lastSeq = seq
  }
  applyCaps(data.epoch, data.caps)
}

/** What the call tells a line over its port: drop, stale, who publishes, a key or capabilities. */
function onLineMessage(line: Line, data: unknown) {
  if (!data || typeof data !== 'object') return
  if ('type' in data && data.type === 'drop') {
    forget()
    return
  }
  if ('type' in data && data.type === 'stale') {
    staleSince ??= clock.now()
    return
  }
  if (line.direction === 'recv' && 'type' in data && data.type === 'sender' && 'senderId' in data && typeof data.senderId === 'string' && data.senderId) {
    line.senderId = data.senderId
    // A reused receiver may now carry another source.
    line.source = 'source' in data && isFrameSource(data.source) ? data.source : null
    return
  }
  if (onKeyMessage(data)) return
  onCapsMessage(data)
}

scope.onrtctransform = (event) => {
  const transformer = event.transformer
  const port = transformer?.options?.port
  const line = lineFrom(transformer?.options)
  if (!transformer || !port || !line) return
  port.onmessage = (message: MessageEvent<unknown>) => onLineMessage(line, message.data)
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
