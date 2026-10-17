import { FRAME_EPOCH_BYTES } from './bounds'

// Wire format of one sealed unit (version 2):
//   'M' '2' | epoch u32 | stream u32 | counter u32 | nonce (12) | AES-GCM
//   ciphertext and tag | Ed25519 signature (64)
// The stream is random per sending transform and the counter counts its
// units, so a receiver can refuse a unit it has already opened. The signature
// is made with the sender's MLS leaf key: other members hold the AES key too,
// but only the sender can sign for its own id.
const MAGIC_0 = 0x4d
const MAGIC_1 = 0x32
const EPOCH_OFFSET = 2
const STREAM_OFFSET = 6
const COUNTER_OFFSET = 10
const NONCE_OFFSET = 14
const NONCE_BYTES = 12
const HEADER_BYTES = NONCE_OFFSET + NONCE_BYTES
const TAG_BYTES = 16
export const SIGNATURE_BYTES = 64
export const FRAME_OVERHEAD_BYTES = HEADER_BYTES + TAG_BYTES + SIGNATURE_BYTES
const MAX_U32 = 0xffffffff
const SIGNATURE_DOMAIN = new TextEncoder().encode('mnema-media-frame-v2')
// PKCS#8 prefix of an Ed25519 private key holding a 32-byte seed.
const ED25519_PKCS8_PREFIX = Uint8Array.of(0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20)
const ED25519_SEED_BYTES = 32
const ED25519_PUBLIC_BYTES = 32

/** What one epoch needs: the group's AES key, this member's signing key and every member's verifying key. */
export interface EpochKeys {
  key: CryptoKey
  signer: CryptoKey
  /** User id -> verifying key. A member whose id holds two leaves has none. */
  verifiers: ReadonlyMap<string, CryptoKey>
}

export interface FrameSeal {
  keys: EpochKeys
  epoch: number
  stream: number
  counter: number
  channelId: string
  senderId: string
  plaintext: Uint8Array
  /** Bytes that stay readable beside the sealed ones. They are authenticated. */
  clear?: Uint8Array
}

export interface FrameOpen {
  keys: ReadonlyMap<number, EpochKeys>
  /**
   * Keys of the group this member held before the call regrouped. The same
   * epoch number can exist in both; the sender's signature tells which.
   */
  previous?: ReadonlyMap<number, EpochKeys>
  channelId: string
  senderId: string
  packet: Uint8Array
  clear?: Uint8Array
  /** Refuses units already opened; marked only once a unit verified and opened. */
  replay: ReplayWindow
  /** Called with the epoch of a frame whose key is not held. */
  onUnknownEpoch?: (epoch: number) => void
}

