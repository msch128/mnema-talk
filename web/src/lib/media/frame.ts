import { FRAME_EPOCH_BYTES } from './bounds'

// Wire format of one sealed unit, version 3:
//   'M' '3' | epoch u32 | stream u32 | counter u32 | line u8 | nonce (12) |
//   AES-GCM ciphertext and tag | Ed25519 signature (64)
// The line byte names the source (microphone, camera, screen, screen sound)
// in its high nibble and the screen layer in its low one. It is part of the
// AAD and of the signed bytes, and a receiver checks it against the line the
// unit arrived on, so a server that moves a unit to another line makes it
// unopenable. Version 2 ('M' '2', the same without the line byte and with its
// own signature domain) is still sealed in epochs with a member that cannot
// open version 3, and opened except in epochs where every member can
// (capability.ts).
// The stream is random per sending transform and the counter counts its
// units, so a receiver can refuse a unit it has already opened. The signature
// is made with the sender's MLS leaf key: other members hold the AES key too,
// but only the sender can sign for its own id.
const MAGIC_0 = 0x4d
const MAGIC_1 = 0x32
const MAGIC_1_V3 = 0x33
const EPOCH_OFFSET = 2
const STREAM_OFFSET = 6
const COUNTER_OFFSET = 10
const NONCE_OFFSET = 14
const NONCE_BYTES = 12
const HEADER_BYTES = NONCE_OFFSET + NONCE_BYTES
// Version 3 inserts the line byte after the counter.
const NONCE_OFFSET_V3 = NONCE_OFFSET + 1
const HEADER_BYTES_V3 = HEADER_BYTES + 1
const TAG_BYTES = 16
export const SIGNATURE_BYTES = 64
export const FRAME_OVERHEAD_BYTES = HEADER_BYTES + TAG_BYTES + SIGNATURE_BYTES
const MAX_U32 = 0xffffffff
const SIGNATURE_DOMAIN = new TextEncoder().encode('mnema-media-frame-v2')
const SIGNATURE_DOMAIN_V3 = new TextEncoder().encode('mnema-media-frame-v3')

/** What a line carries. The numbers are on the wire. */
export type FrameSource = 0 | 1 | 2 | 3
export const SOURCE_MIC = 0
export const SOURCE_CAMERA = 1
export const SOURCE_SCREEN = 2
export const SOURCE_SCREEN_AUDIO = 3
const LAYER_MASK = 3

export function isFrameSource(value: unknown): value is FrameSource {
  return value === 0 || value === 1 || value === 2 || value === 3
}

/** The line byte: source in the high nibble, layer (0..3, screen only) in the low one. */
export function lineByte(source: FrameSource, layer = 0): number {
  return (source << 4) | (layer & LAYER_MASK)
}

export type FrameVersion = 2 | 3
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
  /** 3 binds `source` and `layer` into the unit; 2 (the default) is the older format without them. */
  version?: FrameVersion | undefined
  source?: FrameSource | undefined
  /** Screen layer, 0..3; every other source is layer 0. */
  layer?: number | undefined
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
  /**
   * The line the unit arrived on. A version 3 unit opens only if it names this
   * source (and, for a screen, a layer its stream carries); without one no
   * version 3 unit opens.
   */
  source?: FrameSource | null | undefined
  /** Whether a version 2 unit of this epoch is still taken (while a member cannot open version 3). Absent: taken. */
  acceptV2?: ((epoch: number) => boolean) | undefined
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

export function bindFrameAad(channelId: string, senderId: string, epoch: number, stream: number, counter: number, clear: Uint8Array = new Uint8Array(), line?: number): Uint8Array {
  const channel = new TextEncoder().encode(channelId)
  const sender = new TextEncoder().encode(senderId)
  const fixed = channel.length + 1 + sender.length
  const extra = line === undefined ? 0 : 1
  const out = new Uint8Array(fixed + 12 + extra + clear.length)
  out.set(channel, 0)
  out.set(sender, channel.length + 1)
  const view = new DataView(out.buffer)
  view.setUint32(fixed, epoch)
  view.setUint32(fixed + 4, stream)
  view.setUint32(fixed + 8, counter)
  if (line !== undefined) out[fixed + 12] = line
  out.set(clear, fixed + 12 + extra)
  return out
}

function signedBytes(aad: Uint8Array, body: Uint8Array, domain: Uint8Array = SIGNATURE_DOMAIN): Uint8Array {
  const out = new Uint8Array(domain.byteLength + 4 + aad.byteLength + body.byteLength)
  out.set(domain)
  new DataView(out.buffer).setUint32(domain.byteLength, aad.byteLength)
  out.set(aad, domain.byteLength + 4)
  out.set(body, domain.byteLength + 4 + aad.byteLength)
  return out
}

