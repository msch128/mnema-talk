import type { MediaCryptor } from './crypto/mnema_media_spike.js'

type Cryptor = Pick<MediaCryptor, 'encrypt' | 'decrypt'>
export type ProtectedResult = { kind: 'transformed'; output: Uint8Array } | { kind: 'native-empty-audio-discard' }

/** Empty received RTP-audio payloads have no protected bytes. Discard, never forward. */
export function processProtectedFrame(cryptor: Cryptor | undefined, input: Uint8Array, codec: 'opus' | 'vp8', sending: boolean): ProtectedResult {
  if (!cryptor) throw new Error('protected_context_missing')
  if (!sending && codec === 'opus' && input.byteLength === 0) return { kind: 'native-empty-audio-discard' }
  // Every other input, including any nonempty truncated frame, must use the cryptor.
  return { kind: 'transformed', output: sending ? cryptor.encrypt(input, codec) : cryptor.decrypt(input, codec) }
}
