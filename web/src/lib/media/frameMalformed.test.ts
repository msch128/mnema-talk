import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { CAPABILITY_BYTES, openCapability, sealCapability } from './capability'
import { openMedia, type VideoCodec } from './codec'
import { ReplayWindow, SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, openFrame, sealFrame, type EpochKeys, type FrameSource } from './frame'
import { mulberry32 as prng, randomBytes as bytes } from './malformed.fixture'
import { testAesKey, testEpochKeys, testMember, type TestMember } from './keys.fixture'

// Property-style tests: inputs come from a fixed-seed generator, so a failure
// reproduces. The property is the same everywhere: a unit that is not exactly
// what a member sealed (random, truncated, extended, mutated, in the old
// format or on the wrong line) is refused with null and never throws.
const SEED = 0x6d6e656d
const previous = globalThis.crypto
let alice: TestMember
let bob: TestMember
let keys: EpochKeys

beforeAll(async () => {
  vi.stubGlobal('crypto', webcrypto)
  alice = await testMember()
  bob = await testMember()
  keys = testEpochKeys(await testAesKey(7), alice, { alice, bob })
})
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function sealedUnit(source: FrameSource = SOURCE_MIC, plaintext = Uint8Array.of(1, 2, 3, 4, 5)): Promise<Uint8Array> {
  const packet = await sealFrame({ keys, epoch: 4, stream: 8, counter: 1, channelId: 'channel', senderId: 'alice', plaintext, source })
  if (!packet) throw new Error('expected a sealed frame')
  return packet
}

function tryOpen(packet: Uint8Array, over: { source?: FrameSource | null, onLegacy?: () => void } = {}): Promise<Uint8Array | null> {
  return openFrame({ keys: new Map([[4, keys]]), channelId: 'channel', senderId: 'alice', packet, replay: new ReplayWindow(), source: over.source ?? SOURCE_MIC, ...(over.onLegacy ? { onLegacy: over.onLegacy } : {}) })
}

