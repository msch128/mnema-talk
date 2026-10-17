import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import {
  FRAME_OVERHEAD_BYTES, ReplayWindow, SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, isFrameSource, layerStreamId, lineByte,
  openFrame, sealFrame, type EpochKeys, type FrameSource,
} from './frame'
import { testAesKey, testEpochKeys, testMember, type TestMember } from './keys.fixture'

const previous = globalThis.crypto
let alice: TestMember
let bob: TestMember

beforeAll(async () => {
  vi.stubGlobal('crypto', webcrypto)
  alice = await testMember()
  bob = await testMember()
})
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function keysOf(byte = 7): Promise<EpochKeys> {
  return testEpochKeys(await testAesKey(byte), alice, { alice, bob })
}

interface Made { version?: 2 | 3, source?: FrameSource, layer?: number | undefined, stream?: number, counter?: number }

async function made(keys: EpochKeys, { version = 3, source = SOURCE_MIC, layer, stream = 9, counter = 0 }: Made = {}) {
  const packet = await sealFrame({ keys, epoch: 4, stream, counter, channelId: 'channel', senderId: 'alice', plaintext: Uint8Array.of(1, 2, 3), version, source, layer })
  if (!packet) throw new Error('expected a sealed frame')
  return packet
}

function open(keys: EpochKeys, packet: Uint8Array, over: { source?: FrameSource | null, acceptV2?: (epoch: number) => boolean, replay?: ReplayWindow } = {}) {
  return openFrame({ keys: new Map([[4, keys]]), channelId: 'channel', senderId: 'alice', packet, replay: over.replay ?? new ReplayWindow(), source: over.source, acceptV2: over.acceptV2 })
}

