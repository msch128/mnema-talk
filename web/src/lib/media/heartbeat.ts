// Liveness message of a Talk member (version 1):
//   'M' 'H' | epoch u32 | time f64 (sender's clock, ms) | Ed25519 signature (64)
// It goes over the same handshake channel as the MLS messages, about once per
// HEARTBEAT_MS, so a member that sends no media (muted, listening) still shows
// that it is there. The signature is made with the member's MLS leaf key over
// the group, the sender's id, the epoch and the time, so only the member
// itself can vouch for itself, and only for the epoch it is in. The time is
// never compared with the receiver's clock; it only has to grow, so an older
// copy of a heartbeat does not keep a member alive.
const MAGIC_0 = 0x4d
const MAGIC_1 = 0x48
const BODY_BYTES = 14
const SIGNATURE_BYTES = 64
export const HEARTBEAT_BYTES = BODY_BYTES + SIGNATURE_BYTES
const DOMAIN = new TextEncoder().encode('mnema-media-heartbeat-v1')

function signedBytes(groupId: Uint8Array, userId: string, body: Uint8Array): ArrayBuffer {
  const id = new TextEncoder().encode(userId)
  const out = new Uint8Array(DOMAIN.byteLength + groupId.byteLength + id.byteLength + BODY_BYTES)
  out.set(DOMAIN, 0)
  out.set(groupId, DOMAIN.byteLength)
  out.set(id, DOMAIN.byteLength + groupId.byteLength)
  out.set(body.subarray(0, BODY_BYTES), DOMAIN.byteLength + groupId.byteLength + id.byteLength)
  return out.buffer
}

export function isHeartbeat(wire: Uint8Array): boolean {
  return wire.byteLength >= 2 && wire[0] === MAGIC_0 && wire[1] === MAGIC_1
}

export async function sealHeartbeat(signer: CryptoKey, groupId: Uint8Array, userId: string, epoch: number, at: number): Promise<Uint8Array | null> {
  const out = new Uint8Array(HEARTBEAT_BYTES)
  const view = new DataView(out.buffer)
  out[0] = MAGIC_0
  out[1] = MAGIC_1
  view.setUint32(2, epoch)
  view.setFloat64(6, at)
  try {
    out.set(new Uint8Array(await crypto.subtle.sign('Ed25519', signer, signedBytes(groupId, userId, out))), BODY_BYTES)
    return out
  } catch {
    return null
  }
}

/** The epoch and time of a heartbeat that `verifier` (the sender's leaf key) signed, or null. */
export async function openHeartbeat(verifier: CryptoKey, groupId: Uint8Array, userId: string, wire: Uint8Array): Promise<{ epoch: number, at: number } | null> {
  if (!isHeartbeat(wire) || wire.byteLength !== HEARTBEAT_BYTES) return null
  const view = new DataView(wire.buffer, wire.byteOffset, wire.byteLength)
  const at = view.getFloat64(6)
  if (!Number.isFinite(at)) return null
  try {
    const signature = wire.slice(BODY_BYTES)
    const ok = await crypto.subtle.verify('Ed25519', verifier, signature, signedBytes(groupId, userId, wire))
    return ok ? { epoch: view.getUint32(2), at } : null
  } catch {
    return null
  }
}