function copyBytes(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

function isU32(value: number): boolean {
  return Number.isInteger(value) && value >= 0 && value <= MAX_U32
}

export async function importEpochKey(raw: Uint8Array): Promise<CryptoKey | null> {
  if (raw.byteLength !== FRAME_EPOCH_BYTES) return null
  try {
    return await crypto.subtle.importKey('raw', copyBytes(raw), 'AES-GCM', false, ['encrypt', 'decrypt'])
  } catch {
    return null
  }
}

/**
 * Imports an MLS Ed25519 signature private key: a 32-byte seed, or the PKCS#8
 * form a browser's own key generation returns. Null where the browser has no
 * Ed25519, which leaves the call without keys and so without media.
 */
export async function importFrameSigner(raw: Uint8Array): Promise<CryptoKey | null> {
  let pkcs8: Uint8Array
  if (raw.byteLength === ED25519_SEED_BYTES) {
    pkcs8 = new Uint8Array(ED25519_PKCS8_PREFIX.byteLength + ED25519_SEED_BYTES)
    pkcs8.set(ED25519_PKCS8_PREFIX)
    pkcs8.set(raw, ED25519_PKCS8_PREFIX.byteLength)
  } else if (raw.byteLength === ED25519_PKCS8_PREFIX.byteLength + ED25519_SEED_BYTES) {
    pkcs8 = raw.slice()
  } else {
    return null
  }
  try {
    return await crypto.subtle.importKey('pkcs8', copyBytes(pkcs8), 'Ed25519', false, ['sign'])
  } catch {
    return null
  } finally {
    pkcs8.fill(0)
  }
}

export async function importFrameVerifier(raw: Uint8Array): Promise<CryptoKey | null> {
  if (raw.byteLength !== ED25519_PUBLIC_BYTES) return null
  try {
    return await crypto.subtle.importKey('raw', copyBytes(raw), 'Ed25519', false, ['verify'])
  } catch {
    return null
  }
}

export function bindFrameAad(channelId: string, senderId: string, epoch: number, stream: number, counter: number, clear: Uint8Array = new Uint8Array()): Uint8Array {
  const channel = new TextEncoder().encode(channelId)
  const sender = new TextEncoder().encode(senderId)
  const fixed = channel.length + 1 + sender.length
  const out = new Uint8Array(fixed + 12 + clear.length)
  out.set(channel, 0)
  out.set(sender, channel.length + 1)
  const view = new DataView(out.buffer)
  view.setUint32(fixed, epoch)
  view.setUint32(fixed + 4, stream)
  view.setUint32(fixed + 8, counter)
  out.set(clear, fixed + 12)
  return out
}

function signedBytes(aad: Uint8Array, body: Uint8Array): Uint8Array {
  const out = new Uint8Array(SIGNATURE_DOMAIN.byteLength + 4 + aad.byteLength + body.byteLength)
  out.set(SIGNATURE_DOMAIN)
  new DataView(out.buffer).setUint32(SIGNATURE_DOMAIN.byteLength, aad.byteLength)
  out.set(aad, SIGNATURE_DOMAIN.byteLength + 4)
  out.set(body, SIGNATURE_DOMAIN.byteLength + 4 + aad.byteLength)
  return out
}

export async function sealFrame(frame: FrameSeal): Promise<Uint8Array | null> {
  if (!isU32(frame.epoch) || !isU32(frame.stream) || !isU32(frame.counter)) return null
  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES))
  const aad = bindFrameAad(frame.channelId, frame.senderId, frame.epoch, frame.stream, frame.counter, frame.clear)
  let sealed: ArrayBuffer
  try {
    sealed = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: copyBytes(nonce), additionalData: copyBytes(aad) },
      frame.keys.key,
      copyBytes(frame.plaintext),
    )
  } catch {
    return null
  }
  const cipher = new Uint8Array(sealed)
  const packet = new Uint8Array(HEADER_BYTES + cipher.byteLength + SIGNATURE_BYTES)
  packet[0] = MAGIC_0
  packet[1] = MAGIC_1
  const view = new DataView(packet.buffer)
  view.setUint32(EPOCH_OFFSET, frame.epoch)
  view.setUint32(STREAM_OFFSET, frame.stream)
  view.setUint32(COUNTER_OFFSET, frame.counter)
  packet.set(nonce, NONCE_OFFSET)
  packet.set(cipher, HEADER_BYTES)
  const body = packet.subarray(0, HEADER_BYTES + cipher.byteLength)
  let signature: ArrayBuffer
  try {
    signature = await crypto.subtle.sign('Ed25519', frame.keys.signer, copyBytes(signedBytes(aad, body)))
  } catch {
    return null
  }
  if (signature.byteLength !== SIGNATURE_BYTES) return null
  packet.set(new Uint8Array(signature), body.byteLength)
  return packet
}

/**
 * Returns null when the packet, epoch, sender, signature or tag does not
 * match, or when the replay window has seen the unit.
 */
export async function openFrame(frame: FrameOpen): Promise<Uint8Array | null> {
  const packet = frame.packet
  if (packet.byteLength < FRAME_OVERHEAD_BYTES) return null
  if (packet[0] !== MAGIC_0 || packet[1] !== MAGIC_1) return null
  const view = new DataView(packet.buffer, packet.byteOffset, packet.byteLength)
  const epoch = view.getUint32(EPOCH_OFFSET)
  const stream = view.getUint32(STREAM_OFFSET)
  const counter = view.getUint32(COUNTER_OFFSET)
  const candidates = [frame.keys.get(epoch), frame.previous?.get(epoch)]
    .filter((keys): keys is EpochKeys => keys !== undefined && keys.verifiers.has(frame.senderId))
  if (!candidates.length) {
    if (!frame.keys.has(epoch) && !frame.previous?.has(epoch)) frame.onUnknownEpoch?.(epoch)
    return null
  }
  if (!frame.replay.fresh(epoch, stream, counter)) return null
  const aad = bindFrameAad(frame.channelId, frame.senderId, epoch, stream, counter, frame.clear)
  const bodyEnd = packet.byteLength - SIGNATURE_BYTES
  const body = packet.subarray(0, bodyEnd)
  for (const keys of candidates) {
    try {
      const verifier = keys.verifiers.get(frame.senderId) as CryptoKey
      const signed = await crypto.subtle.verify('Ed25519', verifier, copyBytes(packet.subarray(bodyEnd)), copyBytes(signedBytes(aad, body)))
      if (!signed) continue
      const opened = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: copyBytes(packet.subarray(NONCE_OFFSET, HEADER_BYTES)), additionalData: copyBytes(aad) },
        keys.key,
        copyBytes(packet.subarray(HEADER_BYTES, bodyEnd)),
      )
      // Checked again: another unit of the same stream may have opened meanwhile.
      if (!frame.replay.mark(epoch, stream, counter)) return null
      return new Uint8Array(opened)
    } catch {
      // A tag that does not match under this key: try the other one, if any.
    }
  }
  return null
}

