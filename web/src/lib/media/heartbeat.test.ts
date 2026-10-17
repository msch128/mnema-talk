import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { HEARTBEAT_BYTES, isHeartbeat, openHeartbeat, sealHeartbeat } from './heartbeat'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const groupId = new Uint8Array(16).fill(7)
const user = '00000000-0000-4000-8000-000000000001'

async function pair() {
  return await webcrypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']) as CryptoKeyPair
}

describe('heartbeat', () => {
  it('opens for the signer, its group, its id and nothing else', async () => {
    const keys = await pair()
    const other = await pair()
    const beat = await sealHeartbeat(keys.privateKey, groupId, user, 5, 1234) as Uint8Array
    expect(beat.byteLength).toBe(HEARTBEAT_BYTES)
    expect(isHeartbeat(beat)).toBe(true)
    expect(await openHeartbeat(keys.publicKey, groupId, user, beat)).toEqual({ epoch: 5, at: 1234 })
    expect(await openHeartbeat(other.publicKey, groupId, user, beat)).toBeNull()
    expect(await openHeartbeat(keys.publicKey, new Uint8Array(16).fill(8), user, beat)).toBeNull()
    expect(await openHeartbeat(keys.publicKey, groupId, user.replace(/1$/, '2'), beat)).toBeNull()
    for (const index of [2, 9, 20, HEARTBEAT_BYTES - 1]) {
      const forged = beat.slice()
      forged[index] = (forged[index] ?? 0) ^ 1
      expect(await openHeartbeat(keys.publicKey, groupId, user, forged)).toBeNull()
    }
  })

  it('refuses what is not a heartbeat, a wrong length and a time that is not a number', async () => {
    const keys = await pair()
    expect(isHeartbeat(new Uint8Array([0x4d]))).toBe(false)
    expect(isHeartbeat(new Uint8Array([0, 1, 2]))).toBe(false)
    const beat = await sealHeartbeat(keys.privateKey, groupId, user, 1, 1) as Uint8Array
    expect(await openHeartbeat(keys.publicKey, groupId, user, beat.slice(0, 20))).toBeNull()
    expect(await openHeartbeat(keys.publicKey, groupId, user, new Uint8Array(HEARTBEAT_BYTES))).toBeNull()
    const nan = await sealHeartbeat(keys.privateKey, groupId, user, 1, Number.NaN) as Uint8Array
    expect(await openHeartbeat(keys.publicKey, groupId, user, nan)).toBeNull()
    expect(await sealHeartbeat(keys.publicKey, groupId, user, 1, 1)).toBeNull()
    expect(await openHeartbeat(keys.privateKey, groupId, user, beat)).toBeNull()
  })
})
