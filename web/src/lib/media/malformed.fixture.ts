// Seeded generators for the property-style tests of malformed input.

/** mulberry32: small, fast, deterministic. */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

export function randomBytes(random: () => number, length: number): Uint8Array {
  const out = new Uint8Array(length)
  for (let i = 0; i < length; i++) out[i] = Math.floor(random() * 256)
  return out
}

/** Bytes with the heartbeat magic and its exact length, but a random body and signature. */
export function heartbeatLikeWire(random: () => number): Uint8Array {
  const wire = randomBytes(random, 78)
  wire[0] = 0x4d
  wire[1] = 0x48
  return wire
}

/** Bytes with the capability magic and its exact length, but a random body and signature. */
export function capabilityLikeWire(random: () => number): Uint8Array {
  const wire = randomBytes(random, 67)
  wire[0] = 0x4d
  wire[1] = 0x43
  wire[2] = 1
  return wire
}
