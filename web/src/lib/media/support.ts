import { canArm } from './guard'

let probed: Promise<boolean> | null = null

/** WebCrypto Ed25519: signs every sealed frame. Older Safari and Firefox builds lack it. */
async function hasEd25519(): Promise<boolean> {
  if (typeof crypto === 'undefined' || !crypto.subtle) return false
  try {
    await crypto.subtle.generateKey('Ed25519', false, ['sign', 'verify'])
    return true
  } catch {
    return false
  }
}

/**
 * Whether this browser can seal call media at all: a worker, the frame
 * transform that reaches the encoded media, and the signature algorithm.
 * A browser that cannot never joins a call; media is never sent unsealed.
 */
export function mediaSealingSupported(): Promise<boolean> {
  probed ??= canArm() ? hasEd25519() : Promise.resolve(false)
  return probed
}

export function resetMediaSealingProbe() {
  probed = null
}
