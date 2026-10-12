import { FRAME_EPOCH_BYTES } from './bounds'

const MAGIC_0 = 0x4d
const MAGIC_1 = 0x31
const EPOCH_OFFSET = 2
const NONCE_OFFSET = 6
const NONCE_BYTES = 12
const HEADER_BYTES = NONCE_OFFSET + NONCE_BYTES
const TAG_BYTES = 16

export interface FrameSeal {
  key: CryptoKey
  epoch: number
  channelId: string
  senderId: string
  plaintext: Uint8Array
}

export interface FrameOpen {
  keys: ReadonlyMap<number, CryptoKey>
  channelId: string
  senderId: string
  packet: Uint8Array
}

function copyBytes(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

export async function importEpochKey(raw: Uint8Array): Promise<CryptoKey | null> {
  if (raw.byteLength !== FRAME_EPOCH_BYTES) return null
  try {
    return await crypto.subtle.importKey('raw', copyBytes(raw), 'AES-GCM', false, ['encrypt', 'decrypt'])
  } catch {
    return null
  }
}

export function bindFrameAad(channelId: string, senderId: string, epoch: number): Uint8Array {
  const channel = new TextEncoder().encode(channelId)
  const sender = new TextEncoder().encode(senderId)
  const out = new Uint8Array(channel.length + 1 + sender.length + 4)
  out.set(channel, 0)
  out.set(sender, channel.length + 1)
  const view = new DataView(out.buffer)
  view.setUint32(out.length - 4, epoch)
  return out
}

/** Magic || epoch uint32be || 12-byte nonce || ciphertext and tag. */
export async function sealFrame(frame: FrameSeal): Promise<Uint8Array | null> {
  if (!Number.isInteger(frame.epoch) || frame.epoch < 0 || frame.epoch > 0xffffffff) return null
  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES))
  let sealed: ArrayBuffer
  try {
    sealed = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: copyBytes(nonce), additionalData: copyBytes(bindFrameAad(frame.channelId, frame.senderId, frame.epoch)) },
      frame.key,
      copyBytes(frame.plaintext),
    )
  } catch {
    return null
  }
  const cipher = new Uint8Array(sealed)
  const packet = new Uint8Array(HEADER_BYTES + cipher.byteLength)
  packet[0] = MAGIC_0
  packet[1] = MAGIC_1
  new DataView(packet.buffer).setUint32(EPOCH_OFFSET, frame.epoch)
  packet.set(nonce, NONCE_OFFSET)
  packet.set(cipher, HEADER_BYTES)
  return packet
}

/** Returns null when the packet, epoch, sender binding or tag does not match. */
export async function openFrame(frame: FrameOpen): Promise<Uint8Array | null> {
  if (frame.packet.byteLength < HEADER_BYTES + TAG_BYTES) return null
  if (frame.packet[0] !== MAGIC_0 || frame.packet[1] !== MAGIC_1) return null
  const epoch = new DataView(frame.packet.buffer, frame.packet.byteOffset, frame.packet.byteLength).getUint32(EPOCH_OFFSET)
  const key = frame.keys.get(epoch)
  if (!key) return null
  const nonce = frame.packet.subarray(NONCE_OFFSET, HEADER_BYTES)
  const cipher = frame.packet.subarray(HEADER_BYTES)
  try {
    const opened = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: copyBytes(nonce), additionalData: copyBytes(bindFrameAad(frame.channelId, frame.senderId, epoch)) },
      key,
      copyBytes(cipher),
    )
    return new Uint8Array(opened)
  } catch {
    return null
  }
}

/** The live epoch and the one before it. Older keys cannot open new frames. */
export function rememberEpochKey(keys: Map<number, CryptoKey>, epoch: number, key: CryptoKey): void {
  keys.set(epoch, key)
  const ordered = [...keys.keys()].sort((a, b) => a - b)
  for (const drop of ordered.slice(0, -2)) keys.delete(drop)
}