// A screen share's layers are sealed by transforms of their own (one per
// sending line), so each numbers its units from zero. Their stream IDs differ
// in the low LAYER_STREAM_BITS bits, which carry the layer, so no two layers
// of a share ever share a stream and a receiver's replay window can never
// mistake one layer's counter for another's, however the SFU switches between
// them. The other bits are random.
export const LAYER_STREAM_BITS = 2
export const LAYER_STREAM_MASK = (1 << LAYER_STREAM_BITS) - 1

/** A random stream ID that carries `layer` (0..3) in its low bits. */
export function layerStreamId(layer: number, random = new DataView(crypto.getRandomValues(new Uint8Array(4)).buffer).getUint32(0)): number {
  return ((random & ~LAYER_STREAM_MASK) | (layer & LAYER_STREAM_MASK)) >>> 0
}

/** Numbers the units one sending transform seals. It stops at the end of the counter. */
export class FrameStream {
  readonly id: number
  private counter = 0

  constructor(id = new DataView(crypto.getRandomValues(new Uint8Array(4)).buffer).getUint32(0)) {
    this.id = id
  }

  /** The next counter, or null once all 2^32 are used. */
  next(): number | null {
    if (this.counter > MAX_U32) return null
    return this.counter++
  }

  /** Test seam: continues at the given counter. */
  skipTo(counter: number) {
    this.counter = counter
  }
}

const REPLAY_WINDOW = 64n
const REPLAY_MASK = (1n << REPLAY_WINDOW) - 1n
// Streams one sender may open per epoch. Only a verified unit starts a stream,
// so only the sender itself can use them up; a new epoch starts afresh.
export const REPLAY_STREAMS = 256

/**
 * One sender's units seen per (epoch, stream): the highest counter and a
 * 64-unit bitmap below it, as SRTP does. A unit older than the window, or one
 * already seen, is refused. A stream is never forgotten while its epoch is
 * kept: once a sender has REPLAY_STREAMS streams in an epoch, further new
 * streams are refused instead of evicting an old one, which would let its
 * frames open again.
 */
export class ReplayWindow {
  private readonly streams = new Map<string, { top: number, seen: bigint }>()

  fresh(epoch: number, stream: number, counter: number): boolean {
    const entry = this.streams.get(`${epoch}:${stream}`)
    if (!entry) return this.room(epoch)
    if (counter > entry.top) return true
    const behind = BigInt(entry.top - counter)
    return behind < REPLAY_WINDOW && ((entry.seen >> behind) & 1n) === 0n
  }

  /** Records a unit. False when it was not fresh after all. */
  mark(epoch: number, stream: number, counter: number): boolean {
    if (!this.fresh(epoch, stream, counter)) return false
    const id = `${epoch}:${stream}`
    const entry = this.streams.get(id)
    if (!entry) {
      this.streams.set(id, { top: counter, seen: 1n })
    } else if (counter > entry.top) {
      const ahead = BigInt(counter - entry.top)
      const seen = ahead >= REPLAY_WINDOW ? 1n : ((entry.seen << ahead) | 1n) & REPLAY_MASK
      this.streams.set(id, { top: counter, seen })
    } else {
      this.streams.set(id, { top: entry.top, seen: entry.seen | (1n << BigInt(entry.top - counter)) })
    }
    return true
  }

  private room(epoch: number): boolean {
    const prefix = `${epoch}:`
    let used = 0
    for (const id of this.streams.keys()) if (id.startsWith(prefix)) used++
    return used < REPLAY_STREAMS
  }

  /** Forgets streams of epochs whose keys are gone. */
  retain(epochs: Iterable<number>) {
    const kept = new Set([...epochs].map(String))
    for (const id of [...this.streams.keys()]) if (!kept.has(id.slice(0, id.indexOf(':')))) this.streams.delete(id)
  }
}

/** The live epoch and the one before it. Older keys cannot open new frames. */
export function rememberEpochKey<T>(keys: Map<number, T>, epoch: number, key: T): void {
  keys.set(epoch, key)
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
}
