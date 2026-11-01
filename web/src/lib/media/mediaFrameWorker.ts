import { sealMedia, openMedia, videoCodecOf, type FrameKind } from './codec'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys, type FrameSource, type FrameStream } from './frame'
import { PREVIOUS_EPOCH_MS, PREVIOUS_GROUP_MS, SEND_GRACE_MS, STALE_SEAL_MS } from './bounds'
import { clock } from './clock'
import { lineFrom, type Line, type TransformOptions } from './frameWorkerLine'
import { onLineMessage, type LineActions, type RememberedKey } from './frameWorkerPort'

interface EncodedFrame {
  data: ArrayBuffer
  /** Only video frames have a type. */
  type?: string
  getMetadata?: () => { mimeType?: unknown }
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

/** False when this delivery is a repeat of a key message already applied. */
function noteSeq(seq: number | null): boolean {
  if (seq === null) return true
  if (seq <= lastSeq) return false
  lastSeq = seq
  return true
}

function beginsGroup(replace: boolean, epoch: number): boolean {
  if (replace) return true
  return sendEpoch !== null && epoch < sendEpoch
}

function parkPreviousGroup() {
  // A new group: what this member held so far is now the previous group.
  if (!keys.size) return
  previous = keys
  regroupedAt = clock.now()
}

function resetGroup() {
  keys = new Map()
  retiredAt = new Map()
  sendHold = null
}

function retireOlderEpochs(epoch: number, now: number) {
  for (const held of keys.keys()) {
    if (held < epoch && !retiredAt.has(held)) retiredAt.set(held, now)
  }
}

function keepTwoNewest() {
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
}

function keepsSealing(soft: boolean, replace: boolean, left: number | null): boolean {
  if (!soft || replace) return false
  if (left === null || sendEpoch === null) return false
  return left < sendEpoch && keys.has(left)
}

function remember(item: RememberedKey) {
  if (!noteSeq(item.seq)) return
  if (beginsGroup(item.replace, item.epoch)) {
    parkPreviousGroup()
    resetGroup()
  }
  const left = sendEpoch
  const now = clock.now()
  keys.set(item.epoch, item.key)
  retiredAt.delete(item.epoch)
  retireOlderEpochs(item.epoch, now)
  keepTwoNewest()
  sendEpoch = newest(keys)
  const sealing = keepsSealing(item.soft, item.replace, left)
  sendHold = sealing && left !== null
    ? { epoch: left, until: now + Math.min(SEND_GRACE_MS, PREVIOUS_EPOCH_MS) }
    : null
  dropExpired()
  staleSince = null
  retainReplays()
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
  replays.clear()
}

function packet(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

function sealIsStale(now: number): boolean {
  return staleSince !== null && now - staleSince > STALE_SEAL_MS
}

function clearExpiredHold(now: number) {
  const hold = sendHold
  if (!hold || now < hold.until) return
  sendHold = null
}

function epochToSeal(now: number): number | null {
  const hold = sendHold
  if (hold && now < hold.until && keys.has(hold.epoch)) return hold.epoch
  return sendEpoch
}

interface SealReady {
  epoch: number
  epochKeys: EpochKeys
  stream: FrameStream
  source: FrameSource
  channelId: string
  layer: number
}

/** The epoch, keys and stream a frame may leave on, or null when sealing must drop it. */
function sealReady(line: Line): SealReady | null {
  if (sendEpoch === null) return null
  const now = clock.now()
  if (sealIsStale(now)) return null
  dropExpired()
  clearExpiredHold(now)
  const epoch = epochToSeal(now)
  if (epoch === null) return null
  const epochKeys = keys.get(epoch)
  const stream = line.stream
  const source = line.source
  if (!epochKeys || !stream || source === null) return null
  return { epoch, epochKeys, stream, source, channelId: line.channelId, layer: line.layer }
}

function sealUnit(ready: SealReady, senderId: string, plaintext: Uint8Array, clear: Uint8Array): Promise<Uint8Array | null> {
  const counter = ready.stream.next()
  if (counter === null) return Promise.resolve(null)
  return sealFrame({
    keys: ready.epochKeys,
    epoch: ready.epoch,
    stream: ready.stream.id,
    counter,
    channelId: ready.channelId,
    senderId,
    plaintext,
    clear,
    source: ready.source,
    layer: ready.layer,
  })
}

/** Seals one frame of a sending line under the epoch it is to go out in. */
async function sealOutgoing(data: Uint8Array, kind: FrameKind, codec: ReturnType<typeof videoCodecOf> | null, line: Line, senderId: string): Promise<ArrayBuffer | null> {
  const ready = sealReady(line)
  if (ready === null) return null
  const sealed = await sealMedia(data, kind, codec, (plaintext, clear) => sealUnit(ready, senderId, plaintext, clear))
  return sealed ? packet(sealed) : null
}

/** Opens one frame of a receiving line, and counts what it learns about the publisher's epoch. */
async function openIncoming(data: Uint8Array, kind: FrameKind, codec: ReturnType<typeof videoCodecOf> | null, line: Line, senderId: string): Promise<ArrayBuffer | null> {
  const { channelId } = line
  const replay = replayOf(senderId)
  dropExpired()
  let unknown = -1
  let legacy = false
  const before = previousKeys()
  const opened = await openMedia(data, kind, codec, (sealed, clear) => openFrame({
    keys, previous: before, channelId, senderId, packet: sealed, clear, replay, source: line.source, onLegacy: () => { legacy = true }, onUnknownEpoch: (epoch) => { unknown = Math.max(unknown, epoch) },
  }))
  count(senderId, opened !== null, unknown, legacy)
  return opened ? packet(opened) : null
}

// With Opus DTX (usedtx=1) the encoder hands a sending line a frame of one or
// two bytes, the header and nothing else, for every 20 ms in which it has
// nothing to say. The browser sends none of those itself. Sealed, each would be
// 100 bytes and go out 50 times a second, which is what DTX is there to avoid.
// They carry no sound, so on a line that negotiated DTX they are dropped; the
// listeners see the gap in the RTP timestamps and fill it with comfort noise.
const OPUS_EMPTY_FRAME_BYTES = 2

function isSilentDtxFrame(frame: EncodedFrame, line: Line): boolean {
  return line.dtx && frame.type === undefined && frame.data.byteLength <= OPUS_EMPTY_FRAME_BYTES
}

async function protect(frame: EncodedFrame, line: Line): Promise<ArrayBuffer | null> {
  const { direction, senderId } = line
  // A receiver whose publisher is not known yet opens nothing.
  if (senderId === null) return null
  if (isSilentDtxFrame(frame, line)) return null
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
interface SenderStats { opened: number, newer: number, epoch: number, legacy: number }
const STATS_MS = 1000
const stats = new Map<string, SenderStats>()
let lastStats = -Infinity

function count(senderId: string, opened: boolean, unknownEpoch: number, legacy: boolean) {
  const latest = newest(keys) ?? -1
  const entry = stats.get(senderId) ?? { opened: 0, newer: 0, epoch: -1, legacy: 0 }
  if (opened) entry.opened++
  else if (legacy) entry.legacy++
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

const scope = self as typeof self & {
  onrtctransform: ((event: Event & { transformer?: RtpTransformer }) => void) | null
}

const lineActions: LineActions = {
  drop: forget,
  stale() { staleSince ??= clock.now() },
  remember,
  noteSeq,
}

scope.onrtctransform = (event) => {
  const transformer = event.transformer
  const port = transformer?.options?.port
  const line = lineFrom(transformer?.options)
  if (!transformer || !port || !line) return
  port.onmessage = (message: MessageEvent<unknown>) => onLineMessage(line, message.data, lineActions)
  transformer.readable.pipeThrough(new TransformStream<EncodedFrame, EncodedFrame>({
    async transform(frame, controller) {
      try {
        const next = await protect(frame, line)
        if (!next) return
        frame.data = next
        controller.enqueue(frame)
      } catch (error) {
        dropUnsealed(error)
      }
    },
  })).pipeTo(transformer.writable).catch(dropUnsealed)
}

/** A failed seal or open never becomes a plaintext frame. */
function dropUnsealed(error: unknown) {
  void error
}
