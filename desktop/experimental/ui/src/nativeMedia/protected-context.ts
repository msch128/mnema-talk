import { MediaCryptor } from './crypto/mnema_media_spike.js'

/** Conservative per-worker gate: a source may never recreate its counter. */
export function createProtectedContext(key: Uint8Array, context: number, consumed: Set<number>): MediaCryptor {
  if (key.length !== 32 || (context !== 1 && context !== 2) || consumed.has(context)) throw new Error('protected_context_rejected')
  consumed.add(context)
  return new MediaCryptor(key, context, 1, 1)
}