describe('malformed media units are refused without throwing', () => {
  it('refuses random bytes of any length, with and without the media magic', async () => {
    const random = prng(SEED)
    for (let i = 0; i < 300; i++) {
      const length = Math.floor(random() * 400)
      const packet = bytes(random, length)
      if (i % 2 === 0 && length >= 2) { packet[0] = 0x4d; packet[1] = 0x33 }
      await expect(tryOpen(packet)).resolves.toBeNull()
    }
  })

  it('refuses every truncation of a valid unit, in both formats', async () => {
    {
      const packet = await sealedUnit()
      await expect(tryOpen(packet, { source: SOURCE_MIC })).resolves.toEqual(Uint8Array.of(1, 2, 3, 4, 5))
      for (let length = 0; length < packet.byteLength; length++) {
        await expect(tryOpen(packet.slice(0, length))).resolves.toBeNull()
      }
    }
  })

  it('refuses a valid unit with bytes appended or prepended', async () => {
    const random = prng(SEED + 1)
    {
      const packet = await sealedUnit()
      for (let extra = 1; extra <= 8; extra++) {
        const tail = new Uint8Array(packet.byteLength + extra)
        tail.set(packet)
        tail.set(bytes(random, extra), packet.byteLength)
        await expect(tryOpen(tail)).resolves.toBeNull()
        const head = new Uint8Array(packet.byteLength + extra)
        head.set(bytes(random, extra))
        head.set(packet, extra)
        await expect(tryOpen(head)).resolves.toBeNull()
      }
    }
  })

  it('refuses a valid unit with any single byte changed', async () => {
    const random = prng(SEED + 2)
    {
      const packet = await sealedUnit()
      for (let at = 0; at < packet.byteLength; at++) {
        const copy = packet.slice()
        copy[at] = (copy[at] as number) ^ (1 + Math.floor(random() * 255))
        await expect(tryOpen(copy)).resolves.toBeNull()
      }
    }
  })

  it('refuses a valid unit with several random bytes changed', async () => {
    const random = prng(SEED + 3)
    const packet = await sealedUnit()
    for (let i = 0; i < 100; i++) {
      const copy = packet.slice()
      const flips = 2 + Math.floor(random() * 6)
      for (let f = 0; f < flips; f++) copy[Math.floor(random() * copy.length)] = Math.floor(random() * 256)
      if (copy.every((b, at) => b === packet[at])) continue
      await expect(tryOpen(copy)).resolves.toBeNull()
    }
  })

  it('never opens a unit in the retired version 2 format, and reports it', async () => {
    const v3 = await sealedUnit()
    // A version 3 unit presented as version 2, and unknown versions.
    let legacy = 0
    for (const version of [0x32, 0x00, 0x31, 0x34, 0x4d, 0xff]) {
      const other = v3.slice()
      other[1] = version
      await expect(tryOpen(other, { onLegacy: () => { legacy++ } })).resolves.toBeNull()
    }
    expect(legacy).toBe(1)
    // A version 2 layout (no line byte) with the version 2 magic.
    const v2 = new Uint8Array(v3.byteLength - 1)
    v2.set(v3.subarray(0, 14))
    v2.set(v3.subarray(15), 14)
    v2[1] = 0x32
    await expect(tryOpen(v2)).resolves.toBeNull()
  })

  it('refuses a version 3 unit on any line it was not sealed for, whatever the line byte says', async () => {
    const lines: FrameSource[] = [SOURCE_MIC, SOURCE_CAMERA, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO]
    for (const sealedFor of lines) {
      const packet = await sealedUnit(sealedFor)
      for (const onLine of lines) {
        const opened = await tryOpen(packet, { source: onLine })
        if (onLine === sealedFor && sealedFor !== SOURCE_SCREEN) expect(opened).toEqual(Uint8Array.of(1, 2, 3, 4, 5))
        else if (onLine !== sealedFor) expect(opened).toBeNull()
      }
    }
    const mic = await sealedUnit(SOURCE_MIC)
    for (let line = 0; line < 256; line++) {
      const copy = mic.slice()
      copy[14] = line
      if (line !== 0) await expect(tryOpen(copy, { source: (line >> 4) as FrameSource })).resolves.toBeNull()
    }
  })

  it('refuses units from a sender or channel other than the one that sealed them', async () => {
    const packet = await sealedUnit()
    const open = (senderId: string, channelId: string) => openFrame({ keys: new Map([[4, keys]]), channelId, senderId, packet, replay: new ReplayWindow(), source: SOURCE_MIC })
    await expect(open('alice', 'channel')).resolves.toEqual(Uint8Array.of(1, 2, 3, 4, 5))
    await expect(open('bob', 'channel')).resolves.toBeNull()
    await expect(open('alice', 'other')).resolves.toBeNull()
    await expect(open('carol', 'channel')).resolves.toBeNull()
    await expect(open('', '')).resolves.toBeNull()
  })

  it('does not throw when the media layer is handed garbage for any codec and frame kind', async () => {
    const random = prng(SEED + 5)
    const codecs: Array<VideoCodec | null> = ['h264', 'vp8', null]
    const kinds = ['key', 'delta', undefined] as const
    const open = (packet: Uint8Array, clear: Uint8Array) => openFrame({ keys: new Map([[4, keys]]), channelId: 'channel', senderId: 'alice', packet, clear, replay: new ReplayWindow(), source: SOURCE_CAMERA })
    for (let i = 0; i < 200; i++) {
      const data = bytes(random, Math.floor(random() * 300))
      if (i % 3 === 0 && data.length > 4) { data[0] = 0; data[1] = 0; data[2] = 0; data[3] = 1 }
      for (const codec of codecs) {
        for (const kind of kinds) {
          const result = await openMedia(data, kind, codec, open)
          expect(result).toBeNull()
        }
      }
    }
  })

  it('refuses capability messages that are random, truncated, extended or changed', async () => {
    const random = prng(SEED + 6)
    const group = Uint8Array.of(1, 2, 3)
    const wire = await sealCapability(alice.signer, group, 'alice')
    if (!wire) throw new Error('expected a capability')
    expect(wire.byteLength).toBe(CAPABILITY_BYTES)
    await expect(openCapability(alice.verifier, group, 'alice', wire)).resolves.toBe(1)
    // Another member's key, another group, another user.
    await expect(openCapability(bob.verifier, group, 'alice', wire)).resolves.toBeNull()
    await expect(openCapability(alice.verifier, Uint8Array.of(9), 'alice', wire)).resolves.toBeNull()
    await expect(openCapability(alice.verifier, group, 'bob', wire)).resolves.toBeNull()
    for (let length = 0; length < wire.byteLength; length++) {
      await expect(openCapability(alice.verifier, group, 'alice', wire.slice(0, length))).resolves.toBeNull()
    }
    await expect(openCapability(alice.verifier, group, 'alice', new Uint8Array([...wire, 0]))).resolves.toBeNull()
    for (let at = 0; at < wire.byteLength; at++) {
      const copy = wire.slice()
      copy[at] = (copy[at] as number) ^ (1 + Math.floor(random() * 255))
      await expect(openCapability(alice.verifier, group, 'alice', copy)).resolves.toBeNull()
    }
    for (let i = 0; i < 100; i++) {
      await expect(openCapability(alice.verifier, group, 'alice', bytes(random, Math.floor(random() * 120)))).resolves.toBeNull()
    }
  })
})