export async function sealFrame(frame: FrameSeal): Promise<Uint8Array | null> {
  if (!isU32(frame.epoch) || !isU32(frame.stream) || !isU32(frame.counter)) return null
  const v3 = frame.version === 3
  if (v3 && !isFrameSource(frame.source)) return null
  const line = v3 ? lineByte(frame.source as FrameSource, frame.source === SOURCE_SCREEN ? frame.layer ?? 0 : 0) : undefined
  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES))
  const aad = bindFrameAad(frame.channelId, frame.senderId, frame.epoch, frame.stream, frame.counter, frame.clear, line)
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
  const header = v3 ? HEADER_BYTES_V3 : HEADER_BYTES
  const packet = new Uint8Array(header + cipher.byteLength + SIGNATURE_BYTES)
  packet[0] = MAGIC_0
  packet[1] = v3 ? MAGIC_1_V3 : MAGIC_1
  const view = new DataView(packet.buffer)
  view.setUint32(EPOCH_OFFSET, frame.epoch)
  view.setUint32(STREAM_OFFSET, frame.stream)
  view.setUint32(COUNTER_OFFSET, frame.counter)
  if (line !== undefined) packet[NONCE_OFFSET] = line
  packet.set(nonce, v3 ? NONCE_OFFSET_V3 : NONCE_OFFSET)
  packet.set(cipher, header)
  const body = packet.subarray(0, header + cipher.byteLength)
  let signature: ArrayBuffer
  try {
    signature = await crypto.subtle.sign('Ed25519', frame.keys.signer, copyBytes(signedBytes(aad, body, v3 ? SIGNATURE_DOMAIN_V3 : SIGNATURE_DOMAIN)))
  } catch {
    return null
  }
  if (signature.byteLength !== SIGNATURE_BYTES) return null
  packet.set(new Uint8Array(signature), body.byteLength)
  return packet
}

// The unit must be what the line it arrived on carries: same source, and
// a screen's layer is the one its stream was made for.
function lineAgrees(line: number, expected: FrameOpen['source']): boolean {
  const source = line >> 4
  if (!isFrameSource(source) || expected === undefined || expected === null || source !== expected) return false
  if ((line & ~(0xf0 | LAYER_MASK)) !== 0) return false
  return source === SOURCE_SCREEN || (line & LAYER_MASK) === 0
}

/** The keys of the epoch (this one and the one right before it) that hold the sender's verifier. */
function keyCandidates(frame: FrameOpen, epoch: number): EpochKeys[] {
  const candidates = [frame.keys.get(epoch), frame.previous?.get(epoch)]
    .filter((keys): keys is EpochKeys => keys !== undefined && keys.verifiers.has(frame.senderId))
  if (!candidates.length && !frame.keys.has(epoch) && !frame.previous?.has(epoch)) frame.onUnknownEpoch?.(epoch)
  return candidates
}

interface FrameUnit { epoch: number, stream: number, counter: number, aad: Uint8Array, v3: boolean, header: number }

/** Verifies the signature, then opens the unit, under each candidate key in turn. */
async function openWithKeys(frame: FrameOpen, candidates: EpochKeys[], unit: FrameUnit): Promise<Uint8Array | null> {
  const { epoch, stream, counter, aad, v3, header } = unit
  const packet = frame.packet
  const bodyEnd = packet.byteLength - SIGNATURE_BYTES
  const body = packet.subarray(0, bodyEnd)
  for (const keys of candidates) {
    try {
      const verifier = keys.verifiers.get(frame.senderId) as CryptoKey
      const signed = await crypto.subtle.verify('Ed25519', verifier, copyBytes(packet.subarray(bodyEnd)), copyBytes(signedBytes(aad, body, v3 ? SIGNATURE_DOMAIN_V3 : SIGNATURE_DOMAIN)))
      if (!signed) continue
      const opened = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: copyBytes(packet.subarray(v3 ? NONCE_OFFSET_V3 : NONCE_OFFSET, header)), additionalData: copyBytes(aad) },
        keys.key,
        copyBytes(packet.subarray(header, bodyEnd)),
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

/**
 * Returns null when the packet, epoch, sender, signature or tag does not
 * match, or when the replay window has seen the unit.
 */
export async function openFrame(frame: FrameOpen): Promise<Uint8Array | null> {
  const packet = frame.packet
  if (packet.byteLength < FRAME_OVERHEAD_BYTES || packet[0] !== MAGIC_0) return null
  const v3 = packet[1] === MAGIC_1_V3
  if (!v3 && packet[1] !== MAGIC_1) return null
  if (v3 && packet.byteLength < FRAME_OVERHEAD_BYTES + 1) return null
  const header = v3 ? HEADER_BYTES_V3 : HEADER_BYTES
  const line = v3 ? packet[NONCE_OFFSET] : undefined
  if (line !== undefined && !lineAgrees(line, frame.source)) return null
  const view = new DataView(packet.buffer, packet.byteOffset, packet.byteLength)
  const epoch = view.getUint32(EPOCH_OFFSET)
  const stream = view.getUint32(STREAM_OFFSET)
  const counter = view.getUint32(COUNTER_OFFSET)
  if (!v3 && frame.acceptV2 && !frame.acceptV2(epoch)) return null
  const candidates = keyCandidates(frame, epoch)
  if (!candidates.length) return null
  if (line !== undefined && line >> 4 === SOURCE_SCREEN && (stream & LAYER_MASK) !== (line & LAYER_MASK)) return null
  if (!frame.replay.fresh(epoch, stream, counter)) return null
  const aad = bindFrameAad(frame.channelId, frame.senderId, epoch, stream, counter, frame.clear, line)
  return openWithKeys(frame, candidates, { epoch, stream, counter, aad, v3, header })
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

import { ReplayWindow } from './replay'
export { REPLAY_STREAMS, REPLAY_IDLE_MS, REPLAY_RETIRED, ReplayWindow, rememberEpochKey } from './replay'
