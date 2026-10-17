// Capability message of a Talk member (version 1):
//   'M' 'C' | bits u8 | Ed25519 signature (64)
// It goes over the handshake channel beside the heartbeat: when a member joins
// or meets a member it has not heard from, and with each heartbeat. Bit 0 says
// the member can open frame version 3 (the media line bound into the sealed
// frame). The signature is made with the member's MLS leaf key over the group,
// the sender's id and the bits, so only the member itself can say what it
// supports; the server can withhold the message but not forge one. Older
// clients take it for something that is not MLS and ignore it. It is not tied
// to an epoch: a leaf key lives as long as the browser session that holds it,
// and what that session supports does not change.
const MAGIC_0 = 0x4d
const MAGIC_1 = 0x43
const BODY_BYTES = 3
const SIGNATURE_BYTES = 64
export const CAPABILITY_BYTES = BODY_BYTES + SIGNATURE_BYTES
export const CAP_FRAME_V3 = 0x01
const DOMAIN = new TextEncoder().encode('mnema-media-capability-v1')

function signedBytes(groupId: Uint8Array, userId: string, bits: number): ArrayBuffer {
  const id = new TextEncoder().encode(userId)
  const out = new Uint8Array(DOMAIN.byteLength + 4 + groupId.byteLength + 4 + id.byteLength + 1)
  const view = new DataView(out.buffer)
  let at = 0
  out.set(DOMAIN, at)
  at += DOMAIN.byteLength
  view.setUint32(at, groupId.byteLength)
  at += 4
  out.set(groupId, at)
  at += groupId.byteLength
  view.setUint32(at, id.byteLength)
  at += 4
  out.set(id, at)
  at += id.byteLength
  out[at] = bits
  return out.buffer
}

export function isCapability(wire: Uint8Array): boolean {
  return wire.byteLength >= 2 && wire[0] === MAGIC_0 && wire[1] === MAGIC_1
}

export async function sealCapability(signer: CryptoKey, groupId: Uint8Array, userId: string, bits = CAP_FRAME_V3): Promise<Uint8Array | null> {
  const out = new Uint8Array(CAPABILITY_BYTES)
  out[0] = MAGIC_0
  out[1] = MAGIC_1
  out[2] = bits
  try {
    out.set(new Uint8Array(await crypto.subtle.sign('Ed25519', signer, signedBytes(groupId, userId, bits))), BODY_BYTES)
    return out
  } catch {
    return null
  }
}

/** The capability bits of a message that `verifier` (the sender's leaf key) signed, or null. */
export async function openCapability(verifier: CryptoKey, groupId: Uint8Array, userId: string, wire: Uint8Array): Promise<number | null> {
  if (!isCapability(wire) || wire.byteLength !== CAPABILITY_BYTES) return null
  const bits = wire[2] as number
  try {
    const ok = await crypto.subtle.verify('Ed25519', verifier, wire.slice(BODY_BYTES), signedBytes(groupId, userId, bits))
    return ok ? bits : null
  } catch {
    return null
  }
}
