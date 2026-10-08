import { MediaCryptor, decrypt_known_vector_low_level, encrypt_known_vector_low_level } from './crypto/mnema_media_spike.js'
import { createProtectedContext } from './protected-context.ts'
import { processProtectedFrame } from './protected-frame.ts'

const hex = (value: string) => Uint8Array.from(value.match(/../g)!.map(byte => Number.parseInt(byte, 16)))
const same = (left: Uint8Array, right: Uint8Array) => left.length === right.length && left.every((byte, i) => byte === right[i])

/** Only public fixture bytes. This never supplies keys for live synthetic media. */
export function checkSframe(): string[] {
  const passed: string[] = []
  const require = (condition: boolean, name: string) => { if (!condition) throw new Error(`sframe_selftest_failed_${name}`); passed.push(name) }
  const rejects = (callback: () => unknown) => {
    try { callback(); return false } catch (error) {
      // Traps/panics are not acceptable malformed-input rejection.
      if (error instanceof WebAssembly.RuntimeError) throw new Error('sframe_selftest_wasm_trap')
      return true
    }
  }
  const base = hex('000102030405060708090a0b0c0d0e0f')
  const metadata = new TextEncoder().encode('IETF SFrame WG')
  const plaintext = new TextEncoder().encode('draft-ietf-sframe-enc')
  const vector = hex('990123456794f509d36e9beacb0e261d99c7d1e972f1fed787d4049f17ca21353c1cc24d56ceabced279')
  require(same(encrypt_known_vector_low_level(base, 0x0123, 0x4567, metadata, plaintext), vector), 'rfc9605-suite5-encrypt-exact')
  require(same(decrypt_known_vector_low_level(base, 0x0123, metadata, vector), plaintext), 'rfc9605-suite5-decrypt-exact')
  const payload = Uint8Array.from([0x78, 1, 2, 3, 4, 5, 6, 7, 8])
  const cryptors: MediaCryptor[] = []
  const make = (key = base, context = 1, epoch = 1) => {
    const cryptor = new MediaCryptor(key, context, epoch, 1)
    cryptors.push(cryptor)
    return cryptor
  }
  try {
    const transmit = make()
    const receive = make()
    const ciphertext = transmit.encrypt(payload, 'opus')
    require(!same(ciphertext, payload), 'media-ciphertext-not-plaintext')
    const damaged = ciphertext.slice(); damaged[damaged.length - 1]! ^= 1
    require(rejects(() => receive.decrypt(damaged, 'opus')), 'tampered-auth-tag-rejected')
    const metadataDamaged = ciphertext.slice(); metadataDamaged[0]! ^= 1
    require(rejects(() => receive.decrypt(metadataDamaged, 'opus')), 'tampered-clear-codec-metadata-rejected')
    const headerDamaged = ciphertext.slice(); headerDamaged[1]! ^= 1
    require(rejects(() => receive.decrypt(headerDamaged, 'opus')), 'tampered-sframe-header-rejected')
    require(same(receive.decrypt(ciphertext, 'opus'), payload), 'valid-after-forgery-no-replay-state-poison')
    require(rejects(() => receive.decrypt(ciphertext, 'opus')), 'counter-replay-rejected')
    const freshCiphertext = transmit.encrypt(payload, 'opus')
    const unknown = make(base, 2)
    require(rejects(() => unknown.decrypt(freshCiphertext, 'opus')), 'unknown-kid-rejected')
    const nextEpoch = make(base, 1, 2)
    require(rejects(() => nextEpoch.decrypt(freshCiphertext, 'opus')), 'stale-epoch-kid-rejected')
    const wrongKey = make(new Uint8Array(32).fill(0x99))
    require(rejects(() => wrongKey.decrypt(freshCiphertext, 'opus')), 'wrong-key-same-kid-rejected')
    const bounded = make()
    for (let length = 0; length < freshCiphertext.length; length++) {
      if (!rejects(() => bounded.decrypt(freshCiphertext.slice(0, length), 'opus'))) throw new Error('sframe_selftest_truncation_accepted')
    }
    require(true, 'every-truncated-prefix-rejected-no-traps')
    require(same(bounded.decrypt(freshCiphertext, 'opus'), payload), 'valid-after-truncation')
    require(rejects(() => transmit.encrypt(payload, 'h264')), 'unsupported-codec-send-rejected')
    require(rejects(() => receive.decrypt(payload, 'h264')), 'unsupported-codec-receive-rejected')
    // Test the wrapper's VP8 key/interframe authenticated clear-prefix layouts.
    for (const bytes of [Uint8Array.from([0,1,2,3,4,5,6,7,8,9,10,11]), Uint8Array.from([1,2,3,4,5,6])]) {
      const sender = make(base, 3)
      const receiver = make(base, 3)
      const wire = sender.encrypt(bytes, 'vp8')
      require(same(receiver.decrypt(wire, 'vp8'), bytes), bytes[0] === 0 ? 'vp8-keyframe-roundtrip' : 'vp8-interframe-roundtrip')
    }
    const consumed = new Set<number>()
    const key = new Uint8Array(32).fill(0x88)
    const source = createProtectedContext(key, 1, consumed)
    const first = source.encrypt(payload, 'opus')
    source.free()
    require(rejects(() => createProtectedContext(key, 1, consumed)), 'ended-transform-context-cannot-reset-counter')
    const independent = createProtectedContext(key, 2, consumed)
    require(!same(independent.encrypt(payload, 'opus'), first), 'distinct-source-context-has-distinct-ciphertext')
    independent.free()
    const empty = new Uint8Array()
    const gatedReceiver = make(base, 4)
    const gatedSender = make(base, 4)
    const gateCiphertext = gatedSender.encrypt(payload, 'opus')
    let calls = 0
    const observedCryptor = { encrypt(input: Uint8Array, codec: string) { calls++; return gatedReceiver.encrypt(input, codec) }, decrypt(input: Uint8Array, codec: string) { calls++; return gatedReceiver.decrypt(input, codec) } }
    const discarded = processProtectedFrame(observedCryptor, empty, 'opus', false)
    require(discarded.kind === 'native-empty-audio-discard' && !('output' in discarded) && calls === 0, 'zero-receive-audio-no-crypto-or-output')
    require(rejects(() => processProtectedFrame(gatedSender, empty, 'opus', true)), 'zero-send-audio-still-rejected')
    require(rejects(() => processProtectedFrame(gatedReceiver, empty, 'vp8', false)), 'zero-receive-video-still-rejected')
    for (let length = 1; length < gateCiphertext.length; length++) {
      if (!rejects(() => processProtectedFrame(gatedReceiver, gateCiphertext.slice(0, length), 'opus', false))) throw new Error('sframe_selftest_nonzero_prefix_accepted')
    }
    require(true, 'all-nonzero-prefixes-through-live-gate-rejected')
    const gatedValid = processProtectedFrame(gatedReceiver, gateCiphertext, 'opus', false)
    require(gatedValid.kind === 'transformed' && same(gatedValid.output, payload) && rejects(() => processProtectedFrame(gatedReceiver, gateCiphertext, 'opus', false)), 'empty-discard-preserves-valid-counter-and-replay-gate')
    const nextGateCiphertext = gatedSender.encrypt(payload, 'opus')
    const tamperedGate = nextGateCiphertext.slice(); tamperedGate[tamperedGate.length - 1]! ^= 1
    require(rejects(() => processProtectedFrame(gatedReceiver, tamperedGate, 'opus', false)), 'tamper-through-live-gate-rejected')
    return passed
  } finally { for (const cryptor of cryptors) cryptor.free() }
}
