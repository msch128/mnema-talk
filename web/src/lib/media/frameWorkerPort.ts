import { isFrameSource, type EpochKeys } from './frame'
import type { Line } from './frameWorkerLine'

export interface Caps {
  members: string[]
  complete: boolean
}

export interface RememberedKey {
  epoch: number
  key: EpochKeys
  replace: boolean
  seq: number | null
  soft: boolean
  caps: Caps | null
}

/** What a port message may change on the worker that owns the call's keys. */
export interface LineActions {
  drop(): void
  stale(): void
  remember(item: RememberedKey): void
  noteSeq(seq: number | null): boolean
  applyCaps(epoch: number, caps: Caps): void
}

function isCaps(value: unknown): value is Caps {
  return !!value && typeof value === 'object' && 'members' in value && Array.isArray(value.members) && value.members.every(id => typeof id === 'string')
    && 'complete' in value && typeof value.complete === 'boolean'
}

function isEpochKeys(value: unknown): value is EpochKeys {
  return !!value && typeof value === 'object' && 'key' in value && !!value.key && 'signer' in value && !!value.signer
    && 'verifiers' in value && value.verifiers instanceof Map
}

function isEpoch(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 0xffffffff
}

type CallMessage = object & {
  type?: unknown
  epoch?: unknown
  key?: unknown
  seq?: unknown
  replace?: unknown
  soft?: unknown
  caps?: unknown
  senderId?: unknown
  source?: unknown
}

function isCallMessage(data: unknown): data is CallMessage {
  return !!data && typeof data === 'object'
}

function messageType(data: CallMessage): unknown {
  return 'type' in data ? data.type : null
}

function optionalSeq(data: CallMessage): number | null {
  if (!('seq' in data)) return null
  return typeof data.seq === 'number' ? data.seq : null
}

function isKeyPayload(data: CallMessage): data is CallMessage & { epoch: number, key: EpochKeys } {
  if (messageType(data) !== 'key') return false
  if (!('epoch' in data) || !('key' in data)) return false
  return isEpoch(data.epoch) && isEpochKeys(data.key)
}

function isCapsPayload(data: CallMessage): data is CallMessage & { epoch: number, caps: Caps } {
  if (messageType(data) !== 'caps') return false
  if (!('epoch' in data) || !('caps' in data)) return false
  return isEpoch(data.epoch) && isCaps(data.caps)
}

function capsOf(data: CallMessage): Caps | null {
  if (!('caps' in data) || !isCaps(data.caps)) return null
  return data.caps
}

/** The key message of the call: applied once however many lines deliver it. */
function onKeyMessage(data: CallMessage, actions: LineActions): boolean {
  if (!isKeyPayload(data)) return false
  actions.remember({
    epoch: data.epoch,
    key: data.key,
    replace: 'replace' in data && data.replace === true,
    seq: optionalSeq(data),
    soft: 'soft' in data && data.soft === true,
    caps: capsOf(data),
  })
  return true
}

function onCapsMessage(data: CallMessage, actions: LineActions) {
  if (!isCapsPayload(data)) return
  if (!actions.noteSeq(optionalSeq(data))) return
  actions.applyCaps(data.epoch, data.caps)
}

function onDrop(data: CallMessage, actions: LineActions): boolean {
  if (messageType(data) !== 'drop') return false
  actions.drop()
  return true
}

function onStale(data: CallMessage, actions: LineActions): boolean {
  if (messageType(data) !== 'stale') return false
  actions.stale()
  return true
}

function onSender(line: Line, data: CallMessage): boolean {
  if (line.direction !== 'recv' || messageType(data) !== 'sender') return false
  if (!('senderId' in data)) return false
  if (typeof data.senderId !== 'string' || !data.senderId) return false
  line.senderId = data.senderId
  // A reused receiver may now carry another source.
  line.source = 'source' in data && isFrameSource(data.source) ? data.source : null
  return true
}

/** What the call tells a line over its port: drop, stale, who publishes, a key or capabilities. */
export function onLineMessage(line: Line, data: unknown, actions: LineActions) {
  if (!isCallMessage(data)) return
  if (onDrop(data, actions)) return
  if (onStale(data, actions)) return
  if (onSender(line, data)) return
  if (onKeyMessage(data, actions)) return
  onCapsMessage(data, actions)
}