describe('frame version 3 binds the line', () => {
  it('seals a line byte and opens it on the line it names', async () => {
    const keys = await keysOf()
    for (const source of [SOURCE_MIC, SOURCE_CAMERA, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO] as const) {
      const stream = source === SOURCE_SCREEN ? layerStreamId(0, 100) : 9
      const packet = await made(keys, { source, stream })
      expect([...packet.subarray(0, 2)]).toEqual([0x4d, 0x33])
      expect(packet[14]).toBe(lineByte(source))
      expect(packet.byteLength).toBe(3 + FRAME_OVERHEAD_BYTES + 1)
      await expect(open(keys, packet, { source })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    }
    expect(isFrameSource(3)).toBe(true)
    expect(isFrameSource(4)).toBe(false)
  })

  it('refuses a frame relabelled onto another line: mic as screen sound, camera as screen, and the reverse', async () => {
    const keys = await keysOf()
    const mic = await made(keys, { source: SOURCE_MIC })
    const camera = await made(keys, { source: SOURCE_CAMERA })
    const screen = await made(keys, { source: SOURCE_SCREEN, stream: layerStreamId(0, 100) })
    const sound = await made(keys, { source: SOURCE_SCREEN_AUDIO })
    await expect(open(keys, mic, { source: SOURCE_SCREEN_AUDIO })).resolves.toBeNull()
    await expect(open(keys, sound, { source: SOURCE_MIC })).resolves.toBeNull()
    await expect(open(keys, camera, { source: SOURCE_SCREEN })).resolves.toBeNull()
    await expect(open(keys, screen, { source: SOURCE_CAMERA })).resolves.toBeNull()
    // Nothing opens where the receiver does not know the line.
    await expect(open(keys, mic)).resolves.toBeNull()
    await expect(open(keys, mic, { source: null })).resolves.toBeNull()
  })

  it('cannot be relabelled by rewriting the line byte: it is signed and part of the AAD', async () => {
    const keys = await keysOf()
    const mic = await made(keys, { source: SOURCE_MIC })
    const forged = mic.slice()
    forged[14] = lineByte(SOURCE_SCREEN_AUDIO)
    await expect(open(keys, forged, { source: SOURCE_SCREEN_AUDIO })).resolves.toBeNull()
    // A version 2 unit given a version 3 magic is no version 3 unit either.
    const v2 = await made(keys, { version: 2 })
    const lifted = new Uint8Array(v2.byteLength + 1)
    lifted.set(v2.subarray(0, 14))
    lifted[1] = 0x33
    lifted[14] = lineByte(SOURCE_SCREEN_AUDIO)
    lifted.set(v2.subarray(14), 15)
    await expect(open(keys, lifted, { source: SOURCE_SCREEN_AUDIO })).resolves.toBeNull()
    // And a version 3 unit made to look like version 2 loses its line byte's place.
    const lowered = mic.slice()
    lowered[1] = 0x32
    await expect(open(keys, lowered, { source: SOURCE_MIC })).resolves.toBeNull()
  })

  it('keeps a screen layer to its own stream: layer 1 does not pass for layer 0', async () => {
    const keys = await keysOf()
    const layer1 = await made(keys, { source: SOURCE_SCREEN, layer: 1, stream: layerStreamId(1, 100) })
    await expect(open(keys, layer1, { source: SOURCE_SCREEN })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    // Rewritten to say layer 0: the signature breaks.
    const forged = layer1.slice()
    forged[14] = lineByte(SOURCE_SCREEN, 0)
    await expect(open(keys, forged, { source: SOURCE_SCREEN })).resolves.toBeNull()
    // Sealed honestly with a layer its stream does not carry: refused as well.
    const mismatched = await made(keys, { source: SOURCE_SCREEN, layer: 1, stream: layerStreamId(0, 100) })
    await expect(open(keys, mismatched, { source: SOURCE_SCREEN })).resolves.toBeNull()
    // Only a screen has layers.
    const layered = await made(keys, { source: SOURCE_MIC, layer: 2 })
    expect(layered[14]).toBe(lineByte(SOURCE_MIC))
    const odd = layered.slice()
    odd[14] = (SOURCE_MIC << 4) | 2
    await expect(open(keys, odd, { source: SOURCE_MIC })).resolves.toBeNull()
    const junk = layered.slice()
    junk[14] = (SOURCE_MIC << 4) | 8
    await expect(open(keys, junk, { source: SOURCE_MIC })).resolves.toBeNull()
    const nowhere = layered.slice()
    nowhere[14] = 0x70
    await expect(open(keys, nowhere, { source: SOURCE_MIC })).resolves.toBeNull()
  })

  it('keeps the replay window: a unit opens once, whatever its version', async () => {
    const keys = await keysOf()
    const replay = new ReplayWindow()
    const packet = await made(keys, { counter: 5 })
    await expect(open(keys, packet, { source: SOURCE_MIC, replay })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, packet, { source: SOURCE_MIC, replay })).resolves.toBeNull()
    // The unit that did not open on the wrong line was not marked: it still opens on its own.
    const other = await made(keys, { counter: 6 })
    await expect(open(keys, other, { source: SOURCE_CAMERA, replay })).resolves.toBeNull()
    await expect(open(keys, other, { source: SOURCE_MIC, replay })).resolves.toEqual(Uint8Array.of(1, 2, 3))
  })

  it('seals version 3 only for a source, and short units do not open', async () => {
    const keys = await keysOf()
    await expect(sealFrame({ keys, epoch: 4, stream: 1, counter: 0, channelId: 'channel', senderId: 'alice', plaintext: Uint8Array.of(1), version: 3 })).resolves.toBeNull()
    const packet = await made(keys)
    // The smallest version 2 unit that is one byte short of a version 3 unit.
    const short = packet.subarray(0, FRAME_OVERHEAD_BYTES)
    await expect(open(keys, short, { source: SOURCE_MIC })).resolves.toBeNull()
  })
})

describe('frame version 2 is still taken while a member of its epoch cannot open version 3', () => {
  it('opens version 2 by default, and refuses the epochs acceptV2 names', async () => {
    const keys = await keysOf()
    const v2 = await made(keys, { version: 2 })
    expect([...v2.subarray(0, 2)]).toEqual([0x4d, 0x32])
    await expect(open(keys, v2)).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, v2, { source: SOURCE_SCREEN, acceptV2: () => true })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, v2, { acceptV2: () => false })).resolves.toBeNull()
    // The question is about the unit's own epoch (4), not another.
    await expect(open(keys, v2, { acceptV2: epoch => epoch !== 4 })).resolves.toBeNull()
    await expect(open(keys, v2, { acceptV2: epoch => epoch === 4 })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    // Version 3 always opens, so a capable sender that is slow to switch is not cut off from itself.
    const v3 = await made(keys)
    await expect(open(keys, v3, { source: SOURCE_MIC, acceptV2: () => false })).resolves.toEqual(Uint8Array.of(1, 2, 3))
  })

  it('cannot move a version 2 unit onto another line by relabelling, and counts as it did', async () => {
    const keys = await keysOf()
    const v2 = await made(keys, { version: 2, source: SOURCE_MIC })
    // Version 2 never knew a line: this is exactly what version 3 closes, and it is refused only in an epoch where every member can open version 3.
    await expect(open(keys, v2, { source: SOURCE_SCREEN_AUDIO, acceptV2: () => true })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, v2, { source: SOURCE_SCREEN_AUDIO, acceptV2: () => false })).resolves.toBeNull()
  })
})
