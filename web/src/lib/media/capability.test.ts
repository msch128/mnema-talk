import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { CAPABILITY_BYTES, CAP_FRAME_V3, isCapability, openCapability, sealCapability } from './capability'
import { isHeartbeat } from './heartbeat'
import { testMember, type TestMember } from './keys.fixture'

const previous = globalThis.crypto
let alice: TestMember
let mallory: TestMember
const group = new Uint8Array(16).fill(3)

beforeAll(async () => {
  vi.stubGlobal('crypto', webcrypto)
  alice = await testMember()
  mallory = await testMember()
})
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('capability message', () => {
  it('carries what a member supports, signed with its leaf key', async () => {
    const wire = await sealCapability(alice.signer, group, 'alice')
    expect(wire?.byteLength).toBe(CAPABILITY_BYTES)
    expect([...(wire as Uint8Array).subarray(0, 3)]).toEqual([0x4d, 0x43, CAP_FRAME_V3])
    expect(isCapability(wire as Uint8Array)).toBe(true)
    // It is not a heartbeat: an older client, which takes only exact heartbeats, ignores it.
    expect(isHeartbeat(wire as Uint8Array)).toBe(false)
    await expect(openCapability(alice.verifier, group, 'alice', wire as Uint8Array)).resolves.toBe(CAP_FRAME_V3)
  })

  it('cannot be forged: another key, another member, another group, other bits, other length', async () => {
    const wire = await sealCapability(alice.signer, group, 'alice') as Uint8Array
    // Signed by someone else, or presented as someone else's.
    await expect(openCapability(mallory.verifier, group, 'alice', wire)).resolves.toBeNull()
    await expect(openCapability(alice.verifier, group, 'bob', wire)).resolves.toBeNull()
    await expect(openCapability(alice.verifier, new Uint8Array(16).fill(4), 'alice', wire)).resolves.toBeNull()
    // A member that signed "nothing" cannot be made to claim version 3.
    const none = await sealCapability(alice.signer, group, 'alice', 0) as Uint8Array
    await expect(openCapability(alice.verifier, group, 'alice', none)).resolves.toBe(0)
    const raised = none.slice()
    raised[2] = CAP_FRAME_V3
    await expect(openCapability(alice.verifier, group, 'alice', raised)).resolves.toBeNull()
    // Wrong shape.
    await expect(openCapability(alice.verifier, group, 'alice', wire.subarray(0, CAPABILITY_BYTES - 1))).resolves.toBeNull()
    const longer = new Uint8Array(CAPABILITY_BYTES + 1)
    longer.set(wire)
    await expect(openCapability(alice.verifier, group, 'alice', longer)).resolves.toBeNull()
    const flipped = wire.slice()
    flipped[10] = (flipped[10] ?? 0) ^ 1
    await expect(openCapability(alice.verifier, group, 'alice', flipped)).resolves.toBeNull()
    expect(isCapability(new Uint8Array([0x4d]))).toBe(false)
  })

  it('yields nothing where the browser cannot sign or verify', async () => {
    await expect(sealCapability({} as CryptoKey, group, 'alice')).resolves.toBeNull()
    const wire = await sealCapability(alice.signer, group, 'alice') as Uint8Array
    await expect(openCapability({} as CryptoKey, group, 'alice', wire)).resolves.toBeNull()
  })
})
